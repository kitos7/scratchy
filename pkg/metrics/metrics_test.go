package metrics_test

import (
	"context"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/kitos7/scratchy/pkg/config"
	"github.com/kitos7/scratchy/pkg/metrics"
)

func TestInit_DisabledReturnsNoopShutdown(t *testing.T) {
	shutdown, err := metrics.Init("svc", "test", config.Metrics{Enabled: false})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown = nil: вызов в main упадёт")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown при выключенных метриках: %v", err)
	}
	if _, ok := otel.GetMeterProvider().(*sdkmetric.MeterProvider); ok {
		t.Error("при Enabled=false установлен SDK-провайдер метрик")
	}
}

// Главное, что должен обеспечить пакет: измерения через глобальный
// MeterProvider доезжают до того же registry, который отдаёт /metrics
// на debug-порте (prometheus.DefaultRegisterer / DefaultGatherer).
func TestInit_MetricsReachDefaultRegistry(t *testing.T) {
	shutdown, err := metrics.Init("billing", "test", config.Metrics{Enabled: true})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	if _, ok := otel.GetMeterProvider().(*sdkmetric.MeterProvider); !ok {
		t.Fatalf("глобальный MeterProvider не заменён: %T", otel.GetMeterProvider())
	}

	// Измерение делается через глобальный provider — ровно так, как это
	// делают otelgrpc и otelhttp внутри рантайма.
	counter, err := otel.Meter("test").Int64Counter("scratch_probe_total")
	if err != nil {
		t.Fatalf("Int64Counter: %v", err)
	}
	counter.Add(context.Background(), 3)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	var names []string
	found := false
	for _, f := range families {
		names = append(names, f.GetName())
		if strings.Contains(f.GetName(), "scratch_probe") {
			found = true
		}
	}
	if !found {
		t.Errorf("метрика не доехала до DefaultGatherer — /metrics её не покажет.\nсобрано: %v", names)
	}
}
