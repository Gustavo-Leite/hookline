package delivery

import (
	"errors"
	"testing"
	"time"
)

var errTest = errors.New("connection refused")

func fixedBackoff(random float64) Backoff {
	return Backoff{
		Base:   10 * time.Second,
		Max:    6 * time.Hour,
		Random: func() float64 { return random },
	}
}

func TestDelayGrowsExponentially(t *testing.T) {
	b := fixedBackoff(0)

	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second}

	for i, expected := range want {
		attempt := i + 1
		if got := b.Delay(attempt); got != expected {
			t.Errorf("Delay(%d) = %s, want %s", attempt, got, expected)
		}
	}
}

func TestDelayIsCapped(t *testing.T) {
	b := fixedBackoff(1)

	if got := b.Delay(50); got > b.Max {
		t.Errorf("Delay(50) = %s, want at most %s", got, b.Max)
	}
}

func TestDelayNeverCollapsesToZero(t *testing.T) {
	b := fixedBackoff(0)

	for attempt := range 20 {
		if got := b.Delay(attempt); got <= 0 {
			t.Fatalf("Delay(%d) = %s, want a positive delay", attempt, got)
		}
	}
}

func TestDelayJitterStaysInsideItsHalf(t *testing.T) {
	const attempt = 3

	low := fixedBackoff(0).Delay(attempt)
	high := fixedBackoff(0.999).Delay(attempt)

	if low >= high {
		t.Fatalf("jitter did not widen the delay: low = %s, high = %s", low, high)
	}

	if high > 2*low {
		t.Errorf("jitter exceeded half the window: low = %s, high = %s", low, high)
	}
}

func TestResultClassification(t *testing.T) {
	code := func(c int) *int { return &c }

	tests := []struct {
		name          string
		result        Result
		wantSucceeded bool
		wantRetryable bool
	}{
		{name: "200", result: Result{StatusCode: code(200)}, wantSucceeded: true},
		{name: "204", result: Result{StatusCode: code(204)}, wantSucceeded: true},
		{name: "301", result: Result{StatusCode: code(301)}},
		{name: "400", result: Result{StatusCode: code(400)}},
		{name: "401", result: Result{StatusCode: code(401)}},
		{name: "408", result: Result{StatusCode: code(408)}, wantRetryable: true},
		{name: "429", result: Result{StatusCode: code(429)}, wantRetryable: true},
		{name: "500", result: Result{StatusCode: code(500)}, wantRetryable: true},
		{name: "503", result: Result{StatusCode: code(503)}, wantRetryable: true},
		{name: "transport failure", result: Result{Error: errTest}, wantRetryable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.Succeeded(); got != tt.wantSucceeded {
				t.Errorf("Succeeded() = %v, want %v", got, tt.wantSucceeded)
			}

			if got := tt.result.Retryable(); got != tt.wantRetryable {
				t.Errorf("Retryable() = %v, want %v", got, tt.wantRetryable)
			}
		})
	}
}
