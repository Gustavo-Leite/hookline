package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Gustavo-Leite/hookline/internal/ratelimit"
)

type fakeLimiter struct {
	decision ratelimit.Decision
	err      error
	calls    int
}

func (f *fakeLimiter) Allow(context.Context, string) (ratelimit.Decision, error) {
	f.calls++
	return f.decision, f.err
}

func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestRateLimitAllowsAndReportsTheAllowance(t *testing.T) {
	limiter := &fakeLimiter{decision: ratelimit.Decision{Allowed: true, Limit: 60, Remaining: 59}}

	var reached bool
	rec := httptest.NewRecorder()
	req := authenticated(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/me", nil))

	RateLimit(limiter)(okHandler(&reached)).ServeHTTP(rec, req)

	if !reached {
		t.Fatal("an allowed request did not reach the handler")
	}

	if got := rec.Header().Get("RateLimit-Remaining"); got != "59" {
		t.Errorf("RateLimit-Remaining = %q, want %q", got, "59")
	}
}

func TestRateLimitRejectsWithRetryAfter(t *testing.T) {
	limiter := &fakeLimiter{decision: ratelimit.Decision{Limit: 60, RetryAfter: 2500 * time.Millisecond}}

	var reached bool
	rec := httptest.NewRecorder()
	req := authenticated(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/me", nil))

	RateLimit(limiter)(okHandler(&reached)).ServeHTTP(rec, req)

	if reached {
		t.Error("a throttled request reached the handler")
	}

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	if got := rec.Header().Get("Retry-After"); got != "3" {
		t.Errorf("Retry-After = %q, want %q", got, "3")
	}
}

func TestRateLimitFailsOpenWhenRedisIsDown(t *testing.T) {
	limiter := &fakeLimiter{err: errors.New("connection refused")}

	var reached bool
	rec := httptest.NewRecorder()
	req := authenticated(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/me", nil))

	RateLimit(limiter)(okHandler(&reached)).ServeHTTP(rec, req)

	if !reached {
		t.Error("a Redis outage blocked the request instead of letting it through")
	}
}

func TestRateLimitIgnoresUnauthenticatedRequests(t *testing.T) {
	limiter := &fakeLimiter{}

	var reached bool
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)

	RateLimit(limiter)(okHandler(&reached)).ServeHTTP(rec, req)

	if limiter.calls != 0 {
		t.Error("an unauthenticated request consumed an allowance")
	}

	if !reached {
		t.Error("an unauthenticated request was blocked")
	}
}
