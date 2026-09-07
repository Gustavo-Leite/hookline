package delivery

import (
	"encoding/json"
	"errors"
	"time"
	"uuid"
)

var ErrNotFound = errors.New("delivery: not found")

type Status string

const (
	StatusPending   Status = "pending"
	StatusSucceeded Status = "succeeded"
	StatusDead      Status = "dead"
)

type Delivery struct {
	ID            uuid.UUID
	EventID       uuid.UUID
	EndpointID    uuid.UUID
	ApplicationID uuid.UUID
	Status        Status
	AttemptCount  int
	NextAttemptAt time.Time
	CompletedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Attempt struct {
	ID            uuid.UUID
	DeliveryID    uuid.UUID
	AttemptNumber int
	StatusCode    *int
	Error         *string
	Duration      time.Duration
	AttemptedAt   time.Time
}

type AttemptOutcome struct {
	DeliveryID    uuid.UUID
	AttemptNumber int
	Result        Result
	Status        Status
	NextAttemptAt time.Time
}

type Job struct {
	DeliveryID   uuid.UUID
	AttemptCount int
	EventID      uuid.UUID
	EventType    string
	Payload      json.RawMessage
	URL          string
	Secret       string
}

type Result struct {
	StatusCode *int
	Error      error
	Duration   time.Duration
}

func (r Result) Succeeded() bool {
	return r.Error == nil && r.StatusCode != nil && *r.StatusCode >= 200 && *r.StatusCode < 300
}

func (r Result) Retryable() bool {
	if r.StatusCode == nil {
		return true
	}

	switch code := *r.StatusCode; {
	case code == 408, code == 429:
		return true
	case code >= 500:
		return true
	default:
		return false
	}
}
