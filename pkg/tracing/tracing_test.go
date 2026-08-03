package tracing_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/kitos7/scratchy/pkg/config"
	"github.com/kitos7/scratchy/pkg/tracing"
)

// Пропагация нужна всегда: даже с выключенной трассировкой сервис обязан
// пробрасывать чужой traceparent дальше, иначе цепочка рвётся на нём.
func TestInit_SetsPropagatorEvenWhenDisabled(t *testing.T) {
	if _, err := tracing.Init(context.Background(), "svc", "test", config.Tracing{Enabled: false}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	fields := otel.GetTextMapPropagator().Fields()
	for _, want := range []string{"traceparent", "baggage"} {
		if !slices.Contains(fields, want) {
			t.Errorf("пропагатор не несёт %s: %v", want, fields)
		}
	}
}

func TestInit_DisabledReturnsNoopShutdown(t *testing.T) {
	shutdown, err := tracing.Init(context.Background(), "svc", "test", config.Tracing{Enabled: false})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown = nil: вызов в main упадёт")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown при выключенной трассировке: %v", err)
	}

	// Провайдер не подменялся — спаны никуда не пишутся.
	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); ok {
		t.Error("при Enabled=false установлен SDK-провайдер")
	}
}

// Enabled=true: экспортёр OTLP создаётся лениво, поэтому Init не должен
// падать из-за недоступного коллектора — иначе сервис не поднимется без jaeger.
func TestInit_EnabledWithUnreachableCollector(t *testing.T) {
	cfg := config.Tracing{
		Enabled:     true,
		Endpoint:    "127.0.0.1:1", // никто не слушает
		Insecure:    true,
		SampleRatio: 1.0,
	}

	shutdown, err := tracing.Init(context.Background(), "svc", "test", cfg)
	if err != nil {
		t.Fatalf("Init с недоступным коллектором должен проходить: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	})

	tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	if !ok {
		t.Fatalf("глобальный провайдер не заменён на SDK: %T", otel.GetTracerProvider())
	}

	// Спан создаётся и получает валидный контекст — именно его подхватывает
	// pkg/logging для trace_id.
	_, span := tp.Tracer("test").Start(context.Background(), "op")
	sc := span.SpanContext()
	span.End()

	if !sc.IsValid() {
		t.Error("span context невалиден — trace_id в логах не появится")
	}
	if !sc.IsSampled() {
		t.Error("при SampleRatio=1.0 спан должен быть сэмплирован")
	}
}

func TestInit_ZeroSampleRatioDropsSpans(t *testing.T) {
	cfg := config.Tracing{Enabled: true, Endpoint: "127.0.0.1:1", Insecure: true, SampleRatio: 0}

	shutdown, err := tracing.Init(context.Background(), "svc", "test", cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	})

	tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	if !ok {
		t.Fatalf("глобальный провайдер не заменён: %T", otel.GetTracerProvider())
	}

	_, span := tp.Tracer("test").Start(context.Background(), "op")
	sampled := span.SpanContext().IsSampled()
	span.End()

	if sampled {
		t.Error("при SampleRatio=0 корневой спан не должен сэмплироваться")
	}
}
