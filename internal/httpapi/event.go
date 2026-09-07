package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Gustavo-Leite/hookline/internal/event"
)

const maxEventBodyBytes = 256 << 10

type EventStore interface {
	Create(ctx context.Context, e event.Event) (event.Event, bool, error)
}

type Events struct {
	store EventStore
}

func NewEvents(store EventStore) *Events {
	return &Events{store: store}
}

type createEventRequest struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type eventResponse struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *Events) Create(w http.ResponseWriter, r *http.Request) {
	applicationID, ok := ApplicationID(r.Context())
	if !ok {
		internalError(w)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxEventBodyBytes)

	var req createEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "payload too large"})
			return
		}

		badRequest(w, "malformed json body")
		return
	}

	if req.Type == "" {
		badRequest(w, "type is required")
		return
	}

	if len(req.Payload) == 0 {
		badRequest(w, "payload is required")
		return
	}

	var idempotencyKey *string
	if key := r.Header.Get("Idempotency-Key"); key != "" {
		idempotencyKey = &key
	}

	created, replayed, err := h.store.Create(r.Context(), event.Event{
		ApplicationID:  applicationID,
		Type:           req.Type,
		Payload:        req.Payload,
		IdempotencyKey: idempotencyKey,
		PayloadHash:    event.HashPayload(req.Type, req.Payload),
	})
	switch {
	case errors.Is(err, event.ErrIdempotencyKeyReused):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "idempotency key already used with a different payload"})
		return
	case err != nil:
		slog.Error("creating event", "error", err)
		internalError(w)
		return
	}

	if replayed {
		w.Header().Set("Idempotent-Replay", "true")
	}

	writeJSON(w, http.StatusAccepted, eventResponse{
		ID:        created.ID.String(),
		Type:      created.Type,
		CreatedAt: created.CreatedAt,
	})
}
