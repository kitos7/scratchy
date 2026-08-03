package app_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/kitos7/scratchy/pkg/app"
	"github.com/kitos7/scratchy/pkg/config"
)

// freePort занимает порт на :0 и сразу отпускает. Гонка теоретически
// возможна, но app.Run принимает конкретные номера портов (в том числе
// чтобы gateway знал, куда дозваниваться), поэтому 0 не подходит.
func freePort(t *testing.T) int {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("занять порт: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	if err := lis.Close(); err != nil {
		t.Fatalf("освободить порт: %v", err)
	}
	return port
}

func testConfig(t *testing.T) config.App {
	t.Helper()

	return config.App{
		Name:            "test",
		Environment:     "test",
		GRPCPort:        freePort(t),
		HTTPPort:        freePort(t),
		DebugPort:       freePort(t),
		ShutdownTimeout: 5 * time.Second,
		Swagger:         config.Swagger{Enabled: false},
	}
}

// runApp поднимает приложение и возвращает функцию остановки, которая
// дожидается выхода из Run и проверяет, что он был чистым.
func runApp(t *testing.T, cfg config.App, opts ...app.Option) func() {
	t.Helper()

	log := slog.New(slog.DiscardHandler)
	a := app.New(cfg, log, opts...)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- a.Run(ctx) }()

	waitHealthy(t, cfg.DebugPort)

	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("Run вернул ошибку: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("Run не завершился после отмены контекста")
		}
	}
	t.Cleanup(stop)
	return stop
}

// waitHealthy ждёт, пока debug-сервер начнёт отвечать: это признак того,
// что Run дошёл до запуска серверов.
func waitHealthy(t *testing.T, debugPort int) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", debugPort))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("debug-сервер не поднялся за 15s")
}

func statusOf(t *testing.T, url string) int {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// Все три сервера поднимаются, /readyz становится 200, и Run завершается
// без ошибки по отмене контекста.
func TestRun_StartsAllServersAndShutsDownCleanly(t *testing.T) {
	cfg := testConfig(t)
	stop := runApp(t, cfg)

	if code := statusOf(t, fmt.Sprintf("http://127.0.0.1:%d/readyz", cfg.DebugPort)); code != http.StatusOK {
		t.Errorf("/readyz = %d, want 200: приложение не объявило себя готовым", code)
	}

	// gRPC-порт слушает.
	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", cfg.GRPCPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn.Connect()
	if !conn.WaitForStateChange(ctx, connectivity.Idle) {
		t.Error("gRPC-соединение не вышло из Idle")
	}

	// HTTP-gateway отвечает (маршрутов нет — важно, что сервер живой).
	if code := statusOf(t, fmt.Sprintf("http://127.0.0.1:%d/nope", cfg.HTTPPort)); code == 0 {
		t.Error("gateway не отвечает")
	}

	stop()

	// После остановки debug-порт больше не отвечает.
	if _, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.DebugPort)); err == nil {
		t.Error("debug-сервер отвечает после остановки")
	}
}

// WithHTTPMiddleware: без него единственный способ обвесить gateway —
// форкнуть pkg/app.
func TestWithHTTPMiddleware(t *testing.T) {
	cfg := testConfig(t)

	var outerSawInner atomic.Bool
	first := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-First", "1")
			next.ServeHTTP(w, r)
			// Порядок: первый добавленный — внешний.
			outerSawInner.Store(w.Header().Get("X-Second") == "1")
		})
	}
	second := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Second", "1")
			next.ServeHTTP(w, r)
		})
	}

	runApp(t, cfg, app.WithHTTPMiddleware(first, second))

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/nope", cfg.HTTPPort))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.Header.Get("X-First") != "1" || resp.Header.Get("X-Second") != "1" {
		t.Errorf("middleware не применились: %v", resp.Header)
	}
	if !outerSawInner.Load() {
		t.Error("порядок middleware нарушен: первый добавленный должен быть внешним")
	}
}

// WithGatewayOptions: проверяем через обработчик ошибок маршрутизации —
// он вызывается на любом незарегистрированном пути.
func TestWithGatewayOptions(t *testing.T) {
	cfg := testConfig(t)

	handler := runtime.WithRoutingErrorHandler(
		func(_ context.Context, _ *runtime.ServeMux, _ runtime.Marshaler, w http.ResponseWriter, _ *http.Request, _ int) {
			w.WriteHeader(http.StatusTeapot)
		})

	runApp(t, cfg, app.WithGatewayOptions(handler))

	if code := statusOf(t, fmt.Sprintf("http://127.0.0.1:%d/nope", cfg.HTTPPort)); code != http.StatusTeapot {
		t.Errorf("код = %d, want 418: ServeMuxOption не долетел до gateway", code)
	}
}

// Интерсепторы должны попадать в цепочки, а не игнорироваться.
// Стрим-цепочку проверяем через reflection: его ServerReflectionInfo —
// двунаправленный стрим, то есть проходит именно через stream-интерсепторы.
func TestInterceptorsAreInstalled(t *testing.T) {
	cfg := testConfig(t)

	var streamCalls atomic.Int64
	streamSpy := func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		streamCalls.Add(1)
		return handler(srv, ss)
	}
	unarySpy := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(ctx, req)
	}

	runApp(t, cfg,
		app.WithStreamInterceptors(streamSpy),
		app.WithUnaryInterceptors(unarySpy),
	)

	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", cfg.GRPCPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	desc := &grpc.StreamDesc{StreamName: "ServerReflectionInfo", ServerStreams: true, ClientStreams: true}
	stream, err := conn.NewStream(ctx, desc, "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo")
	if err != nil {
		t.Fatalf("открыть стрим к reflection: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("CloseSend: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for streamCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if streamCalls.Load() == 0 {
		t.Error("stream-интерсептор не вызван: ChainStreamInterceptor не подключён")
	}
}

// Swagger включается только вместе со спекой: иначе на debug-порте
// появится UI, которому нечего показывать.
func TestSwaggerServedOnDebugPort(t *testing.T) {
	cfg := testConfig(t)
	cfg.Swagger = config.Swagger{Enabled: true, TargetHost: "example:1234"}
	spec := []byte(`{"swagger":"2.0","info":{"title":"было"},"paths":{}}`)

	runApp(t, cfg, app.WithSwagger(spec))

	base := fmt.Sprintf("http://127.0.0.1:%d", cfg.DebugPort)
	if code := statusOf(t, base+"/swagger/"); code != http.StatusOK {
		t.Errorf("/swagger/ = %d, want 200", code)
	}

	resp, err := http.Get(base + "/swagger.json")
	if err != nil {
		t.Fatalf("GET /swagger.json: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("чтение спеки: %v", err)
	}
	// TargetHost из конфига должен доехать до спеки, иначе Try it out
	// пойдёт на debug-порт вместо публичного HTTP.
	if !strings.Contains(string(body), `"host": "example:1234"`) {
		t.Errorf("host из конфига не попал в спеку:\n%s", body)
	}
}

// Спека не передана — swagger-ручек нет, даже если он включён в конфиге.
func TestSwaggerAbsentWithoutSpec(t *testing.T) {
	cfg := testConfig(t)
	cfg.Swagger = config.Swagger{Enabled: true}

	runApp(t, cfg)

	if code := statusOf(t, fmt.Sprintf("http://127.0.0.1:%d/swagger/", cfg.DebugPort)); code != http.StatusNotFound {
		t.Errorf("/swagger/ = %d, want 404", code)
	}
}
