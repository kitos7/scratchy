// Package grpcmw — базовые unary-интерсепторы: логирование и recovery.
package grpcmw

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Logging логирует каждый gRPC-вызов: метод, код, длительность.
// Благодаря контексту в записи попадают trace_id/span_id.
func Logging(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)

		attrs := []slog.Attr{
			slog.String("method", info.FullMethod),
			slog.String("code", status.Code(err).String()),
			slog.Duration("duration", time.Since(start)),
		}
		lvl := slog.LevelInfo
		if err != nil {
			lvl = slog.LevelError
			attrs = append(attrs, slog.String("error", err.Error()))
		}
		log.LogAttrs(ctx, lvl, "grpc call", attrs...)

		return resp, err
	}
}

// Recovery перехватывает паники в хендлерах и возвращает codes.Internal.
func Recovery(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.LogAttrs(ctx, slog.LevelError, "panic recovered",
					slog.String("method", info.FullMethod),
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
				)
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}
