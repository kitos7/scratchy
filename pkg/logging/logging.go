// Package logging настраивает slog-логгер с прокидыванием
// trace_id/span_id из контекста в каждую запись.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/kitos7/scratchy/pkg/config"
)

// New создаёт логгер по конфигурации, пишет в stdout и устанавливает
// его дефолтным.
func New(cfg config.Log) *slog.Logger {
	logger := NewWithWriter(os.Stdout, cfg)
	slog.SetDefault(logger)
	return logger
}

// NewWithWriter создаёт логгер с явным получателем записей и не трогает
// дефолтный логгер. Нужен, когда вывод надо перехватить — в тестах или
// когда сервис пишет лог не в stdout.
func NewWithWriter(w io.Writer, cfg config.Log) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(cfg.Level)}

	var h slog.Handler
	if strings.EqualFold(cfg.Format, "text") {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}

	return slog.New(newTraceHandler(h))
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// traceHandler добавляет trace_id/span_id активного спана в записи лога —
// всегда на верхний уровень записи, даже если логгер открыл группу через
// WithGroup. Иначе trace_id уехал бы внутрь группы и поиск по нему в
// агрегаторе логов зависел бы от того, какой логгер сделал запись.
type traceHandler struct {
	// Handler — цепочка, собранная жадно: быстрый путь, когда групп нет.
	slog.Handler
	// base — тот же хендлер до всех WithAttrs/WithGroup.
	base slog.Handler
	// ops — что применили к base, в порядке применения.
	ops []handlerOp
	// inGroup — среди ops есть WithGroup, то есть быстрый путь неприменим.
	inGroup bool
}

// handlerOp — одно звено цепочки: либо группа, либо набор атрибутов.
type handlerOp struct {
	group string
	attrs []slog.Attr
}

func newTraceHandler(h slog.Handler) traceHandler {
	return traceHandler{Handler: h, base: h}
}

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return h.Handler.Handle(ctx, r)
	}

	attrs := []slog.Attr{
		slog.String("trace_id", sc.TraceID().String()),
		slog.String("span_id", sc.SpanID().String()),
	}

	// Групп нет — атрибуты записи и так окажутся на верхнем уровне.
	if !h.inGroup {
		r = r.Clone()
		r.AddAttrs(attrs...)
		return h.Handler.Handle(ctx, r)
	}

	// Группа открыта: ставим trace_id на base (там групп ещё нет) и заново
	// проигрываем цепочку поверх.
	next := h.base.WithAttrs(attrs)
	for _, op := range h.ops {
		if op.group != "" {
			next = next.WithGroup(op.group)
			continue
		}
		next = next.WithAttrs(op.attrs)
	}
	return next.Handle(ctx, r)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return traceHandler{
		Handler: h.Handler.WithAttrs(attrs),
		base:    h.base,
		ops:     append(slices.Clip(h.ops), handlerOp{attrs: attrs}),
		inGroup: h.inGroup,
	}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return traceHandler{
		Handler: h.Handler.WithGroup(name),
		base:    h.base,
		ops:     append(slices.Clip(h.ops), handlerOp{group: name}),
		inGroup: true,
	}
}
