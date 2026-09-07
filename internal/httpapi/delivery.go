package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/Gustavo-Leite/hookline/internal/delivery"
)

const (
	defaultDeliveryLimit = 50
	maxDeliveryLimit     = 200
)

type DeliveryStore interface {
	List(ctx context.Context, applicationID uuid.UUID, status string, limit int) ([]delivery.Delivery, error)
	Get(ctx context.Context, applicationID, id uuid.UUID) (delivery.Delivery, error)
	Attempts(ctx context.Context, applicationID, deliveryID uuid.UUID) ([]delivery.Attempt, error)
	Replay(ctx context.Context, applicationID, id uuid.UUID) (delivery.Delivery, error)
}

type Deliveries struct {
	store DeliveryStore
}

func NewDeliveries(store DeliveryStore) *Deliveries {
	return &Deliveries{store: store}
}

type deliveryResponse struct {
	ID            string     `json:"id"`
	EventID       string     `json:"event_id"`
	EndpointID    string     `json:"endpoint_id"`
	Status        string     `json:"status"`
	AttemptCount  int        `json:"attempt_count"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	CompletedAt   *time.Time `json:"completed_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

type attemptResponse struct {
	AttemptNumber int       `json:"attempt_number"`
	StatusCode    *int      `json:"status_code"`
	Error         *string   `json:"error"`
	DurationMS    int64     `json:"duration_ms"`
	AttemptedAt   time.Time `json:"attempted_at"`
}

func (h *Deliveries) List(w http.ResponseWriter, r *http.Request) {
	applicationID, ok := ApplicationID(r.Context())
	if !ok {
		internalError(w)
		return
	}

	status := r.URL.Query().Get("status")
	switch delivery.Status(status) {
	case "", delivery.StatusPending, delivery.StatusSucceeded, delivery.StatusDead:
	default:
		badRequest(w, "status must be one of pending, succeeded or dead")
		return
	}

	limit := defaultDeliveryLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxDeliveryLimit {
			badRequest(w, "limit must be between 1 and "+strconv.Itoa(maxDeliveryLimit))
			return
		}
		limit = parsed
	}

	found, err := h.store.List(r.Context(), applicationID, status, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "listing deliveries", "error", err)
		internalError(w)
		return
	}

	responses := make([]deliveryResponse, 0, len(found))
	for _, d := range found {
		responses = append(responses, newDeliveryResponse(d))
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": responses})
}

func (h *Deliveries) Get(w http.ResponseWriter, r *http.Request) {
	applicationID, id, ok := endpointTarget(w, r)
	if !ok {
		return
	}

	found, err := h.store.Get(r.Context(), applicationID, id)
	switch {
	case errors.Is(err, delivery.ErrNotFound):
		notFound(w)
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "getting delivery", "error", err)
		internalError(w)
		return
	}

	attempts, err := h.store.Attempts(r.Context(), applicationID, id)
	if err != nil {
		slog.ErrorContext(r.Context(), "listing delivery attempts", "error", err)
		internalError(w)
		return
	}

	responses := make([]attemptResponse, 0, len(attempts))
	for _, a := range attempts {
		responses = append(responses, attemptResponse{
			AttemptNumber: a.AttemptNumber,
			StatusCode:    a.StatusCode,
			Error:         a.Error,
			DurationMS:    a.Duration.Milliseconds(),
			AttemptedAt:   a.AttemptedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"delivery": newDeliveryResponse(found),
		"attempts": responses,
	})
}

func (h *Deliveries) Replay(w http.ResponseWriter, r *http.Request) {
	applicationID, id, ok := endpointTarget(w, r)
	if !ok {
		return
	}

	replayed, err := h.store.Replay(r.Context(), applicationID, id)
	switch {
	case errors.Is(err, delivery.ErrNotFound):
		notFound(w)
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "replaying delivery", "error", err)
		internalError(w)
		return
	}

	writeJSON(w, http.StatusAccepted, newDeliveryResponse(replayed))
}

func newDeliveryResponse(d delivery.Delivery) deliveryResponse {
	response := deliveryResponse{
		ID:           d.ID.String(),
		EventID:      d.EventID.String(),
		EndpointID:   d.EndpointID.String(),
		Status:       string(d.Status),
		AttemptCount: d.AttemptCount,
		CompletedAt:  d.CompletedAt,
		CreatedAt:    d.CreatedAt,
	}

	if d.Status == delivery.StatusPending {
		next := d.NextAttemptAt
		response.NextAttemptAt = &next
	}

	return response
}
