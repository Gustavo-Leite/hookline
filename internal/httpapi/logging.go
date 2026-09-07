package httpapi

import (
	"context"
	"log/slog"
)

func NewLogHandler(inner slog.Handler) slog.Handler {
	return contextLogHandler{Handler: inner}
}

type contextLogHandler struct {
	slog.Handler
}

func (h contextLogHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := RequestIDFrom(ctx); id != "" {
		record.AddAttrs(slog.String("request_id", id))
	}

	return h.Handler.Handle(ctx, record)
}

func (h contextLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextLogHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h contextLogHandler) WithGroup(name string) slog.Handler {
	return contextLogHandler{Handler: h.Handler.WithGroup(name)}
}
