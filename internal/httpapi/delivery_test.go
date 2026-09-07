package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/Gustavo-Leite/hookline/internal/delivery"
)

type fakeDeliveryStore struct {
	list           []delivery.Delivery
	attempts       []delivery.Attempt
	getErr         error
	replayErr      error
	receivedStatus string
	receivedLimit  int
}

func (f *fakeDeliveryStore) List(_ context.Context, _ uuid.UUID, status string, limit int) ([]delivery.Delivery, error) {
	f.receivedStatus = status
	f.receivedLimit = limit

	return f.list, nil
}

func (f *fakeDeliveryStore) Get(context.Context, uuid.UUID, uuid.UUID) (delivery.Delivery, error) {
	return delivery.Delivery{Status: delivery.StatusDead}, f.getErr
}

func (f *fakeDeliveryStore) Attempts(context.Context, uuid.UUID, uuid.UUID) ([]delivery.Attempt, error) {
	return f.attempts, nil
}

func (f *fakeDeliveryStore) Replay(context.Context, uuid.UUID, uuid.UUID) (delivery.Delivery, error) {
	return delivery.Delivery{Status: delivery.StatusPending, NextAttemptAt: time.Now()}, f.replayErr
}

func deliveryRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()

	req := authenticated(httptest.NewRequestWithContext(t.Context(), method, target, nil))
	req.SetPathValue("id", uuid.NewV7().String())

	return req
}

func TestListDeliveriesRejectsAnUnknownStatus(t *testing.T) {
	rec := httptest.NewRecorder()

	NewDeliveries(&fakeDeliveryStore{}).List(rec, deliveryRequest(t, http.MethodGet, "/v1/deliveries?status=whatever"))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListDeliveriesRejectsAnOutOfRangeLimit(t *testing.T) {
	for _, limit := range []string{"0", "-1", "201", "abc"} {
		rec := httptest.NewRecorder()

		NewDeliveries(&fakeDeliveryStore{}).List(rec, deliveryRequest(t, http.MethodGet, "/v1/deliveries?limit="+limit))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit=%s: status = %d, want %d", limit, rec.Code, http.StatusBadRequest)
		}
	}
}

func TestListDeliveriesPassesTheFilterThrough(t *testing.T) {
	store := &fakeDeliveryStore{}
	rec := httptest.NewRecorder()

	NewDeliveries(store).List(rec, deliveryRequest(t, http.MethodGet, "/v1/deliveries?status=dead&limit=10"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}

	if store.receivedStatus != "dead" || store.receivedLimit != 10 {
		t.Errorf("store received status=%q limit=%d, want dead and 10", store.receivedStatus, store.receivedLimit)
	}
}

func TestGetDeliveryReturnsItsAttempts(t *testing.T) {
	code := 500
	store := &fakeDeliveryStore{
		attempts: []delivery.Attempt{
			{AttemptNumber: 1, StatusCode: &code, Duration: 42 * time.Millisecond},
		},
	}

	rec := httptest.NewRecorder()
	NewDeliveries(store).Get(rec, deliveryRequest(t, http.MethodGet, "/v1/deliveries/x"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}

	var body struct {
		Attempts []attemptResponse `json:"attempts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	if len(body.Attempts) != 1 || body.Attempts[0].DurationMS != 42 {
		t.Errorf("attempts = %+v, want one attempt lasting 42ms", body.Attempts)
	}
}

func TestGetDeliveryNotFound(t *testing.T) {
	rec := httptest.NewRecorder()

	NewDeliveries(&fakeDeliveryStore{getErr: delivery.ErrNotFound}).
		Get(rec, deliveryRequest(t, http.MethodGet, "/v1/deliveries/x"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestReplayDeliveryIsAccepted(t *testing.T) {
	rec := httptest.NewRecorder()

	NewDeliveries(&fakeDeliveryStore{}).Replay(rec, deliveryRequest(t, http.MethodPost, "/v1/deliveries/x/replay"))

	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d (%s)", rec.Code, http.StatusAccepted, rec.Body)
	}
}
