// Package app — рантайм scratch-сервиса: gRPC-сервер, HTTP-gateway
// и debug-сервер с graceful shutdown и OTel-инструментированием.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	"github.com/kitos7/scratchy/pkg/config"
	"github.com/kitos7/scratchy/pkg/debug"
	"github.com/kitos7/scratchy/pkg/grpcmw"
	"github.com/kitos7/scratchy/pkg/httpmw"
)

// GRPCRegistrar регистрирует gRPC-сервисы на сервере.
type GRPCRegistrar func(*grpc.Server)

// GatewayRegistrar регистрирует хендлеры grpc-gateway
// (сигнатура сгенерированных Register<Service>Handler).
type GatewayRegistrar func(context.Context, *runtime.ServeMux, *grpc.ClientConn) error

// App — собранное приложение.
type App struct {
	cfg config.App
	log *slog.Logger

	grpcRegs    []GRPCRegistrar
	gatewayRegs []GatewayRegistrar
	swaggerJSON []byte

	extraUnary  []grpc.UnaryServerInterceptor
	extraStream []grpc.StreamServerInterceptor

	gatewayOpts    []runtime.ServeMuxOption
	httpMiddleware []func(http.Handler) http.Handler
	debugHandlers  map[string]http.Handler
}

// New собирает приложение из конфигурации и опций.
func New(cfg config.App, log *slog.Logger, opts ...Option) *App {
	a := &App{cfg: cfg, log: log}
	for _, o := range opts {
		o(a)
	}
	return a
}

// corsConfig возвращает настройки CORS с поправкой на Swagger: его UI живёт
// на debug-порту, а запросы Try it out уходят на публичный HTTP-порт — это
// кросс-доменные запросы, и без разрешения браузер их не пропустит.
// Debug-порт открыт только внутри, поэтому разрешать его безопасно.
func (a *App) corsConfig(swaggerServed bool) config.CORS {
	cors := a.cfg.CORS
	if !swaggerServed {
		return cors
	}

	// Отдельный слайс: append не должен задеть массив из конфигурации.
	origins := make([]string, 0, len(cors.AllowedOrigins)+2)
	origins = append(origins, cors.AllowedOrigins...)
	origins = append(origins,
		fmt.Sprintf("http://localhost:%d", a.cfg.DebugPort),
		fmt.Sprintf("http://127.0.0.1:%d", a.cfg.DebugPort),
	)
	cors.AllowedOrigins = origins
	return cors
}

// Run запускает все серверы и блокируется до сигнала остановки или ошибки.
func (a *App) Run(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// gRPC-сервер: трассировка через stats handler, recovery + логирование.
	// Стримы прикрыты той же парой: `scratch handlers` заводит стрим-ручки,
	// и паника в них не должна ронять процесс.
	unary := make([]grpc.UnaryServerInterceptor, 0, 2+len(a.extraUnary))
	unary = append(unary, grpcmw.Recovery(a.log), grpcmw.Logging(a.log))
	unary = append(unary, a.extraUnary...)

	stream := make([]grpc.StreamServerInterceptor, 0, 2+len(a.extraStream))
	stream = append(stream, grpcmw.RecoveryStream(a.log), grpcmw.LoggingStream(a.log))
	stream = append(stream, a.extraStream...)

	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(stream...),
	)
	for _, register := range a.grpcRegs {
		register(grpcServer)
	}
	reflection.Register(grpcServer)

	grpcLis, err := net.Listen("tcp", fmt.Sprintf(":%d", a.cfg.GRPCPort))
	if err != nil {
		return fmt.Errorf("listen grpc port: %w", err)
	}

	// HTTP-gateway: loopback-соединение к gRPC, трассировка сквозная.
	conn, err := grpc.NewClient(
		fmt.Sprintf("127.0.0.1:%d", a.cfg.GRPCPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return fmt.Errorf("dial grpc for gateway: %w", err)
	}
	defer func() { _ = conn.Close() }()

	gatewayMux := runtime.NewServeMux(a.gatewayOpts...)
	for _, register := range a.gatewayRegs {
		if err := register(ctx, gatewayMux, conn); err != nil {
			return fmt.Errorf("register gateway handler: %w", err)
		}
	}

	// Спека нужна раньше gateway: от того, отдаётся ли Swagger, зависит
	// список origin для CORS.
	var swaggerJSON []byte
	if a.cfg.Swagger.Enabled {
		swaggerJSON = a.swaggerJSON
	}

	// Middleware внутри otelhttp: спан уже создан, значит логи из middleware
	// получают trace_id. Первый в списке — самый внешний.
	middleware := a.httpMiddleware
	if cors := a.corsConfig(swaggerJSON != nil); cors.Enabled() {
		// CORS впереди пользовательских: preflight не должен зависеть от них.
		middleware = append([]func(http.Handler) http.Handler{httpmw.CORS(cors)}, middleware...)
	}

	var gatewayHandler http.Handler = gatewayMux
	for i := len(middleware) - 1; i >= 0; i-- {
		gatewayHandler = middleware[i](gatewayHandler)
	}

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", a.cfg.HTTPPort),
		Handler:           otelhttp.NewHandler(gatewayHandler, "http.gateway"),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Debug-сервер: swagger, metrics, healthz, pprof.
	swaggerTarget := a.cfg.Swagger.TargetHost
	if swaggerTarget == "" {
		swaggerTarget = fmt.Sprintf("localhost:%d", a.cfg.HTTPPort)
	}
	debugServer := debug.New(a.cfg.DebugPort, debug.Options{
		SwaggerJSON: swaggerJSON,
		Title:       a.cfg.Name,
		TargetHost:  swaggerTarget,
		Handlers:    a.debugHandlers,
	})

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		a.log.Info("grpc server started", slog.Int("port", a.cfg.GRPCPort))
		if err := grpcServer.Serve(grpcLis); err != nil {
			return fmt.Errorf("grpc server: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		a.log.Info("http gateway started", slog.Int("port", a.cfg.HTTPPort))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http gateway: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		a.log.Info("debug server started",
			slog.Int("port", a.cfg.DebugPort),
			slog.String("swagger", fmt.Sprintf("http://localhost:%d/swagger/", a.cfg.DebugPort)),
		)
		if err := debugServer.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("debug server: %w", err)
		}
		return nil
	})

	// Graceful shutdown по сигналу или первой ошибке.
	g.Go(func() error {
		<-gctx.Done()
		a.log.Info("shutting down", slog.Duration("timeout", a.cfg.ShutdownTimeout))
		debugServer.SetReady(false)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
		defer cancel()

		_ = httpServer.Shutdown(shutdownCtx)
		_ = debugServer.Shutdown(shutdownCtx)

		stopped := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-shutdownCtx.Done():
			grpcServer.Stop()
		}
		return nil
	})

	debugServer.SetReady(true)

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
