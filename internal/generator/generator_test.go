package generator

import (
	"bytes"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func testParams(t *testing.T) Params {
	t.Helper()
	p, err := NewParams("github.com/acme/demo-api", "", "../..", "", "v0.3.0")
	if err != nil {
		t.Fatalf("NewParams: %v", err)
	}
	return p
}

func TestNewParams_Derivation(t *testing.T) {
	p := testParams(t)

	if p.AppName != "demo-api" {
		t.Errorf("AppName = %q, want demo-api", p.AppName)
	}
	if p.Package != "demoapi" {
		t.Errorf("Package = %q, want demoapi", p.Package)
	}
	if p.ServiceName != "DemoApi" {
		t.Errorf("ServiceName = %q, want DemoApi", p.ServiceName)
	}
	if p.ServerPkg != "demoapiservice" {
		t.Errorf("ServerPkg = %q, want demoapiservice", p.ServerPkg)
	}
	if p.LibVersion != "v0.3.0" {
		t.Errorf("LibVersion = %q, want v0.3.0", p.LibVersion)
	}

	if _, err := NewParams("no spaces allowed", "", "", "", "dev"); err == nil {
		t.Error("ожидалась ошибка на некорректном module path")
	}
}

// TestGoCamelCase сверяет воспроизведённые правила protoc-gen-go.
// Ключевой случай — буква после цифры: из-за него имя сервиса, объявленное
// в proto, и имя в сгенерированном Go-коде могут разойтись, и проект
// перестанет собираться.
func TestGoCamelCase(t *testing.T) {
	cases := map[string]string{
		"e2edemo":       "E2Edemo",
		"demo":          "Demo",
		"demo_api":      "DemoApi",
		"DemoService":   "DemoService",
		"s3proxy":       "S3Proxy",
		"oauth2server":  "Oauth2Server",
		"v2":            "V2",
		"EchoRequest":   "EchoRequest",
		"echo_request":  "EchoRequest",
		"_leading":      "XLeading",
		"pkg.message":   "PkgMessage",
		"pkg.Message":   "Pkg_Message",
		"DemoServiceV2": "DemoServiceV2",
		"":              "",
	}
	for in, want := range cases {
		if got := GoCamelCase(in); got != want {
			t.Errorf("GoCamelCase(%q) = %q, want %q", in, got, want)
		}
	}
}

// Имя сервиса должно быть неподвижной точкой GoCamelCase: одно и то же
// имя идёт и в proto, и в Go-код шаблонов.
func TestServiceName_IsGoCamelCaseFixedPoint(t *testing.T) {
	for _, appName := range []string{"demo", "demo-api", "e2edemo", "s3proxy", "oauth2server", "my-long-name"} {
		svc := serviceName(appName)
		if got := GoCamelCase(svc); got != svc {
			t.Errorf("serviceName(%q) = %q, но GoCamelCase(%q) = %q — proto и Go-код разойдутся",
				appName, svc, svc, got)
		}
		if got := GoCamelCase(svc + "Service"); got != svc+"Service" {
			t.Errorf("%qService манглится в %q — ссылки на Unimplemented/Register сломаются", svc, got)
		}
	}
}

// TestScratchInstallable: «есть semver-тег» и «резолвится из сети» — разные
// факты. Если их смешать, `make bootstrap` пойдёт за неопубликованным модулем.
func TestScratchInstallable(t *testing.T) {
	cases := []struct {
		name        string
		version     string
		libReplace  string
		installable bool
	}{
		{"тег без replace — можно go install", "v0.3.0", "", true},
		{"тег с replace — модуль только локальный", "v0.3.0", "../..", false},
		{"без тега", "dev", "", false},
		{"без тега, с replace", "dev", "../..", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := NewParams("github.com/acme/demo", "", c.libReplace, "", c.version)
			if err != nil {
				t.Fatalf("NewParams: %v", err)
			}
			if p.ScratchInstallable != c.installable {
				t.Errorf("ScratchInstallable = %v, want %v", p.ScratchInstallable, c.installable)
			}

			// Манифест должен восстанавливать тот же вывод, иначе
			// `scratch update` перерендерит Makefile другой веткой.
			restored := NewManifest(p).ToParams(c.version)
			if restored.ScratchInstallable != c.installable {
				t.Errorf("ToParams: ScratchInstallable = %v, want %v", restored.ScratchInstallable, c.installable)
			}
		})
	}
}

// TestRender_BootstrapBranch: каждая из трёх ветвей bootstrap добывает scratch
// своим способом, и ветки не смешиваются.
func TestRender_BootstrapBranch(t *testing.T) {
	render := func(t *testing.T, version, libReplace string) string {
		t.Helper()
		p, err := NewParams("github.com/acme/demo", "", libReplace, "", version)
		if err != nil {
			t.Fatalf("NewParams: %v", err)
		}
		files, err := Render(p)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		return string(files["Makefile"])
	}

	goInstall := "go install " + ScratchModule + "/cmd/scratch@"

	t.Run("тег без replace — go install", func(t *testing.T) {
		mk := render(t, "v0.3.0", "")
		if !strings.Contains(mk, goInstall+"v0.3.0") {
			t.Errorf("нет go install либы:\n%s", mk)
		}
	})

	t.Run("тег с replace — сборка из локальной копии", func(t *testing.T) {
		mk := render(t, "v0.3.0", "/opt/go-scratch")
		if strings.Contains(mk, goInstall) {
			t.Errorf("go install неопубликованного модуля:\n%s", mk)
		}
		if !strings.Contains(mk, "cd /opt/go-scratch && go build -o $(BIN_DIR)/scratch ./cmd/scratch") {
			t.Errorf("нет сборки scratch из replace-пути:\n%s", mk)
		}
	})

	t.Run("без тега и без replace — подсказка", func(t *testing.T) {
		mk := render(t, "dev", "")
		if strings.Contains(mk, goInstall) || strings.Contains(mk, "go build -o $(BIN_DIR)/scratch") {
			t.Errorf("добывать scratch неоткуда, а Makefile пытается:\n%s", mk)
		}
		if !strings.Contains(mk, "собери его локально") {
			t.Errorf("нет подсказки собрать scratch руками:\n%s", mk)
		}
	})
}

func TestRender_AllTemplates(t *testing.T) {
	files, err := Render(testParams(t))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Ключевые файлы каркаса присутствуют.
	for _, want := range []string{
		"go.mod",
		"Makefile",
		"buf.yaml",
		"buf.gen.yaml",
		".golangci.yml",
		".mockery.yaml",
		"Dockerfile",
		".dockerignore",
		"docker-compose.yml",
		".github/workflows/ci.yml",
		"api/proto/demoapi/v1/service.proto",
		"api/proto/google/api/annotations.proto",
		"api/proto/google/api/http.proto",
		"gen/embed.go",
		"cmd/demo-api/main.go",
		"internal/config/config.go",
		"internal/di/wire.go",
		"internal/di/providers.go",
		"internal/di/ditest/wire.go",
		"internal/di/ditest/providers.go",
		// Пакет-заглушка: без него go mod tidy в make generate уходит
		// искать <module>/internal/mocks в сети (mockery ещё не запускался).
		"internal/mocks/doc.go",
		"internal/repository/repository.go",
		"internal/server/demoapiservice/server.go",
		"internal/server/demoapiservice/echo.go",
		"internal/server/demoapiservice/stats.go",
		"internal/service/service.go",
		"internal/service/service_test.go",
		"README.md",
	} {
		if _, ok := files[want]; !ok {
			t.Errorf("не отрендерился файл %s", want)
		}
	}

	// Ни в одном файле не осталось незаменённых плейсхолдеров.
	for name, content := range files {
		s := string(content)
		if strings.Contains(s, "[[") || strings.Contains(name, "__") {
			t.Errorf("%s: остались незаменённые плейсхолдеры", name)
		}
	}

	// Managed-файлы действительно рендерятся (иначе update молча деградирует).
	for m := range managedFiles {
		if _, ok := files[m]; !ok {
			t.Errorf("managed-файл %s отсутствует в шаблонах", m)
		}
	}

	if !strings.Contains(string(files["go.mod"]), "module github.com/acme/demo-api") {
		t.Error("go.mod: нет module path")
	}
	if !strings.Contains(string(files["go.mod"]), "replace "+ScratchModule+" => ../..") {
		t.Error("go.mod: нет replace для --lib-replace")
	}
	if !strings.Contains(string(files["api/proto/demoapi/v1/service.proto"]), "service DemoApiService") {
		t.Error("service.proto: нет имени сервиса")
	}

	// SCRATCH должен раскрываться лениво: при `make bootstrap generate`
	// бинарник появляется в ./bin уже после чтения Makefile.
	if strings.Contains(string(files["Makefile"]), "SCRATCH :=") {
		t.Error("Makefile: SCRATCH := раскрывается на парсинге — make bootstrap generate не найдёт бинарник")
	}

	// .dockerignore обязан выкинуть ./bin (после bootstrap там сотни мегабайт
	// инструментов) и обязан НЕ выкинуть gen/ — Dockerfile не генерирует код.
	dockerignore := string(files[".dockerignore"])
	if !strings.Contains(dockerignore, "bin/") {
		t.Errorf(".dockerignore: ./bin не исключён — инструменты уедут в build context:\n%s", dockerignore)
	}
	for line := range strings.SplitSeq(dockerignore, "\n") {
		if s := strings.TrimSpace(line); s == "gen" || s == "gen/" {
			t.Error(".dockerignore: gen/ исключён — сборка в Docker не найдёт сгенерированный код")
		}
	}
}

// TestRender_GoFilesAreValid: раньше проверялось только наличие файлов и
// отсутствие незаменённых плейсхолдеров — опечатка в шаблоне доезжала до
// пользователя. Парсер ловит её мгновенно и без сети.
func TestRender_GoFilesAreValid(t *testing.T) {
	files, err := Render(testParams(t))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	checked := 0
	for name, content := range files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		checked++

		if _, err := parser.ParseFile(token.NewFileSet(), name, content, parser.SkipObjectResolution); err != nil {
			t.Errorf("%s: не разбирается как Go-код: %v", name, err)
			continue
		}

		formatted, err := format.Source(content)
		if err != nil {
			t.Errorf("%s: gofmt не смог обработать: %v", name, err)
			continue
		}
		if !bytes.Equal(formatted, content) {
			t.Errorf("%s: шаблон рендерится в неотформатированный код (нарушит `make lint` в проекте)", name)
		}
	}

	if checked == 0 {
		t.Fatal("не проверено ни одного .go — тест ничего не гарантирует")
	}
}

// TestRender_YAMLIsValid: сломанный отступ в шаблоне YAML иначе всплывёт
// только при запуске buf/mockery/docker в чужом проекте.
func TestRender_YAMLIsValid(t *testing.T) {
	files, err := Render(testParams(t))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	checked := 0
	for name, content := range files {
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		checked++

		var doc any
		if err := yaml.Unmarshal(content, &doc); err != nil {
			t.Errorf("%s: невалидный YAML: %v", name, err)
		}
	}

	if checked == 0 {
		t.Fatal("не проверено ни одного YAML — тест ничего не гарантирует")
	}
}

func TestGenerateAndUpdate(t *testing.T) {
	dir := t.TempDir()
	p := testParams(t)

	res, err := Generate(dir, p, false)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Files) == 0 {
		t.Fatal("Generate не создал файлов")
	}

	// Повторная генерация в непустую директорию без force — ошибка.
	if _, err := Generate(dir, p, false); err == nil {
		t.Error("ожидалась ошибка генерации в непустую директорию")
	}

	// Правим managed-файл руками и обновляемся: должен появиться конфликт.
	makefile := filepath.Join(dir, "Makefile")
	if err := os.WriteFile(makefile, []byte("# правка руками\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Update(dir, "v0.4.0")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if report.Files["Makefile"] != StatusConflict {
		t.Errorf("Makefile: статус %s, want %s", report.Files["Makefile"], StatusConflict)
	}
	if _, err := os.Stat(makefile + ".scratch-new"); err != nil {
		t.Error("нет Makefile.scratch-new при конфликте")
	}

	// Неизменённые managed-файлы просто перезаписываются без конфликта.
	if st := report.Files["buf.yaml"]; st != StatusUnchanged && st != StatusUpdated {
		t.Errorf("buf.yaml: статус %s, want unchanged/updated", st)
	}

	// go.mod: версия либы поднята.
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gomod), ScratchModule+" v0.4.0") {
		t.Errorf("go.mod: версия либы не поднята до v0.4.0:\n%s", gomod)
	}

	// Манифест обновлён.
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.ScratchVersion != "v0.4.0" {
		t.Errorf("manifest.ScratchVersion = %q, want v0.4.0", m.ScratchVersion)
	}
}

// TestUpdate_ReplaceOverridesBump: при живом replace бамп require ничего не
// меняет для сборки — отчёт не должен заявлять обновление либы состоявшимся.
func TestUpdate_ReplaceOverridesBump(t *testing.T) {
	dir := t.TempDir()
	// testParams уже с --lib-replace ../.., то есть replace попадёт в go.mod.
	if _, err := Generate(dir, testParams(t), false); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	report, err := Update(dir, "v0.4.0")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if report.LibBumped {
		t.Error("LibBumped = true при живом replace: отчёт заявляет обновление, которого сборка не увидит")
	}
	if !slices.ContainsFunc(report.Warnings, func(w string) bool { return strings.Contains(w, "replace") }) {
		t.Errorf("нет предупреждения про replace: %v", report.Warnings)
	}

	// require при этом всё равно поднимается — чтобы после снятия replace
	// проект оказался на новой версии.
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gomod), ScratchModule+" v0.4.0") {
		t.Errorf("require не поднят:\n%s", gomod)
	}

	// Без replace то же обновление проходит как состоявшееся.
	noReplace := t.TempDir()
	p := testParams(t)
	p.LibReplace = ""
	if _, err := Generate(noReplace, p, false); err != nil {
		t.Fatalf("Generate без replace: %v", err)
	}
	report, err = Update(noReplace, "v0.4.0")
	if err != nil {
		t.Fatalf("Update без replace: %v", err)
	}
	if !report.LibBumped {
		t.Errorf("LibBumped = false без replace, warnings: %v", report.Warnings)
	}
}

// TestLoadManifest_FutureSchema: манифест новее бинарника — внятная ошибка,
// а не молчаливая работа по чужой структуре.
func TestLoadManifest_FutureSchema(t *testing.T) {
	dir := t.TempDir()
	if _, err := Generate(dir, testParams(t), false); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	m.Schema = currentSchema + 1
	if err := m.Save(dir); err != nil {
		t.Fatal(err)
	}

	_, err = LoadManifest(dir)
	if err == nil {
		t.Fatal("ожидалась ошибка на манифесте будущей схемы")
	}
	if !strings.Contains(err.Error(), "schema") {
		t.Errorf("ошибка не объясняет причину: %v", err)
	}

	// Через Update та же ошибка должна доходить до пользователя.
	if _, err := Update(dir, "v0.4.0"); err == nil {
		t.Error("Update отработал на манифесте будущей схемы")
	}
}
