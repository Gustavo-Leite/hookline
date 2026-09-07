package delivery

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"
)

func testJob(url string) Job {
	return Job{
		DeliveryID: uuid.NewV7(),
		EventID:    uuid.NewV7(),
		EventType:  "user.created",
		Payload:    []byte(`{"id":42}`),
		URL:        url,
		Secret:     "whsec_test-secret",
	}
}

func TestSendSignsTheRequest(t *testing.T) {
	var (
		received  *http.Request
		body      []byte
		serverErr error
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r
		body, serverErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	job := testJob(server.URL)

	result := NewSender(SenderOptions{AllowPrivateTargets: true}).Send(t.Context(), job)

	if !result.Succeeded() {
		t.Fatalf("Send failed: %+v", result)
	}
	if serverErr != nil {
		t.Fatalf("reading the request body: %v", serverErr)
	}

	if string(body) != string(job.Payload) {
		t.Errorf("body = %s, want %s", body, job.Payload)
	}

	if got := received.Header.Get(EventIDHeader); got != job.EventID.String() {
		t.Errorf("%s = %q, want %q", EventIDHeader, got, job.EventID)
	}

	if got := received.Header.Get(EventTypeHeader); got != job.EventType {
		t.Errorf("%s = %q, want %q", EventTypeHeader, got, job.EventType)
	}

	if received.Header.Get(SignatureHeader) == "" {
		t.Error("the request carried no signature")
	}

	if received.Header.Get(TimestampHeader) == "" {
		t.Error("the request carried no timestamp")
	}
}

func TestSendProducesAVerifiableSignature(t *testing.T) {
	var verifyErr error

	job := testJob("")

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			verifyErr = err
			return
		}

		seconds, err := time.ParseDuration(r.Header.Get(TimestampHeader) + "s")
		if err != nil {
			verifyErr = err
			return
		}

		timestamp := time.Unix(int64(seconds.Seconds()), 0)

		verifyErr = Verify(job.Secret, r.Header.Get(SignatureHeader), job.EventID, timestamp, time.Now(), body)
	}))
	defer server.Close()

	job.URL = server.URL

	NewSender(SenderOptions{AllowPrivateTargets: true}).Send(t.Context(), job)

	if verifyErr != nil {
		t.Errorf("the receiver could not verify the signature: %v", verifyErr)
	}
}

func TestSendReportsTheStatusCode(t *testing.T) {
	tests := []struct {
		status        int
		wantSucceeded bool
		wantRetryable bool
	}{
		{status: http.StatusOK, wantSucceeded: true},
		{status: http.StatusBadRequest},
		{status: http.StatusInternalServerError, wantRetryable: true},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			result := NewSender(SenderOptions{AllowPrivateTargets: true}).Send(t.Context(), testJob(server.URL))

			if result.StatusCode == nil || *result.StatusCode != tt.status {
				t.Fatalf("StatusCode = %v, want %d", result.StatusCode, tt.status)
			}
			if result.Succeeded() != tt.wantSucceeded {
				t.Errorf("Succeeded() = %v, want %v", result.Succeeded(), tt.wantSucceeded)
			}
			if result.Retryable() != tt.wantRetryable {
				t.Errorf("Retryable() = %v, want %v", result.Retryable(), tt.wantRetryable)
			}
		})
	}
}

func TestSendDoesNotFollowRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "https://elsewhere.example.com/", http.StatusFound)
	}))
	defer server.Close()

	result := NewSender(SenderOptions{AllowPrivateTargets: true}).Send(t.Context(), testJob(server.URL))

	if result.StatusCode == nil || *result.StatusCode != http.StatusFound {
		t.Fatalf("StatusCode = %v, want %d", result.StatusCode, http.StatusFound)
	}

	if result.Succeeded() {
		t.Error("a redirect must not count as a successful delivery")
	}
}

func TestSendRefusesNonPublicTargets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := NewSender(SenderOptions{}).Send(t.Context(), testJob(server.URL))

	if result.Error == nil {
		t.Fatal("the sender connected to a loopback address")
	}

	if !errors.Is(result.Error, ErrBlockedTarget) {
		t.Errorf("error = %v, want it to wrap %v", result.Error, ErrBlockedTarget)
	}
}
