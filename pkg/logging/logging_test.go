package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/nikita/scratch/pkg/config"
)

// ctxWithSpan собирает контекст с валидным span context, не поднимая
// настоящий TracerProvider.
func ctxWithSpan(t *testing.T) (context.Context, string, string) {
	t.Helper()

	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("TraceIDFromHex: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("1112131415161718")
	if err != nil {
		t.Fatalf("SpanIDFromHex: %v", err)
	}

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithSpanContext(context.Background(), sc), traceID.String(), spanID.String()
}

// logLine парсит единственную JSON-запись из буфера.
func logLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Fatal("лог пуст")
	}
	if strings.Contains(out, "\n") {
		t.Fatalf("ожидалась одна запись, получено:\n%s", out)
	}

	var rec map[string]any
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("запись не JSON (%v):\n%s", err, out)
	}
	return rec
}

func TestNewWithWriter_TraceIDFromContext(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, config.Log{Level: "info", Format: "json"})

	ctx, wantTrace, wantSpan := ctxWithSpan(t)
	log.InfoContext(ctx, "hello")

	rec := logLine(t, &buf)
	if rec["trace_id"] != wantTrace {
		t.Errorf("trace_id = %v, want %s", rec["trace_id"], wantTrace)
	}
	if rec["span_id"] != wantSpan {
		t.Errorf("span_id = %v, want %s", rec["span_id"], wantSpan)
	}
	if rec["msg"] != "hello" {
		t.Errorf("msg = %v, want hello", rec["msg"])
	}
}

func TestNewWithWriter_NoSpanNoTraceID(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, config.Log{Level: "info", Format: "json"})

	log.InfoContext(context.Background(), "hello")

	rec := logLine(t, &buf)
	if _, ok := rec["trace_id"]; ok {
		t.Errorf("без активного спана trace_id не должен появляться: %v", rec)
	}
}

// Обёртки WithAttrs/WithGroup написаны руками — легко потерять
// инъекцию trace_id в цепочке.
func TestNewWithWriter_TraceIDSurvivesWrapping(t *testing.T) {
	ctx, wantTrace, _ := ctxWithSpan(t)

	t.Run("With", func(t *testing.T) {
		var buf bytes.Buffer
		log := NewWithWriter(&buf, config.Log{Format: "json"}).With("component", "svc")
		log.InfoContext(ctx, "hello")

		rec := logLine(t, &buf)
		if rec["trace_id"] != wantTrace {
			t.Errorf("после With потерян trace_id: %v", rec)
		}
		if rec["component"] != "svc" {
			t.Errorf("после With потерян атрибут: %v", rec)
		}
	})

	t.Run("WithGroup", func(t *testing.T) {
		var buf bytes.Buffer
		log := NewWithWriter(&buf, config.Log{Format: "json"}).WithGroup("req").With("id", "42")
		log.InfoContext(ctx, "hello")

		rec := logLine(t, &buf)
		// trace_id добавляется на уровне записи, а не внутрь группы.
		if rec["trace_id"] != wantTrace {
			t.Errorf("после WithGroup потерян trace_id: %v", rec)
		}
		group, ok := rec["req"].(map[string]any)
		if !ok || group["id"] != "42" {
			t.Errorf("группа собрана неверно: %v", rec)
		}
		if _, nested := group["trace_id"]; nested {
			t.Errorf("trace_id внутри группы — поиск по нему сломается: %v", rec)
		}
	})

	// Чередование: атрибуты до группы остаются наверху, после — внутри,
	// trace_id всегда наверху.
	t.Run("With + WithGroup + With", func(t *testing.T) {
		var buf bytes.Buffer
		log := NewWithWriter(&buf, config.Log{Format: "json"}).
			With("outer", "1").WithGroup("g").With("inner", "2")
		log.InfoContext(ctx, "hello")

		rec := logLine(t, &buf)
		if rec["trace_id"] != wantTrace {
			t.Errorf("trace_id не на верхнем уровне: %v", rec)
		}
		if rec["outer"] != "1" {
			t.Errorf("атрибут до группы должен быть наверху: %v", rec)
		}
		group, ok := rec["g"].(map[string]any)
		if !ok || group["inner"] != "2" {
			t.Errorf("атрибут после группы должен быть внутри неё: %v", rec)
		}
		if _, leaked := rec["inner"]; leaked {
			t.Errorf("атрибут группы утёк наверх: %v", rec)
		}
	})

	// Вложенные группы: цепочка проигрывается целиком.
	t.Run("вложенные группы", func(t *testing.T) {
		var buf bytes.Buffer
		log := NewWithWriter(&buf, config.Log{Format: "json"}).
			WithGroup("a").WithGroup("b").With("id", "7")
		log.InfoContext(ctx, "hello")

		rec := logLine(t, &buf)
		if rec["trace_id"] != wantTrace {
			t.Errorf("trace_id не на верхнем уровне: %v", rec)
		}
		outer, ok := rec["a"].(map[string]any)
		if !ok {
			t.Fatalf("нет внешней группы: %v", rec)
		}
		inner, ok := outer["b"].(map[string]any)
		if !ok || inner["id"] != "7" {
			t.Errorf("вложенная группа собрана неверно: %v", rec)
		}
	})
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"":        slog.LevelInfo,
		"мусор":   slog.LevelInfo,
	}
	for in, want := range cases {
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNewWithWriter_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, config.Log{Level: "warn", Format: "json"})

	log.Info("не должно попасть в лог")
	if buf.Len() != 0 {
		t.Errorf("info прошёл при уровне warn: %s", buf.String())
	}

	log.Warn("должно попасть")
	if rec := logLine(t, &buf); rec["msg"] != "должно попасть" {
		t.Errorf("warn не попал в лог: %v", rec)
	}
}

func TestNewWithWriter_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, config.Log{Format: "text"})

	ctx, wantTrace, _ := ctxWithSpan(t)
	log.InfoContext(ctx, "hello")

	out := buf.String()
	if !strings.Contains(out, "msg=hello") {
		t.Errorf("не text-формат: %s", out)
	}
	if !strings.Contains(out, "trace_id="+wantTrace) {
		t.Errorf("в text-формате нет trace_id: %s", out)
	}
}

// New отличается от NewWithWriter только тем, что пишет в stdout и
// становится дефолтным логгером.
func TestNew_SetsDefault(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	log := New(config.Log{Format: "json"})
	if slog.Default() != log {
		t.Error("New не установил логгер дефолтным")
	}
}
