//go:build e2e

// Сквозная проверка главного обещания генератора: сгенерированный проект
// собирается и проходит свои тесты. Требует сети (make bootstrap тянет buf,
// protoc-плагины, wire, mockery, линтер) и потому вынесена под тег e2e:
//
//	make e2e
package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// repoRoot — корень go-scratch: на него ставится replace, из него же
// `make bootstrap` собирает бинарник scratch для шага handlers.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к тесту")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatalf("абсолютный путь к корню: %v", err)
	}
	return root
}

// runMake выполняет цель make в директории проекта.
func runMake(t *testing.T, dir string, target ...string) {
	t.Helper()

	start := time.Now()
	cmd := exec.Command("make", target...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	label := "make " + strings.Join(target, " ")
	if err != nil {
		t.Fatalf("%s провалился (%v):\n%s", label, err, out)
	}
	t.Logf("%s — ок за %s", label, time.Since(start).Round(time.Second))
}

// writeAuditProto кладёт в проект контракт в отдельном proto-пакете —
// регрессия: до фикса buf generate и scratch handlers видели только
// api/proto/<package> и молча пропускали остальные сервисы.
func writeAuditProto(t *testing.T, dir string) {
	t.Helper()

	const auditProto = `syntax = "proto3";

package audit.v1;

import "google/api/annotations.proto";

option go_package = "github.com/acme/e2edemo/gen/audit/v1;auditv1";

service AuditService {
  rpc Log(LogRequest) returns (LogResponse) {
    option (google.api.http) = {
      post: "/v1/audit/log"
      body: "*"
    };
  }
}

message LogRequest {
  string entry = 1;
}

message LogResponse {}
`
	path := filepath.Join(dir, "api", "proto", "audit", "v1", "service.proto")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(auditProto), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestE2E_GeneratedProjectBuilds проходит путь пользователя целиком:
// new → bootstrap → generate → test → build.
func TestE2E_GeneratedProjectBuilds(t *testing.T) {
	dir := t.TempDir()

	params, err := NewParams("github.com/acme/e2edemo", "", repoRoot(t), "", "v0.1.0")
	if err != nil {
		t.Fatalf("NewParams: %v", err)
	}
	// Либа подключена через replace, значит из сети она не резолвится и
	// bootstrap обязан собрать scratch из локальной копии.
	if params.ScratchInstallable {
		t.Fatal("с --lib-replace ScratchInstallable должен быть false")
	}

	res, err := Generate(dir, params, false)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	t.Logf("сгенерировано %d файлов в %s", len(res.Files), dir)

	// Второй proto-пакет рядом со скелетным: buf и scratch handlers должны
	// видеть все контракты, а не только пакет, заданный при скаффолдинге.
	writeAuditProto(t, dir)

	// Порядок как в README: инструменты, кодогенерация, тесты, сборка.
	runMake(t, dir, "bootstrap")
	runMake(t, dir, "generate")
	runMake(t, dir, "test")
	runMake(t, dir, "build")

	// После generate артефакты кодогенерации должны существовать: без них
	// проект бы не собрался, но лучше сказать прямо, чего не хватает.
	for _, rel := range []string{
		"gen/e2edemo/v1/service.pb.go",
		"gen/e2edemo/v1/service_grpc.pb.go",
		"gen/e2edemo/v1/service.pb.gw.go",
		"gen/audit/v1/service.pb.go",
		"gen/audit/v1/service_grpc.pb.go",
		"gen/audit/v1/service.pb.gw.go",
		"internal/server/auditservice/server.go",
		"internal/server/auditservice/log.go",
		"gen/api.swagger.json",
		"internal/mocks/Repository.go",
		"internal/di/wire_gen.go",
		"internal/di/ditest/wire_gen.go",
		"bin/e2edemo",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("после make generate/build нет %s: %v", rel, err)
		}
	}

	// Объединённый сваггер должен включать оба сервиса: со strategy: directory
	// buf запускал бы openapiv2 по каталогам и второй api.swagger.json терялся.
	swagger, err := os.ReadFile(filepath.Join(dir, "gen", "api.swagger.json"))
	if err != nil {
		t.Fatalf("читать api.swagger.json: %v", err)
	}
	// Имя сервиса скелета — E2Edemo, не E2edemo: GoCamelCase поднимает
	// букву после цифры (см. комментарий к GoCamelCase).
	for _, op := range []string{"E2EdemoService_Echo", "AuditService_Log"} {
		if !strings.Contains(string(swagger), op) {
			t.Errorf("в api.swagger.json нет операции %s", op)
		}
	}

	// Повторный прогон обязан быть идемпотентным: CI сгенерированного
	// проекта проверяет это через `git diff --exit-code`.
	runMake(t, dir, "generate")
	runMake(t, dir, "build")
}

// TestE2E_BootstrapThenGenerateInOneMake ловит регрессию `SCRATCH :=`:
// при раскрытии на этапе парсинга цель handlers не находит бинарник,
// который bootstrap положил в ./bin в этом же прогоне.
func TestE2E_BootstrapThenGenerateInOneMake(t *testing.T) {
	dir := t.TempDir()

	params, err := NewParams("github.com/acme/onemake", "", repoRoot(t), "", "v0.1.0")
	if err != nil {
		t.Fatalf("NewParams: %v", err)
	}
	if _, err := Generate(dir, params, false); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	runMake(t, dir, "bootstrap", "generate")
}
