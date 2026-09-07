package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/Gustavo-Leite/hookline/internal/endpoint"
)

type fakeEndpointStore struct {
	list           []endpoint.Endpoint
	getResult      endpoint.Endpoint
	getErr         error
	updateErr      error
	receivedUpdate endpoint.UpdateParams
	deleteErr      error
}

func (f *fakeEndpointStore) Create(_ context.Context, e endpoint.Endpoint) (endpoint.Endpoint, error) {
	e.ID = uuid.NewV7()
	return e, nil
}

func (f *fakeEndpointStore) List(context.Context, uuid.UUID) ([]endpoint.Endpoint, error) {
	return f.list, nil
}

func (f *fakeEndpointStore) Get(context.Context, uuid.UUID, uuid.UUID) (endpoint.Endpoint, error) {
	return f.getResult, f.getErr
}

func (f *fakeEndpointStore) Update(_ context.Context, _, _ uuid.UUID, params endpoint.UpdateParams) (endpoint.Endpoint, error) {
	f.receivedUpdate = params

	if f.updateErr != nil {
		return endpoint.Endpoint{}, f.updateErr
	}

	updated := f.getResult
	if params.URL != nil {
		updated.URL = *params.URL
	}
	if params.EventTypes != nil {
		updated.EventTypes = *params.EventTypes
	}

	return updated, nil
}

func (f *fakeEndpointStore) Delete(context.Context, uuid.UUID, uuid.UUID) error {
	return f.deleteErr
}

func authenticated(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), applicationIDKey, uuid.NewV7())
	ctx = context.WithValue(ctx, apiKeyIDKey, uuid.NewV7())

	return r.WithContext(ctx)
}

func TestCreateEndpointReturnsTheSecret(t *testing.T) {
	body := `{"url":"https://example.com/hooks","event_types":["user.created"]}`
	req := authenticated(httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/endpoints", strings.NewReader(body)))
	rec := httptest.NewRecorder()

	NewEndpoints(&fakeEndpointStore{}).Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusCreated, rec.Body)
	}

	var response endpointResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	if !strings.HasPrefix(response.Secret, "whsec_") {
		t.Errorf("Secret = %q, want it to start with whsec_", response.Secret)
	}
}

func TestCreateEndpointRejectsPlainHTTP(t *testing.T) {
	body := `{"url":"http://example.com/hooks"}`
	req := authenticated(httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/endpoints", strings.NewReader(body)))
	rec := httptest.NewRecorder()

	NewEndpoints(&fakeEndpointStore{}).Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListEndpointsNeverExposesTheSecret(t *testing.T) {
	const secret = "whsec_do-not-leak-me"

	store := &fakeEndpointStore{
		list: []endpoint.Endpoint{{
			ID:         uuid.NewV7(),
			URL:        "https://example.com/hooks",
			Secret:     secret,
			EventTypes: []string{},
		}},
	}

	req := authenticated(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/endpoints", nil))
	rec := httptest.NewRecorder()

	NewEndpoints(store).List(rec, req)

	if strings.Contains(rec.Body.String(), secret) {
		t.Errorf("the listing leaked the endpoint secret: %s", rec.Body)
	}
}

func TestGetEndpointRejectsMalformedID(t *testing.T) {
	req := authenticated(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/endpoints/abc", nil))
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	NewEndpoints(&fakeEndpointStore{}).Get(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestDeleteEndpointNotFound(t *testing.T) {
	store := &fakeEndpointStore{deleteErr: endpoint.ErrNotFound}

	req := authenticated(httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/v1/endpoints/x", nil))
	req.SetPathValue("id", uuid.NewV7().String())
	rec := httptest.NewRecorder()

	NewEndpoints(store).Delete(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestUpdateEndpointAppliesOnlyThePresentFields(t *testing.T) {
	store := &fakeEndpointStore{}

	body := `{"disabled":true}`
	req := authenticated(httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/v1/endpoints/x", strings.NewReader(body)))
	req.SetPathValue("id", uuid.NewV7().String())
	rec := httptest.NewRecorder()

	NewEndpoints(store).Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}

	if store.receivedUpdate.Disabled == nil || !*store.receivedUpdate.Disabled {
		t.Error("disabled did not reach the store")
	}

	if store.receivedUpdate.URL != nil || store.receivedUpdate.Description != nil || store.receivedUpdate.EventTypes != nil {
		t.Error("absent fields must stay nil so the store leaves them untouched")
	}
}

func TestUpdateEndpointRejectsAnEmptyBody(t *testing.T) {
	req := authenticated(httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/v1/endpoints/x", strings.NewReader(`{}`)))
	req.SetPathValue("id", uuid.NewV7().String())
	rec := httptest.NewRecorder()

	NewEndpoints(&fakeEndpointStore{}).Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateEndpointRejectsPlainHTTP(t *testing.T) {
	req := authenticated(httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/v1/endpoints/x", strings.NewReader(`{"url":"http://example.com"}`)))
	req.SetPathValue("id", uuid.NewV7().String())
	rec := httptest.NewRecorder()

	NewEndpoints(&fakeEndpointStore{}).Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
