package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubPinger struct {
	err error
}

func (s stubPinger) Ping(context.Context) error {
	return s.err
}

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)

	Health().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("status field = %q, want %q", body["status"], "ok")
	}
}

func TestReady(t *testing.T) {
	down := errors.New("connection refused")

	tests := []struct {
		name       string
		postgres   error
		redis      error
		wantStatus int
		wantChecks map[string]string
	}{
		{
			name:       "all dependencies up",
			wantStatus: http.StatusOK,
			wantChecks: map[string]string{"postgres": "ok", "redis": "ok"},
		},
		{
			name:       "postgres down",
			postgres:   down,
			wantStatus: http.StatusServiceUnavailable,
			wantChecks: map[string]string{"postgres": "unavailable", "redis": "ok"},
		},
		{
			name:       "redis down",
			redis:      down,
			wantStatus: http.StatusServiceUnavailable,
			wantChecks: map[string]string{"postgres": "ok", "redis": "unavailable"},
		},
		{
			name:       "everything down",
			postgres:   down,
			redis:      down,
			wantStatus: http.StatusServiceUnavailable,
			wantChecks: map[string]string{"postgres": "unavailable", "redis": "unavailable"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil)

			Ready(stubPinger{tt.postgres}, stubPinger{tt.redis}).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			var body map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decoding body: %v", err)
			}

			for name, want := range tt.wantChecks {
				if body[name] != want {
					t.Errorf("%s = %q, want %q", name, body[name], want)
				}
			}
		})
	}
}

// The response must never carry the underlying error: it can leak hosts,
// ports and usernames to whoever can reach the endpoint.
func TestReadyDoesNotLeakErrorDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil)

	secret := "postgres://user:hunter2@db.internal:5432"
	Ready(stubPinger{errors.New(secret)}, stubPinger{nil}).ServeHTTP(rec, req)

	if body := rec.Body.String(); strings.Contains(body, secret) {
		t.Errorf("response body leaked the connection error: %s", body)
	}
}
