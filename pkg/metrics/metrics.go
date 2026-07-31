// Package metrics включает метрики OpenTelemetry с экспортом в Prometheus.
//
// Своих измерений пакет не вводит: инструментирование уже стоит в рантайме
// (otelgrpc на gRPC-сервере, otelhttp на gateway) и молчит только потому,
// что не задан MeterProvider. Init его задаёт — и /metrics на debug-порте
// начинает отдавать длительности и счётчики RPC, включая стриминговые.
package metrics

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/nikita/scratch/internal/otelres"
	"github.com/nikita/scratch/pkg/config"
)

// ShutdownFunc останавливает провайдер метрик.
type ShutdownFunc func(ctx context.Context) error

// Init настраивает глобальный MeterProvider с Prometheus-экспортёром.
// Экспортёр регистрируется в prometheus.DefaultRegisterer — том самом,
// который отдаёт promhttp.Handler на debug-порте (см. pkg/debug).
// Возвращённый ShutdownFunc нужно вызвать при остановке сервиса.
func Init(serviceName, environment string, cfg config.Metrics) (ShutdownFunc, error) {
	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otelprom.New()
	if err != nil {
		return nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	res, err := otelres.New(serviceName, environment)
	if err != nil {
		return nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return mp.Shutdown, nil
}
