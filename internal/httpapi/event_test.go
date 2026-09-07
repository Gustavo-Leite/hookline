package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/Gustavo-Leite/hookline/internal/event"
)

type fakeEventStore struct {
	received event.Event
	replayed bool
	err      error
}

func (f *fakeEventStore) Create(_ context.Context, e event.Event) (event.Event, bool, error) {
	f.received = e

	if f.err != nil {
		return event.Event{}, false, f.err
	}

	e.ID = uuid.NewV7()
	e.CreatedAt = time.Now()

	return e, f.replayed, nil
}

func postEvent(t *testing.T, store EventStore, body string, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()

	req := authenticated(httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/events", strings.NewReader(body)))
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	rec := httptest.NewRecorder()
	NewEvents(store).Create(rec, req)

	return rec
}

func TestCreateEventAccepted(t *testing.T) {
	store := &fakeEventStore{}

	rec := postEvent(t, store, `{"type":"user.created","payload":{"id":42}}`, "abc")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusAccepted, rec.Body)
	}

	if rec.Header().Get("Idempotent-Replay") != "" {
		t.Error("a first delivery must not be marked as a replay")
	}

	if store.received.IdempotencyKey == nil || *store.received.IdempotencyKey != "abc" {
		t.Error("the Idempotency-Key header did not reach the store")
	}
}

func TestCreateEventWithoutIdempotencyKey(t *testing.T) {
	store := &fakeEventStore{}

	postEvent(t, store, `{"type":"user.created","payload":{"id":42}}`, "")

	if store.received.IdempotencyKey != nil {
		t.Error("a missing header must reach the store as nil, not an empty string")
	}
}

func TestCreateEventReplayIsMarkedInTheHeaders(t *testing.T) {
	store := &fakeEventStore{replayed: true}

	rec := postEvent(t, store, `{"type":"user.created","payload":{"id":42}}`, "abc")

	if rec.Header().Get("Idempotent-Replay") != "true" {
		t.Error("a replay must be reported in the response headers")
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	if _, present := body["replayed"]; present {
		t.Error("the body must stay identical between the original and the replay")
	}
}

func TestCreateEventRejectsAReusedKey(t *testing.T) {
	store := &fakeEventStore{err: event.ErrIdempotencyKeyReused}

	rec := postEvent(t, store, `{"type":"user.created","payload":{"id":99}}`, "abc")

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestCreateEventValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing type", body: `{"payload":{"id":42}}`},
		{name: "missing payload", body: `{"type":"user.created"}`},
		{name: "malformed json", body: `{"type":`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := postEvent(t, &fakeEventStore{}, tt.body, "")

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}
