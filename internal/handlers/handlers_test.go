package handlers

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kitos7/scratchy/internal/generator"
)

// extraProto — второй сервис в том же контракте: unary, оба вида стриминга
// и ручка с типом из чужого proto-пакета.
const extraProto = `
service DemoServiceV2 {
  rpc Ping(PingRequest) returns (PingResponse);
  rpc WatchEchoes(WatchRequest) returns (stream EchoResponse);
  rpc UploadEchoes(stream EchoRequest) returns (StatsResponse);
  rpc Health(google.protobuf.Empty) returns (google.protobuf.Empty);
}

message PingRequest {}
message PingResponse { string pong = 1; }
message WatchRequest {}
`

// newProject генерирует проект-скелет и дописывает в контракт второй сервис.
func newProject(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	p, err := generator.NewParams("github.com/acme/demo", "", "", "", "v0.1.0")
	if err != nil {
		t.Fatalf("NewParams: %v", err)
	}
	if _, err := generator.Generate(dir, p, false); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	protoPath := filepath.Join(dir, "api", "proto", "demo", "v1", "service.proto")
	f, err := os.OpenFile(protoPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("открыть контракт: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(extraProto); err != nil {
		t.Fatalf("дописать контракт: %v", err)
	}
	return dir
}

func TestGenerate_NewService(t *testing.T) {
	dir := newProject(t)

	report, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	for _, want := range []string{
		"internal/server/demoservicev2/server.go",
		"internal/server/demoservicev2/ping.go",
		"internal/server/demoservicev2/watch_echoes.go",
		"internal/server/demoservicev2/upload_echoes.go",
	} {
		if !slices.Contains(report.Created, want) {
			t.Errorf("не создан %s (создано: %v)", want, report.Created)
		}
	}

	// Ручки скелета уже реализованы — их не трогаем.
	for _, want := range []string{"DemoService.Echo", "DemoService.Stats"} {
		if !slices.Contains(report.Existing, want) {
			t.Errorf("%s должен считаться реализованным (existing: %v)", want, report.Existing)
		}
	}
	if slices.ContainsFunc(report.Created, func(p string) bool {
		return strings.HasPrefix(p, "internal/server/demoservice/")
	}) {
		t.Errorf("пакет скелета не должен меняться: %v", report.Created)
	}

	// Ручка с чужими типами — предупреждение, а не файл.
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "Health") {
		t.Errorf("ожидалось предупреждение про Health, получено: %v", report.Warnings)
	}

	if len(report.NewServices) != 1 || report.NewServices[0].Name != "DemoServiceV2" {
		t.Fatalf("ожидался один новый сервис DemoServiceV2, получено: %+v", report.NewServices)
	}
	if hint := DIHint(report.NewServices[0]); !strings.Contains(hint, "RegisterDemoServiceV2Server") {
		t.Errorf("подсказка для DI без регистрации сервиса:\n%s", hint)
	}
}

// separateProto — сервис в отдельном proto-пакете (api/proto/audit), не в том,
// что задан при скаффолдинге: такие контракты тоже должны попадать в генерацию.
const separateProto = `syntax = "proto3";

package audit.v1;

option go_package = "github.com/acme/demo/gen/audit/v1;auditv1";

service AuditService {
  rpc Log(LogRequest) returns (LogResponse);
}

message LogRequest { string entry = 1; }
message LogResponse {}
`

func TestGenerate_ServiceInSeparatePackage(t *testing.T) {
	dir := newProject(t)

	protoPath := filepath.Join(dir, "api", "proto", "audit", "v1", "service.proto")
	if err := os.MkdirAll(filepath.Dir(protoPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(protoPath, []byte(separateProto), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	for _, want := range []string{
		"internal/server/auditservice/server.go",
		"internal/server/auditservice/log.go",
	} {
		if !slices.Contains(report.Created, want) {
			t.Errorf("не создан %s (создано: %v)", want, report.Created)
		}
	}
	if !slices.ContainsFunc(report.NewServices, func(s Service) bool { return s.Name == "AuditService" }) {
		t.Errorf("AuditService не попал в новые сервисы: %+v", report.NewServices)
	}

	// Пакет нового сервиса ссылается на свой pb-пакет, а не на пакет скелета.
	server := readFile(t, filepath.Join(dir, "internal", "server", "auditservice", "server.go"))
	for _, want := range []string{
		"github.com/acme/demo/gen/audit/v1",
		"auditv1.UnimplementedAuditServiceServer",
	} {
		if !strings.Contains(server, want) {
			t.Errorf("server.go: нет %q\n%s", want, server)
		}
	}
}

func TestGenerate_Signatures(t *testing.T) {
	dir := newProject(t)
	if _, err := Generate(dir); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	cases := map[string]string{
		"ping.go":          "func (s *Server) Ping(ctx context.Context, req *demov1.PingRequest) (*demov1.PingResponse, error)",
		"watch_echoes.go":  "func (s *Server) WatchEchoes(req *demov1.WatchRequest, stream demov1.DemoServiceV2_WatchEchoesServer) error",
		"upload_echoes.go": "func (s *Server) UploadEchoes(stream demov1.DemoServiceV2_UploadEchoesServer) error",
	}
	for file, want := range cases {
		content := readFile(t, filepath.Join(dir, "internal", "server", "demoservicev2", file))
		if !strings.Contains(content, want) {
			t.Errorf("%s: нет сигнатуры\n  want: %s\n  got:\n%s", file, want, content)
		}
		if !strings.Contains(content, "codes.Unimplemented") {
			t.Errorf("%s: заготовка должна отвечать Unimplemented", file)
		}
	}

	// Пакет нового сервиса собран целиком: тип, конструктор, Unimplemented-встройка.
	server := readFile(t, filepath.Join(dir, "internal", "server", "demoservicev2", "server.go"))
	for _, want := range []string{
		"package demoservicev2",
		"demov1.UnimplementedDemoServiceV2Server",
		"func New(svc *service.Service) *Server",
	} {
		if !strings.Contains(server, want) {
			t.Errorf("server.go: нет %q\n%s", want, server)
		}
	}
}

func TestGenerate_Idempotent(t *testing.T) {
	dir := newProject(t)
	if _, err := Generate(dir); err != nil {
		t.Fatalf("первый Generate: %v", err)
	}

	pingPath := filepath.Join(dir, "internal", "server", "demoservicev2", "ping.go")
	before := readFile(t, pingPath)

	report, err := Generate(dir)
	if err != nil {
		t.Fatalf("второй Generate: %v", err)
	}
	if len(report.Created) != 0 {
		t.Errorf("повторный прогон создал файлы: %v", report.Created)
	}
	if after := readFile(t, pingPath); after != before {
		t.Error("повторный прогон переписал существующий файл")
	}
}

func TestGenerate_MethodInOtherFile(t *testing.T) {
	dir := newProject(t)
	if _, err := Generate(dir); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Разработчик перенёс ручку в другой файл: дубликата быть не должно.
	pkgDir := filepath.Join(dir, "internal", "server", "demoservicev2")
	ping := readFile(t, filepath.Join(pkgDir, "ping.go"))
	if err := os.Remove(filepath.Join(pkgDir, "ping.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "rpc_ping.go"), []byte(ping), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(report.Created) != 0 {
		t.Errorf("ручка уже реализована в другом файле, а создано: %v", report.Created)
	}
	if !slices.Contains(report.Existing, "DemoServiceV2.Ping") {
		t.Errorf("Ping должен считаться реализованным, existing: %v", report.Existing)
	}
}

func TestGenerate_FileExistsWithoutMethod(t *testing.T) {
	dir := newProject(t)
	pkgDir := filepath.Join(dir, "internal", "server", "demoservicev2")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Файл занят чем-то посторонним — перезаписывать нельзя.
	if err := os.WriteFile(filepath.Join(pkgDir, "ping.go"), []byte("package demoservicev2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if slices.Contains(report.Created, "internal/server/demoservicev2/ping.go") {
		t.Error("существующий ping.go перезаписан")
	}
	if !slices.ContainsFunc(report.Warnings, func(w string) bool { return strings.Contains(w, "Ping") }) {
		t.Errorf("нет предупреждения про занятый файл: %v", report.Warnings)
	}
}

func TestSnakeAndGoPackage(t *testing.T) {
	for in, want := range map[string]string{
		"Echo":        "echo",
		"ListUsers":   "list_users",
		"HTTPGet":     "http_get",
		"ListUsersV2": "list_users_v2",
	} {
		if got := snake(in); got != want {
			t.Errorf("snake(%q) = %q, want %q", in, got, want)
		}
	}

	imp, alias := splitGoPackage("github.com/acme/demo/gen/demo/v1;demov1")
	if imp != "github.com/acme/demo/gen/demo/v1" || alias != "demov1" {
		t.Errorf("splitGoPackage = %q, %q", imp, alias)
	}
	if imp, alias := splitGoPackage("github.com/acme/demo/gen/demo/v1"); alias != "v1" || imp == "" {
		t.Errorf("splitGoPackage без алиаса = %q, %q", imp, alias)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("читать %s: %v", path, err)
	}
	return string(b)
}
