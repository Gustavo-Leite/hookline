package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gustavo-Leite/hookline/internal/event"
)

const eventColumns = `id, application_id, event_type, payload, idempotency_key, payload_hash, created_at`

type EventStore struct {
	pool *pgxpool.Pool
}

func NewEventStore(pool *pgxpool.Pool) *EventStore {
	return &EventStore{pool: pool}
}

func (s *EventStore) Create(ctx context.Context, e event.Event) (event.Event, bool, error) {
	query := `
		INSERT INTO events (application_id, event_type, payload, idempotency_key, payload_hash)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (application_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING ` + eventColumns

	row := s.pool.QueryRow(ctx, query, e.ApplicationID, e.Type, e.Payload, e.IdempotencyKey, e.PayloadHash)

	created, err := scanEvent(row)
	switch {
	case err == nil:
		return created, false, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return event.Event{}, false, fmt.Errorf("postgres: creating event: %w", err)
	case e.IdempotencyKey == nil:
		return event.Event{}, false, errors.New("postgres: insert returned no row without an idempotency key")
	}

	existing, err := s.findByIdempotencyKey(ctx, e.ApplicationID, *e.IdempotencyKey)
	if err != nil {
		return event.Event{}, false, err
	}

	if !bytes.Equal(existing.PayloadHash, e.PayloadHash) {
		return event.Event{}, false, event.ErrIdempotencyKeyReused
	}

	return existing, true, nil
}

func (s *EventStore) findByIdempotencyKey(ctx context.Context, applicationID uuid.UUID, key string) (event.Event, error) {
	query := `SELECT ` + eventColumns + ` FROM events WHERE application_id = $1 AND idempotency_key = $2`

	found, err := scanEvent(s.pool.QueryRow(ctx, query, applicationID, key))
	if err != nil {
		return event.Event{}, fmt.Errorf("postgres: finding event by idempotency key: %w", err)
	}

	return found, nil
}

func scanEvent(row scanner) (event.Event, error) {
	var e event.Event

	err := row.Scan(
		&e.ID, &e.ApplicationID, &e.Type, &e.Payload,
		&e.IdempotencyKey, &e.PayloadHash, &e.CreatedAt,
	)

	return e, err
}
