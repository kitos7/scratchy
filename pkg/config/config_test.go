package config_test

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/kitos7/scratchy/pkg/config"
)

// clearEnv снимает все переменные, которые читает config.App: иначе
// окружение разработчика поедет в проверку дефолтов. Пара Setenv+Unsetenv —
// чтобы t.Setenv зарегистрировал восстановление исходного состояния, а
// переменная при этом оказалась именно отсутствующей, а не пустой.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"APP_NAME", "APP_ENV", "GRPC_PORT", "HTTP_PORT", "DEBUG_PORT",
		"SHUTDOWN_TIMEOUT", "LOG_LEVEL", "LOG_FORMAT",
		"TRACING_ENABLED", "TRACING_ENDPOINT", "TRACING_INSECURE", "TRACING_SAMPLE_RATIO",
		"METRICS_ENABLED", "SWAGGER_ENABLED", "SWAGGER_TARGET_HOST", "GREETING",
		"CORS_ALLOWED_ORIGINS", "CORS_ALLOW_CREDENTIALS", "CORS_MAX_AGE",
	} {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("unset %s: %v", k, err)
		}
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)

	cfg, err := config.Load[config.App]()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Name != "app" {
		t.Errorf("Name = %q, want app", cfg.Name)
	}
	if cfg.Environment != "local" {
		t.Errorf("Environment = %q, want local", cfg.Environment)
	}
	if cfg.GRPCPort != 9090 || cfg.HTTPPort != 8080 || cfg.DebugPort != 8081 {
		t.Errorf("порты = %d/%d/%d, want 9090/8080/8081", cfg.GRPCPort, cfg.HTTPPort, cfg.DebugPort)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 10s", cfg.ShutdownTimeout)
	}
	if cfg.Log.Level != "info" || cfg.Log.Format != "json" {
		t.Errorf("Log = %+v, want info/json", cfg.Log)
	}
	if !cfg.Tracing.Enabled || cfg.Tracing.Endpoint != "localhost:4317" || cfg.Tracing.SampleRatio != 1.0 {
		t.Errorf("Tracing = %+v", cfg.Tracing)
	}
	if !cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled по умолчанию должен быть true")
	}
	if !cfg.Swagger.Enabled {
		t.Error("Swagger.Enabled по умолчанию должен быть true")
	}
	// Пустой TargetHost — сигнал «подставь localhost:<HTTP_PORT>» в pkg/app.
	if cfg.Swagger.TargetHost != "" {
		t.Errorf("Swagger.TargetHost = %q, want пусто", cfg.Swagger.TargetHost)
	}

	// CORS по умолчанию выключен: сервис за ingress не должен дублировать
	// заголовки, которые тот уже поставил.
	if cfg.CORS.Enabled() {
		t.Errorf("CORS включён по умолчанию: %+v", cfg.CORS)
	}
	if cfg.CORS.AllowCredentials {
		t.Error("AllowCredentials по умолчанию должен быть false")
	}
	if cfg.CORS.MaxAge != 10*time.Minute {
		t.Errorf("CORS.MaxAge = %v, want 10m", cfg.CORS.MaxAge)
	}
}

func TestLoad_CORSFromEnv(t *testing.T) {
	clearEnv(t)

	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com,https://admin.example.com")
	t.Setenv("CORS_ALLOW_CREDENTIALS", "true")
	t.Setenv("CORS_MAX_AGE", "30s")

	cfg, err := config.Load[config.App]()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []string{"https://app.example.com", "https://admin.example.com"}
	if !slices.Equal(cfg.CORS.AllowedOrigins, want) {
		t.Errorf("AllowedOrigins = %v, want %v", cfg.CORS.AllowedOrigins, want)
	}
	if !cfg.CORS.Enabled() {
		t.Error("CORS должен считаться включённым")
	}
	if !cfg.CORS.AllowCredentials {
		t.Error("AllowCredentials не применился")
	}
	if cfg.CORS.MaxAge != 30*time.Second {
		t.Errorf("MaxAge = %v, want 30s", cfg.CORS.MaxAge)
	}
}

func TestLoad_FromEnv(t *testing.T) {
	clearEnv(t)

	t.Setenv("APP_NAME", "billing")
	t.Setenv("APP_ENV", "prod")
	t.Setenv("GRPC_PORT", "7000")
	t.Setenv("SHUTDOWN_TIMEOUT", "45s")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "text")
	t.Setenv("TRACING_ENABLED", "false")
	t.Setenv("TRACING_SAMPLE_RATIO", "0.25")
	t.Setenv("METRICS_ENABLED", "false")
	t.Setenv("SWAGGER_TARGET_HOST", "localhost")

	cfg, err := config.Load[config.App]()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Name != "billing" || cfg.Environment != "prod" {
		t.Errorf("Name/Environment = %q/%q", cfg.Name, cfg.Environment)
	}
	if cfg.GRPCPort != 7000 {
		t.Errorf("GRPCPort = %d, want 7000", cfg.GRPCPort)
	}
	if cfg.ShutdownTimeout != 45*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 45s", cfg.ShutdownTimeout)
	}
	if cfg.Log.Level != "debug" || cfg.Log.Format != "text" {
		t.Errorf("Log = %+v", cfg.Log)
	}
	if cfg.Tracing.Enabled {
		t.Error("TRACING_ENABLED=false не применился")
	}
	if cfg.Tracing.SampleRatio != 0.25 {
		t.Errorf("SampleRatio = %v, want 0.25", cfg.Tracing.SampleRatio)
	}
	if cfg.Metrics.Enabled {
		t.Error("METRICS_ENABLED=false не применился")
	}
	if cfg.Swagger.TargetHost != "localhost" {
		t.Errorf("TargetHost = %q, want localhost", cfg.Swagger.TargetHost)
	}
}

// Load[T] — дженерик: сгенерированный проект оборачивает config.App своим
// типом и грузит его целиком одним вызовом.
func TestLoad_EmbeddedAppConfig(t *testing.T) {
	clearEnv(t)

	type projectConfig struct {
		App      config.App
		Greeting string `env:"GREETING" envDefault:"Hello"`
	}

	cfg, err := config.Load[projectConfig]()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Greeting != "Hello" {
		t.Errorf("Greeting = %q, want Hello", cfg.Greeting)
	}
	if cfg.App.GRPCPort != 9090 {
		t.Errorf("вложенный App не заполнен: %+v", cfg.App)
	}

	t.Setenv("GREETING", "Privet")
	t.Setenv("HTTP_PORT", "1234")
	cfg, err = config.Load[projectConfig]()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Greeting != "Privet" || cfg.App.HTTPPort != 1234 {
		t.Errorf("env не применился к вложенному конфигу: %+v", cfg)
	}
}

func TestLoad_InvalidValue(t *testing.T) {
	clearEnv(t)
	t.Setenv("GRPC_PORT", "не число")

	if _, err := config.Load[config.App](); err == nil {
		t.Error("ожидалась ошибка разбора порта")
	}
}
