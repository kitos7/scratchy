// Package otelres собирает OTel-ресурс сервиса. Трейсы и метрики обязаны
// описывать себя одинаково — иначе их не сматчить в бэкенде, поэтому
// атрибуты строятся в одном месте.
package otelres

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

// New собирает ресурс с именем сервиса и окружением поверх дефолтного.
func New(serviceName, environment string) (*resource.Resource, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", serviceName),
		attribute.String("deployment.environment", environment),
	))
	if err != nil {
		return nil, fmt.Errorf("build resource: %w", err)
	}
	return res, nil
}
