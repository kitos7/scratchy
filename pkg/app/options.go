package app

import "google.golang.org/grpc"

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
