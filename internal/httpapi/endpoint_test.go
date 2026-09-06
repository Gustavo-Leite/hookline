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
	list      []endpoint.Endpoint
	getResult endpoint.Endpoint
	getErr    error
	deleteErr error
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

func (f *fakeEndpointStore) Delete(context.Context, uuid.UUID, uuid.UUID) error {
	return f.deleteErr
}

func authenticated(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), applicationIDKey, uuid.NewV7()))
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
