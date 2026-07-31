// Package grpcmw — базовые интерсепторы: логирование и recovery.
// Каждый есть в двух видах — unary и stream: `scratch handlers` умеет
// заводить стриминговые ручки, и они должны быть защищены так же.
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

// LoggingStream логирует стриминговый вызов: метод, вид стрима, код,
// длительность. Запись делается по завершении вызова, а не на каждое
// сообщение — иначе долгий стрим зальёт лог.
func LoggingStream(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)

		ctx := ss.Context()
		attrs := []slog.Attr{
			slog.String("method", info.FullMethod),
			slog.String("stream", streamKind(info)),
			slog.String("code", status.Code(err).String()),
			slog.Duration("duration", time.Since(start)),
		}
		lvl := slog.LevelInfo
		if err != nil {
			lvl = slog.LevelError
			attrs = append(attrs, slog.String("error", err.Error()))
		}
		log.LogAttrs(ctx, lvl, "grpc stream", attrs...)

		return err
	}
}

// streamKind описывает вид стрима для лога.
func streamKind(info *grpc.StreamServerInfo) string {
	switch {
	case info.IsClientStream && info.IsServerStream:
		return "bidi"
	case info.IsClientStream:
		return "client"
	case info.IsServerStream:
		return "server"
	default:
		return "unary"
	}
}

// Recovery перехватывает паники в unary-хендлерах и возвращает codes.Internal.
func Recovery(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(ctx, log, info.FullMethod, r)
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

// RecoveryStream перехватывает паники в стриминговых хендлерах.
// Без него паника в стрим-ручке роняет весь процесс.
func RecoveryStream(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(ss.Context(), log, info.FullMethod, r)
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(srv, ss)
	}
}

func logPanic(ctx context.Context, log *slog.Logger, method string, r any) {
	log.LogAttrs(ctx, slog.LevelError, "panic recovered",
		slog.String("method", method),
		slog.Any("panic", r),
		slog.String("stack", string(debug.Stack())),
	)
}
