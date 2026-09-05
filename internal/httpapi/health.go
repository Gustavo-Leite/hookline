package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

const readyTimeout = 2 * time.Second

type Pinger interface {
	Ping(ctx context.Context) error
}

func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func Ready(postgres, redis Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()

		dependencies := map[string]Pinger{"postgres": postgres, "redis": redis}
		checks := make(map[string]string, len(dependencies))
		status := http.StatusOK

		for name, dependency := range dependencies {
			if err := dependency.Ping(ctx); err != nil {
				slog.Error("readiness check failed", "dependency", name, "error", err)
				checks[name] = "unavailable"
				status = http.StatusServiceUnavailable
				continue
			}
			checks[name] = "ok"
		}

		writeJSON(w, status, checks)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
