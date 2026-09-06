package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/Gustavo-Leite/hookline/internal/endpoint"
)

const maxBodyBytes = 64 << 10

type EndpointStore interface {
	Create(ctx context.Context, e endpoint.Endpoint) (endpoint.Endpoint, error)
	List(ctx context.Context, applicationID uuid.UUID) ([]endpoint.Endpoint, error)
	Get(ctx context.Context, applicationID, id uuid.UUID) (endpoint.Endpoint, error)
	Delete(ctx context.Context, applicationID, id uuid.UUID) error
}

type Endpoints struct {
	store EndpointStore
}

func NewEndpoints(store EndpointStore) *Endpoints {
	return &Endpoints{store: store}
}

type createEndpointRequest struct {
	URL         string   `json:"url"`
	Description string   `json:"description"`
	EventTypes  []string `json:"event_types"`
}

type endpointResponse struct {
	ID          string     `json:"id"`
	URL         string     `json:"url"`
	Description string     `json:"description"`
	EventTypes  []string   `json:"event_types"`
	Secret      string     `json:"secret,omitempty"`
	DisabledAt  *time.Time `json:"disabled_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (h *Endpoints) Create(w http.ResponseWriter, r *http.Request) {
	applicationID, ok := ApplicationID(r.Context())
	if !ok {
		internalError(w)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var req createEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "malformed json body")
		return
	}

	if err := endpoint.ValidateURL(req.URL); err != nil {
		badRequest(w, err.Error())
		return
	}

	secret, err := endpoint.GenerateSecret()
	if err != nil {
		slog.Error("generating endpoint secret", "error", err)
		internalError(w)
		return
	}

	eventTypes := req.EventTypes
	if eventTypes == nil {
		eventTypes = []string{}
	}

	created, err := h.store.Create(r.Context(), endpoint.Endpoint{
		ApplicationID: applicationID,
		URL:           req.URL,
		Description:   req.Description,
		Secret:        secret,
		EventTypes:    eventTypes,
	})
	if err != nil {
		slog.Error("creating endpoint", "error", err)
		internalError(w)
		return
	}

	response := newEndpointResponse(created)
	response.Secret = created.Secret

	writeJSON(w, http.StatusCreated, response)
}

func (h *Endpoints) List(w http.ResponseWriter, r *http.Request) {
	applicationID, ok := ApplicationID(r.Context())
	if !ok {
		internalError(w)
		return
	}

	found, err := h.store.List(r.Context(), applicationID)
	if err != nil {
		slog.Error("listing endpoints", "error", err)
		internalError(w)
		return
	}

	responses := make([]endpointResponse, 0, len(found))
	for _, e := range found {
		responses = append(responses, newEndpointResponse(e))
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": responses})
}

func (h *Endpoints) Get(w http.ResponseWriter, r *http.Request) {
	applicationID, id, ok := endpointTarget(w, r)
	if !ok {
		return
	}

	found, err := h.store.Get(r.Context(), applicationID, id)
	switch {
	case errors.Is(err, endpoint.ErrNotFound):
		notFound(w)
		return
	case err != nil:
		slog.Error("getting endpoint", "error", err)
		internalError(w)
		return
	}

	writeJSON(w, http.StatusOK, newEndpointResponse(found))
}

func (h *Endpoints) Delete(w http.ResponseWriter, r *http.Request) {
	applicationID, id, ok := endpointTarget(w, r)
	if !ok {
		return
	}

	err := h.store.Delete(r.Context(), applicationID, id)
	switch {
	case errors.Is(err, endpoint.ErrNotFound):
		notFound(w)
		return
	case err != nil:
		slog.Error("deleting endpoint", "error", err)
		internalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func newEndpointResponse(e endpoint.Endpoint) endpointResponse {
	return endpointResponse{
		ID:          e.ID.String(),
		URL:         e.URL,
		Description: e.Description,
		EventTypes:  e.EventTypes,
		DisabledAt:  e.DisabledAt,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
}

func endpointTarget(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	applicationID, ok := ApplicationID(r.Context())
	if !ok {
		internalError(w)
		return uuid.Nil(), uuid.Nil(), false
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid endpoint id")
		return uuid.Nil(), uuid.Nil(), false
	}

	return applicationID, id, true
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
}

func badRequest(w http.ResponseWriter, reason string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": reason})
}

func internalError(w http.ResponseWriter) {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
}
