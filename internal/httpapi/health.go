package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

const readyTimeout = 2 * time.Second

func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func Ready(pool *pgxpool.Pool, rdb *goredis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()

		checks := map[string]string{"postgres": "ok", "redis": "ok"}
		status := http.StatusOK

		if err := pool.Ping(ctx); err != nil {
			slog.Error("readiness check failed", "dependency", "postgres", "error", err)
			checks["postgres"] = "unavailable"
			status = http.StatusServiceUnavailable
		}

		if err := rdb.Ping(ctx).Err(); err != nil {
			slog.Error("readiness check failed", "dependency", "redis", "error", err)
			checks["redis"] = "unavailable"
			status = http.StatusServiceUnavailable
		}

		writeJSON(w, status, checks)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
