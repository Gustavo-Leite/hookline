package delivery

import (
	"errors"
	"testing"
	"time"
	"uuid"
)

const secret = "whsec_test-secret"

func TestSignAndVerify(t *testing.T) {
	var (
		eventID = uuid.NewV7()
		now     = time.Now()
		payload = []byte(`{"id":42}`)
	)

	signature := Sign(secret, eventID, now, payload)

	if err := Verify(secret, signature, eventID, now, now, payload); err != nil {
		t.Fatalf("a freshly produced signature did not verify: %v", err)
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	var (
		eventID = uuid.NewV7()
		now     = time.Now()
		payload = []byte(`{"amount":10}`)
	)

	signature := Sign(secret, eventID, now, payload)

	tests := []struct {
		name      string
		secret    string
		eventID   uuid.UUID
		timestamp time.Time
		payload   []byte
		want      error
	}{
		{
			name:    "payload changed",
			secret:  secret,
			eventID: eventID, timestamp: now,
			payload: []byte(`{"amount":1000}`),
			want:    ErrSignatureMismatch,
		},
		{
			name:    "different secret",
			secret:  "whsec_other",
			eventID: eventID, timestamp: now,
			payload: payload,
			want:    ErrSignatureMismatch,
		},
		{
			name:    "event id changed",
			secret:  secret,
			eventID: uuid.NewV7(), timestamp: now,
			payload: payload,
			want:    ErrSignatureMismatch,
		},
		{
			name:    "timestamp changed",
			secret:  secret,
			eventID: eventID, timestamp: now.Add(time.Minute),
			payload: payload,
			want:    ErrSignatureMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Verify(tt.secret, signature, tt.eventID, tt.timestamp, now, tt.payload)
			if !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestVerifyRejectsReplayOutsideTheWindow(t *testing.T) {
	var (
		eventID = uuid.NewV7()
		signed  = time.Now()
		payload = []byte(`{"id":42}`)
	)

	signature := Sign(secret, eventID, signed, payload)

	captured := signed.Add(MaxClockSkew + time.Second)
	if err := Verify(secret, signature, eventID, signed, captured, payload); !errors.Is(err, ErrSignatureExpired) {
		t.Errorf("error = %v, want %v", err, ErrSignatureExpired)
	}

	fresh := signed.Add(MaxClockSkew - time.Second)
	if err := Verify(secret, signature, eventID, signed, fresh, payload); err != nil {
		t.Errorf("a signature inside the window was rejected: %v", err)
	}
}

func TestVerifyRejectsMalformedHeaders(t *testing.T) {
	eventID := uuid.NewV7()
	now := time.Now()

	for _, header := range []string{"", "abc", "v1,", "v2,YWJj", ",YWJj", "v1,not-base64!"} {
		if err := Verify(secret, header, eventID, now, now, nil); err == nil {
			t.Errorf("header %q was accepted", header)
		}
	}
}
