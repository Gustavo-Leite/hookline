package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Gustavo-Leite/hookline/internal/ratelimit"
)

type fakeLimiter struct {
	decision     ratelimit.Decision
	err          error
	calls        int
	receivedKeys []string
}

func (f *fakeLimiter) Allow(_ context.Context, key string) (ratelimit.Decision, error) {
	f.calls++
	f.receivedKeys = append(f.receivedKeys, key)

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

func TestRateLimitCountsUnauthenticatedRequests(t *testing.T) {
	limiter := &fakeLimiter{decision: ratelimit.Decision{Allowed: true}}

	var reached bool
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/me", nil)
	req.RemoteAddr = "203.0.113.7:54321"

	RateLimit(limiter)(okHandler(&reached)).ServeHTTP(rec, req)

	if limiter.calls != 1 {
		t.Fatalf("the limiter was called %d times, want 1", limiter.calls)
	}

	if got := limiter.receivedKeys[0]; got != "ip:203.0.113.7" {
		t.Errorf("bucket = %q, want it keyed by the peer address", got)
	}

	if !reached {
		t.Error("an allowed request was blocked")
	}
}

func TestRateLimitBucketsByCredentialNotByIdentity(t *testing.T) {
	limiter := &fakeLimiter{decision: ratelimit.Decision{Allowed: true}}

	var reached bool
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer hl_test_whatever")

	RateLimit(limiter)(okHandler(&reached)).ServeHTTP(rec, req)

	key := limiter.receivedKeys[0]

	if !strings.HasPrefix(key, "key:") {
		t.Errorf("bucket = %q, want it keyed by the presented credential", key)
	}

	if strings.Contains(key, "hl_test_whatever") {
		t.Error("the bucket key contains the raw credential")
	}
}
