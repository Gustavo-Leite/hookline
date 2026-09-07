package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"
	"uuid"
)

const (
	requestIDHeader    = "X-Request-Id"
	maxRequestIDLength = 64
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeRequestID(r.Header.Get(requestIDHeader))
		if id == "" {
			id = uuid.NewV7().String()
		}

		w.Header().Set(requestIDHeader, id)

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}

		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(started).Milliseconds(),
			"request_id", RequestIDFrom(r.Context()),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func sanitizeRequestID(id string) string {
	if len(id) > maxRequestIDLength {
		return ""
	}

	for _, r := range id {
		if r < ' ' || r > '~' {
			return ""
		}
	}

	return id
}
