package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/Gustavo-Leite/hookline/api"
	"github.com/Gustavo-Leite/hookline/internal/apikey"
	"github.com/Gustavo-Leite/hookline/internal/endpoint"
	"github.com/Gustavo-Leite/hookline/internal/event"
)

var (
	specOnce   sync.Once
	specRouter routers.Router
	specErr    error
)

func loadSpec(t *testing.T) routers.Router {
	t.Helper()

	specOnce.Do(func() {
		loader := openapi3.NewLoader()

		doc, err := loader.LoadFromData(api.Spec)
		if err != nil {
			specErr = err
			return
		}

		if err := doc.Validate(context.Background()); err != nil {
			specErr = err
			return
		}

		specRouter, specErr = gorillamux.NewRouter(doc)
	})

	if specErr != nil {
		t.Fatalf("loading api/openapi.yaml: %v", specErr)
	}

	return specRouter
}

func specRequest(t *testing.T, method, target string, body io.Reader) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, "http://localhost:8080"+target, body)
	req.Header.Set("Content-Type", "application/json")

	return req
}

func assertMatchesSpec(t *testing.T, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()

	route, pathParams, err := loadSpec(t).FindRoute(req)
	if err != nil {
		t.Fatalf("%s %s is not described in the specification: %v", req.Method, req.URL.Path, err)
	}

	input := &openapi3filter.RequestValidationInput{
		Request:    req,
		PathParams: pathParams,
		Route:      route,
		Options:    &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	}

	err = openapi3filter.ValidateResponse(t.Context(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: input,
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Body:                   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		Options: &openapi3filter.Options{
			IncludeResponseStatus: true,
			AuthenticationFunc:    openapi3filter.NoopAuthenticationFunc,
		},
	})
	if err != nil {
		t.Errorf("the %d response of %s %s does not match the specification: %v\nbody: %s",
			rec.Code, req.Method, req.URL.Path, err, rec.Body)
	}
}

type fakeAPIKeyFinder struct {
	record apikey.Record
	err    error
}

func (f fakeAPIKeyFinder) FindByHash(context.Context, []byte) (apikey.Record, error) {
	return f.record, f.err
}

func TestHealthMatchesSpec(t *testing.T) {
	req := specRequest(t, http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	Health().ServeHTTP(rec, req)

	assertMatchesSpec(t, req, rec)
}

func TestReadyMatchesSpec(t *testing.T) {
	tests := []struct {
		name     string
		postgres error
	}{
		{name: "healthy"},
		{name: "degraded", postgres: errors.New("connection refused")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := specRequest(t, http.MethodGet, "/readyz", nil)
			rec := httptest.NewRecorder()

			Ready(stubPinger{tt.postgres}, stubPinger{nil}).ServeHTTP(rec, req)

			assertMatchesSpec(t, req, rec)
		})
	}
}

func TestMeMatchesSpec(t *testing.T) {
	req := authenticated(specRequest(t, http.MethodGet, "/v1/me", nil))
	rec := httptest.NewRecorder()

	Me().ServeHTTP(rec, req)

	assertMatchesSpec(t, req, rec)
}

func TestUnauthorizedMatchesSpec(t *testing.T) {
	req := specRequest(t, http.MethodGet, "/v1/me", nil)
	rec := httptest.NewRecorder()

	Authenticate(fakeAPIKeyFinder{err: apikey.ErrNotFound})(Me()).ServeHTTP(rec, req)

	assertMatchesSpec(t, req, rec)
}

func TestEndpointResponsesMatchSpec(t *testing.T) {
	const id = "01a07478-4c52-78d4-bb1e-5a87a196f5ec"

	handlers := NewEndpoints(&fakeEndpointStore{
		list: []endpoint.Endpoint{{
			URL:        "https://example.com/hooks",
			Secret:     "whsec_secret",
			EventTypes: []string{"user.created"},
		}},
	})

	t.Run("created", func(t *testing.T) {
		req := authenticated(specRequest(t, http.MethodPost, "/v1/endpoints",
			strings.NewReader(`{"url":"https://example.com/hooks"}`)))
		rec := httptest.NewRecorder()

		handlers.Create(rec, req)

		assertMatchesSpec(t, req, rec)
	})

	t.Run("invalid url", func(t *testing.T) {
		req := authenticated(specRequest(t, http.MethodPost, "/v1/endpoints",
			strings.NewReader(`{"url":"http://example.com/hooks"}`)))
		rec := httptest.NewRecorder()

		handlers.Create(rec, req)

		assertMatchesSpec(t, req, rec)
	})

	t.Run("listed", func(t *testing.T) {
		req := authenticated(specRequest(t, http.MethodGet, "/v1/endpoints", nil))
		rec := httptest.NewRecorder()

		handlers.List(rec, req)

		assertMatchesSpec(t, req, rec)
	})

	t.Run("not found", func(t *testing.T) {
		req := authenticated(specRequest(t, http.MethodDelete, "/v1/endpoints/"+id, nil))
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()

		NewEndpoints(&fakeEndpointStore{deleteErr: endpoint.ErrNotFound}).Delete(rec, req)

		assertMatchesSpec(t, req, rec)
	})
}

func TestEventResponsesMatchSpec(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		req := authenticated(specRequest(t, http.MethodPost, "/v1/events",
			strings.NewReader(`{"type":"user.created","payload":{"id":42}}`)))
		rec := httptest.NewRecorder()

		NewEvents(&fakeEventStore{}).Create(rec, req)

		assertMatchesSpec(t, req, rec)
	})

	t.Run("reused idempotency key", func(t *testing.T) {
		req := authenticated(specRequest(t, http.MethodPost, "/v1/events",
			strings.NewReader(`{"type":"user.created","payload":{"id":42}}`)))
		req.Header.Set("Idempotency-Key", "abc")
		rec := httptest.NewRecorder()

		NewEvents(&fakeEventStore{err: event.ErrIdempotencyKeyReused}).Create(rec, req)

		assertMatchesSpec(t, req, rec)
	})
}
