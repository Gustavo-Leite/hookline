package event

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"
	"uuid"
)

var ErrIdempotencyKeyReused = errors.New("event: idempotency key already used with a different payload")

type Event struct {
	ID             uuid.UUID
	ApplicationID  uuid.UUID
	Type           string
	Payload        json.RawMessage
	IdempotencyKey *string
	PayloadHash    []byte
	CreatedAt      time.Time
}

func HashPayload(eventType string, payload []byte) []byte {
	data := make([]byte, 0, len(eventType)+1+len(payload))
	data = append(data, eventType...)
	data = append(data, 0)
	data = append(data, payload...)

	sum := sha256.Sum256(data)
	return sum[:]
}
