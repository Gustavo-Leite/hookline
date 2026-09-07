package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Gustavo-Leite/hookline/internal/metrics"
	"github.com/Gustavo-Leite/hookline/internal/ratelimit"
)

type Limiter interface {
	Allow(ctx context.Context, key string) (ratelimit.Decision, error)
}

func RateLimit(limiter Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			keyID, ok := APIKeyID(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			decision, err := limiter.Allow(r.Context(), keyID.String())
			if err != nil {
				slog.ErrorContext(r.Context(), "rate limiting unavailable, letting the request through", "error", err)
				next.ServeHTTP(w, r)

				return
			}

			w.Header().Set("RateLimit-Limit", strconv.Itoa(decision.Limit))
			w.Header().Set("RateLimit-Remaining", strconv.Itoa(decision.Remaining))

			if decision.Allowed {
				next.ServeHTTP(w, r)
				return
			}

			metrics.RateLimited.Inc()

			retryAfter := max(1, int(decision.RetryAfter.Round(time.Second)/time.Second))

			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		})
	}
}
