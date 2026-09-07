package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gustavo-Leite/hookline/internal/delivery"
)

type DeliveryStore struct {
	pool *pgxpool.Pool
}

func NewDeliveryStore(pool *pgxpool.Pool) *DeliveryStore {
	return &DeliveryStore{pool: pool}
}

func (s *DeliveryStore) Claim(ctx context.Context, limit int, lease time.Duration) ([]delivery.Job, error) {
	query := `
		WITH claimed AS (
			SELECT id FROM deliveries
			WHERE status = 'pending' AND next_attempt_at <= now()
			ORDER BY next_attempt_at
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		),
		leased AS (
			UPDATE deliveries d
			SET attempt_count   = d.attempt_count + 1,
			    next_attempt_at = now() + make_interval(secs => $2),
			    updated_at      = now()
			FROM claimed c
			WHERE d.id = c.id
			RETURNING d.id, d.attempt_count, d.event_id, d.endpoint_id
		)
		SELECT l.id, l.attempt_count, ev.id, ev.event_type, ev.payload, ep.url, ep.secret
		FROM leased l
		JOIN events ev ON ev.id = l.event_id
		JOIN endpoints ep ON ep.id = l.endpoint_id`

	rows, err := s.pool.Query(ctx, query, limit, lease.Seconds())
	if err != nil {
		return nil, fmt.Errorf("postgres: claiming deliveries: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (delivery.Job, error) {
		var job delivery.Job

		err := row.Scan(&job.DeliveryID, &job.AttemptCount, &job.EventID, &job.EventType, &job.Payload, &job.URL, &job.Secret)

		return job, err
	})
}

func (s *DeliveryStore) RecordAttempt(ctx context.Context, outcome delivery.AttemptOutcome) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var failure *string
	if outcome.Result.Error != nil {
		message := outcome.Result.Error.Error()
		failure = &message
	}

	attemptQuery := `
		INSERT INTO delivery_attempts (delivery_id, attempt_number, status_code, error, duration_ms)
		VALUES ($1, $2, $3, $4, $5)`

	_, err = tx.Exec(ctx, attemptQuery,
		outcome.DeliveryID,
		outcome.AttemptNumber,
		outcome.Result.StatusCode,
		failure,
		outcome.Result.Duration.Milliseconds(),
	)
	if err != nil {
		return fmt.Errorf("postgres: recording delivery attempt: %w", err)
	}

	deliveryQuery := `
		UPDATE deliveries
		SET status          = $2,
		    next_attempt_at = $3,
		    completed_at    = CASE WHEN $2 = 'pending' THEN NULL ELSE now() END,
		    updated_at      = now()
		WHERE id = $1`

	if _, err := tx.Exec(ctx, deliveryQuery, outcome.DeliveryID, string(outcome.Status), outcome.NextAttemptAt); err != nil {
		return fmt.Errorf("postgres: updating delivery: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: committing delivery attempt: %w", err)
	}

	return nil
}
