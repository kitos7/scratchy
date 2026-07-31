package app

import (
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

// Option настраивает App.
type Option func(*App)

// WithGRPC добавляет регистрацию gRPC-сервисов.
func WithGRPC(r GRPCRegistrar) Option {
	return func(a *App) { a.grpcRegs = append(a.grpcRegs, r) }
}

// WithGateway добавляет регистрацию хендлеров grpc-gateway.
func WithGateway(r GatewayRegistrar) Option {
	return func(a *App) { a.gatewayRegs = append(a.gatewayRegs, r) }
}

// WithSwagger подключает OpenAPI-спецификацию для Swagger UI на debug-порте.
func WithSwagger(spec []byte) Option {
	return func(a *App) { a.swaggerJSON = spec }
}

// WithUnaryInterceptors добавляет пользовательские unary-интерсепторы
// после базовых (recovery, logging).
func WithUnaryInterceptors(ints ...grpc.UnaryServerInterceptor) Option {
	return func(a *App) { a.extraUnary = append(a.extraUnary, ints...) }
}

// WithStreamInterceptors добавляет пользовательские stream-интерсепторы
// после базовых (recovery, logging).
func WithStreamInterceptors(ints ...grpc.StreamServerInterceptor) Option {
	return func(a *App) { a.extraStream = append(a.extraStream, ints...) }
}

// WithGatewayOptions настраивает ServeMux gateway: маппинг заголовков
// в metadata, обработчик ошибок, маршалер и т.п.
func WithGatewayOptions(opts ...runtime.ServeMuxOption) Option {
	return func(a *App) { a.gatewayOpts = append(a.gatewayOpts, opts...) }
}

// WithHTTPMiddleware оборачивает HTTP-gateway. Middleware применяются в
// порядке добавления (первый — внешний) и живут внутри otelhttp,
// то есть видят активный спан и пишут trace_id в логи.
func WithHTTPMiddleware(mw ...func(http.Handler) http.Handler) Option {
	return func(a *App) { a.httpMiddleware = append(a.httpMiddleware, mw...) }
}
