package ratelimit

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func newLimiter(t *testing.T, perMinute, burst int) (*Limiter, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	return New(client, perMinute, burst), server
}

func TestAllowConsumesTheBurst(t *testing.T) {
	limiter, _ := newLimiter(t, 60, 3)

	for attempt := 1; attempt <= 3; attempt++ {
		decision, err := limiter.Allow(t.Context(), "key")
		if err != nil {
			t.Fatalf("Allow: %v", err)
		}

		if !decision.Allowed {
			t.Fatalf("request %d was rejected while the bucket still had tokens", attempt)
		}
	}

	decision, err := limiter.Allow(t.Context(), "key")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}

	if decision.Allowed {
		t.Error("the request after the burst was allowed")
	}

	if decision.RetryAfter <= 0 {
		t.Error("a rejected request must say when to try again")
	}
}

func TestAllowRefillsOverTime(t *testing.T) {
	limiter, server := newLimiter(t, 60, 1)

	start := time.Now()
	server.SetTime(start)

	if decision, _ := limiter.Allow(t.Context(), "key"); !decision.Allowed {
		t.Fatal("the first request was rejected")
	}

	if decision, _ := limiter.Allow(t.Context(), "key"); decision.Allowed {
		t.Fatal("the bucket did not run out")
	}

	server.SetTime(start.Add(time.Second))

	if decision, _ := limiter.Allow(t.Context(), "key"); !decision.Allowed {
		t.Error("the bucket did not refill after a second")
	}
}

func TestAllowIsolatesKeys(t *testing.T) {
	limiter, _ := newLimiter(t, 60, 1)

	if decision, _ := limiter.Allow(t.Context(), "first"); !decision.Allowed {
		t.Fatal("the first key was rejected")
	}

	decision, err := limiter.Allow(t.Context(), "second")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}

	if !decision.Allowed {
		t.Error("one key exhausting its bucket blocked another")
	}
}

func TestAllowReportsRemaining(t *testing.T) {
	limiter, _ := newLimiter(t, 600, 10)

	decision, err := limiter.Allow(t.Context(), "key")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}

	if decision.Limit != 10 {
		t.Errorf("Limit = %d, want 10", decision.Limit)
	}

	if decision.Remaining != 9 {
		t.Errorf("Remaining = %d, want 9", decision.Remaining)
	}
}
