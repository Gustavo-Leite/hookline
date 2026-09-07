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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return event.Event{}, false, fmt.Errorf("postgres: beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	query := `
		INSERT INTO events (application_id, event_type, payload, idempotency_key, payload_hash)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (application_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING ` + eventColumns

	row := tx.QueryRow(ctx, query, e.ApplicationID, e.Type, e.Payload, e.IdempotencyKey, e.PayloadHash)

	created, err := scanEvent(row)
	switch {
	case err == nil:
		if err := fanOut(ctx, tx, created); err != nil {
			return event.Event{}, false, err
		}

		if err := tx.Commit(ctx); err != nil {
			return event.Event{}, false, fmt.Errorf("postgres: committing event: %w", err)
		}

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

func fanOut(ctx context.Context, tx pgx.Tx, e event.Event) error {
	query := `
		INSERT INTO deliveries (event_id, endpoint_id, application_id)
		SELECT $1, endpoints.id, endpoints.application_id
		FROM endpoints
		WHERE endpoints.application_id = $2
		  AND endpoints.disabled_at IS NULL
		  AND (cardinality(endpoints.event_types) = 0 OR $3 = ANY (endpoints.event_types))`

	if _, err := tx.Exec(ctx, query, e.ID, e.ApplicationID, e.Type); err != nil {
		return fmt.Errorf("postgres: fanning out event: %w", err)
	}

	return nil
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
