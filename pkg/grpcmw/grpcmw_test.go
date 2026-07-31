package grpcmw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// testLogger отдаёт логгер и разбор его единственной JSON-записи.
func testLogger(t *testing.T) (*slog.Logger, func() map[string]any) {
	t.Helper()

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	return log, func() map[string]any {
		t.Helper()
		out := strings.TrimSpace(buf.String())
		if out == "" {
			t.Fatal("интерсептор ничего не записал в лог")
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(strings.Split(out, "\n")[0]), &rec); err != nil {
			t.Fatalf("запись не JSON (%v):\n%s", err, out)
		}
		return rec
	}
}

// fakeStream — минимальный grpc.ServerStream: интерсепторам нужен только Context.
type fakeStream struct{ ctx context.Context }

func (f fakeStream) SetHeader(metadata.MD) error  { return nil }
func (f fakeStream) SendHeader(metadata.MD) error { return nil }
func (f fakeStream) SetTrailer(metadata.MD)       {}
func (f fakeStream) Context() context.Context     { return f.ctx }
func (f fakeStream) SendMsg(any) error            { return nil }
func (f fakeStream) RecvMsg(any) error            { return nil }

func TestLogging(t *testing.T) {
	t.Run("успех — info и код OK", func(t *testing.T) {
		log, read := testLogger(t)
		info := &grpc.UnaryServerInfo{FullMethod: "/demo.v1.DemoService/Echo"}

		resp, err := Logging(log)(context.Background(), "req", info,
			func(context.Context, any) (any, error) { return "resp", nil })

		if err != nil || resp != "resp" {
			t.Fatalf("ответ хендлера не проброшен: %v, %v", resp, err)
		}
		rec := read()
		if rec["level"] != "INFO" {
			t.Errorf("level = %v, want INFO", rec["level"])
		}
		if rec["code"] != codes.OK.String() {
			t.Errorf("code = %v, want %s", rec["code"], codes.OK)
		}
		if rec["method"] != info.FullMethod {
			t.Errorf("method = %v, want %s", rec["method"], info.FullMethod)
		}
		if _, ok := rec["duration"]; !ok {
			t.Errorf("нет длительности вызова: %v", rec)
		}
	})

	t.Run("ошибка — error и код из status", func(t *testing.T) {
		log, read := testLogger(t)
		want := status.Error(codes.NotFound, "нет такого")

		_, err := Logging(log)(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/m"},
			func(context.Context, any) (any, error) { return nil, want })

		if !errors.Is(err, want) {
			t.Fatalf("ошибка хендлера не проброшена: %v", err)
		}
		rec := read()
		if rec["level"] != "ERROR" {
			t.Errorf("level = %v, want ERROR", rec["level"])
		}
		if rec["code"] != codes.NotFound.String() {
			t.Errorf("code = %v, want %s", rec["code"], codes.NotFound)
		}
		if !strings.Contains(rec["error"].(string), "нет такого") {
			t.Errorf("в логе нет текста ошибки: %v", rec)
		}
	})
}

func TestRecovery(t *testing.T) {
	log, read := testLogger(t)

	resp, err := Recovery(log)(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/m"},
		func(context.Context, any) (any, error) { panic("бум") })

	if resp != nil {
		t.Errorf("при панике ответ должен быть nil, получено %v", resp)
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("код = %s, want %s", status.Code(err), codes.Internal)
	}
	// Наружу не должно утечь содержимое паники.
	if strings.Contains(status.Convert(err).Message(), "бум") {
		t.Errorf("текст паники утёк клиенту: %v", err)
	}

	rec := read()
	if rec["msg"] != "panic recovered" {
		t.Errorf("паника не залогирована: %v", rec)
	}
	if rec["panic"] != "бум" {
		t.Errorf("в логе нет значения паники: %v", rec)
	}
	if stack, ok := rec["stack"].(string); !ok || stack == "" {
		t.Errorf("в логе нет стека: %v", rec)
	}
}

func TestLoggingStream(t *testing.T) {
	cases := []struct {
		name       string
		info       *grpc.StreamServerInfo
		wantStream string
	}{
		{"клиентский", &grpc.StreamServerInfo{FullMethod: "/m", IsClientStream: true}, "client"},
		{"серверный", &grpc.StreamServerInfo{FullMethod: "/m", IsServerStream: true}, "server"},
		{"двунаправленный", &grpc.StreamServerInfo{FullMethod: "/m", IsClientStream: true, IsServerStream: true}, "bidi"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log, read := testLogger(t)

			err := LoggingStream(log)(nil, fakeStream{ctx: context.Background()}, c.info,
				func(any, grpc.ServerStream) error { return nil })
			if err != nil {
				t.Fatalf("ошибка: %v", err)
			}

			rec := read()
			if rec["msg"] != "grpc stream" {
				t.Errorf("msg = %v, want 'grpc stream'", rec["msg"])
			}
			if rec["stream"] != c.wantStream {
				t.Errorf("stream = %v, want %s", rec["stream"], c.wantStream)
			}
			if rec["code"] != codes.OK.String() {
				t.Errorf("code = %v, want OK", rec["code"])
			}
		})
	}

	t.Run("ошибка стрима — error", func(t *testing.T) {
		log, read := testLogger(t)

		err := LoggingStream(log)(nil, fakeStream{ctx: context.Background()},
			&grpc.StreamServerInfo{FullMethod: "/m", IsServerStream: true},
			func(any, grpc.ServerStream) error { return status.Error(codes.Aborted, "оборвался") })

		if status.Code(err) != codes.Aborted {
			t.Fatalf("код = %s, want Aborted", status.Code(err))
		}
		if rec := read(); rec["level"] != "ERROR" {
			t.Errorf("level = %v, want ERROR", rec["level"])
		}
	})
}

// Ключевой тест: до появления RecoveryStream паника в стриминговой ручке
// роняла весь процесс.
func TestRecoveryStream(t *testing.T) {
	log, read := testLogger(t)

	err := RecoveryStream(log)(nil, fakeStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: "/m", IsServerStream: true},
		func(any, grpc.ServerStream) error { panic("бум в стриме") })

	if status.Code(err) != codes.Internal {
		t.Fatalf("код = %s, want %s", status.Code(err), codes.Internal)
	}
	if strings.Contains(status.Convert(err).Message(), "бум") {
		t.Errorf("текст паники утёк клиенту: %v", err)
	}
	if rec := read(); rec["panic"] != "бум в стриме" {
		t.Errorf("паника стрима не залогирована: %v", rec)
	}
}

func TestStreamKind(t *testing.T) {
	if got := streamKind(&grpc.StreamServerInfo{}); got != "unary" {
		t.Errorf("streamKind без флагов = %q, want unary", got)
	}
}
