// Package tracing инициализирует OpenTelemetry-трассировку
// с экспортом по OTLP/gRPC (jaeger, otel-collector).
package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/kitos7/scratchy/internal/otelres"
	"github.com/kitos7/scratchy/pkg/config"
)

// ShutdownFunc останавливает провайдер трассировки, сбрасывая буферы.
type ShutdownFunc func(ctx context.Context) error

// Init настраивает глобальный TracerProvider и W3C-пропагацию контекста.
// Возвращённый ShutdownFunc нужно вызвать при остановке сервиса.
func Init(ctx context.Context, serviceName, environment string, cfg config.Tracing) (ShutdownFunc, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}

	res, err := otelres.New(serviceName, environment)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}
