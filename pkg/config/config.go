// Package config предоставляет базовую конфигурацию сервиса,
// загружаемую из переменных окружения.
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

// App — базовая конфигурация, которую встраивает каждый scratch-сервис.
type App struct {
	Name        string `env:"APP_NAME" envDefault:"app"`
	Environment string `env:"APP_ENV"  envDefault:"local"`

	GRPCPort  int `env:"GRPC_PORT"  envDefault:"9090"`
	HTTPPort  int `env:"HTTP_PORT"  envDefault:"8080"`
	DebugPort int `env:"DEBUG_PORT" envDefault:"8081"`

	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`

	Log     Log
	Tracing Tracing
	Metrics Metrics
	Swagger Swagger
	CORS    CORS
}

// Log — настройки логирования.
type Log struct {
	// Level: debug | info | warn | error.
	Level string `env:"LOG_LEVEL" envDefault:"info"`
	// Format: json | text.
	Format string `env:"LOG_FORMAT" envDefault:"json"`
}

// Tracing — настройки OpenTelemetry-трассировки.
type Tracing struct {
	Enabled bool `env:"TRACING_ENABLED" envDefault:"true"`
	// Endpoint OTLP/gRPC-коллектора (jaeger, otel-collector), host:port.
	Endpoint    string  `env:"TRACING_ENDPOINT" envDefault:"localhost:4317"`
	Insecure    bool    `env:"TRACING_INSECURE" envDefault:"true"`
	SampleRatio float64 `env:"TRACING_SAMPLE_RATIO" envDefault:"1.0"`
}

// Metrics — настройки метрик. Экспорт идёт в prometheus-registry, который
// отдаёт /metrics на debug-порте; выключение оставляет там только
// стандартные метрики Go-рантайма.
type Metrics struct {
	Enabled bool `env:"METRICS_ENABLED" envDefault:"true"`
}

// CORS — кросс-доменный доступ к HTTP-gateway.
//
// По умолчанию выключен: список origin пуст. Это осознанно — если заголовки
// уже ставит ingress, вторые от сервиса сломают браузеру ответ (два
// Access-Control-Allow-Origin недопустимы). Включай CORS в одном месте.
type CORS struct {
	// AllowedOrigins — точные origin вида https://app.example.com
	// (схема, хост и порт должны совпадать). "*" разрешает любой.
	AllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS" envSeparator:","`
	// AllowCredentials разрешает куки и Authorization в кросс-доменных
	// запросах. С "*" игнорируется: браузер отвергает такую комбинацию.
	AllowCredentials bool `env:"CORS_ALLOW_CREDENTIALS" envDefault:"false"`
	// MaxAge — насколько браузеру можно кешировать результат preflight.
	MaxAge time.Duration `env:"CORS_MAX_AGE" envDefault:"10m"`
}

// Enabled сообщает, настроен ли CORS хоть для одного origin.
func (c CORS) Enabled() bool { return len(c.AllowedOrigins) > 0 }

// Swagger — настройки Swagger UI на debug-порте.
type Swagger struct {
	Enabled bool `env:"SWAGGER_ENABLED" envDefault:"true"`
	// TargetHost — host[:port], куда Swagger UI шлёт запросы Try it out.
	// Пусто — localhost:<HTTP_PORT>. В docker-compose: localhost (порт 80).
	TargetHost string `env:"SWAGGER_TARGET_HOST"`
}

// Load загружает конфигурацию типа T из переменных окружения.
func Load[T any]() (*T, error) {
	var cfg T
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}
	return &cfg, nil
}
