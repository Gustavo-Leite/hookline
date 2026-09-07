package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gustavo-Leite/hookline/internal/delivery"
	"github.com/Gustavo-Leite/hookline/internal/secrets"
)

type DeliveryStore struct {
	pool   *pgxpool.Pool
	cipher *secrets.Cipher
}

func NewDeliveryStore(pool *pgxpool.Pool, cipher *secrets.Cipher) *DeliveryStore {
	return &DeliveryStore{pool: pool, cipher: cipher}
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

		if err := row.Scan(&job.DeliveryID, &job.AttemptCount, &job.EventID, &job.EventType, &job.Payload, &job.URL, &job.Secret); err != nil {
			return delivery.Job{}, err
		}

		secret, err := s.cipher.Decrypt(job.Secret)
		if err != nil {
			return delivery.Job{}, err
		}
		job.Secret = secret

		return job, nil
	})
}

func (s *DeliveryStore) PendingCount(ctx context.Context) (int, error) {
	query := `SELECT count(*) FROM deliveries WHERE status = 'pending' AND next_attempt_at <= now()`

	var pending int
	if err := s.pool.QueryRow(ctx, query).Scan(&pending); err != nil {
		return 0, fmt.Errorf("postgres: counting pending deliveries: %w", err)
	}

	return pending, nil
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

const deliveryColumns = `id, event_id, endpoint_id, application_id, status, attempt_count, next_attempt_at, completed_at, created_at, updated_at`

func (s *DeliveryStore) List(ctx context.Context, applicationID uuid.UUID, status string, limit int) ([]delivery.Delivery, error) {
	query := `
		SELECT ` + deliveryColumns + `
		FROM deliveries
		WHERE application_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3`

	rows, err := s.pool.Query(ctx, query, applicationID, status, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing deliveries: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (delivery.Delivery, error) {
		return scanDelivery(row)
	})
}

func (s *DeliveryStore) Get(ctx context.Context, applicationID, id uuid.UUID) (delivery.Delivery, error) {
	query := `SELECT ` + deliveryColumns + ` FROM deliveries WHERE application_id = $1 AND id = $2`

	found, err := scanDelivery(s.pool.QueryRow(ctx, query, applicationID, id))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return delivery.Delivery{}, delivery.ErrNotFound
	case err != nil:
		return delivery.Delivery{}, fmt.Errorf("postgres: getting delivery: %w", err)
	}

	return found, nil
}

func (s *DeliveryStore) Attempts(ctx context.Context, applicationID, deliveryID uuid.UUID) ([]delivery.Attempt, error) {
	query := `
		SELECT a.id, a.delivery_id, a.attempt_number, a.status_code, a.error, a.duration_ms, a.attempted_at
		FROM delivery_attempts a
		JOIN deliveries d ON d.id = a.delivery_id
		WHERE d.application_id = $1 AND a.delivery_id = $2
		ORDER BY a.attempt_number`

	rows, err := s.pool.Query(ctx, query, applicationID, deliveryID)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing delivery attempts: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (delivery.Attempt, error) {
		var (
			attempt    delivery.Attempt
			durationMS int64
		)

		err := row.Scan(&attempt.ID, &attempt.DeliveryID, &attempt.AttemptNumber,
			&attempt.StatusCode, &attempt.Error, &durationMS, &attempt.AttemptedAt)

		attempt.Duration = time.Duration(durationMS) * time.Millisecond

		return attempt, err
	})
}

func (s *DeliveryStore) Replay(ctx context.Context, applicationID, id uuid.UUID) (delivery.Delivery, error) {
	query := `
		UPDATE deliveries
		SET status          = 'pending',
		    attempt_count   = 0,
		    next_attempt_at = now(),
		    completed_at    = NULL,
		    updated_at      = now()
		WHERE application_id = $1 AND id = $2
		RETURNING ` + deliveryColumns

	replayed, err := scanDelivery(s.pool.QueryRow(ctx, query, applicationID, id))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return delivery.Delivery{}, delivery.ErrNotFound
	case err != nil:
		return delivery.Delivery{}, fmt.Errorf("postgres: replaying delivery: %w", err)
	}

	return replayed, nil
}

func scanDelivery(row scanner) (delivery.Delivery, error) {
	var d delivery.Delivery

	err := row.Scan(&d.ID, &d.EventID, &d.EndpointID, &d.ApplicationID, &d.Status,
		&d.AttemptCount, &d.NextAttemptAt, &d.CompletedAt, &d.CreatedAt, &d.UpdatedAt)

	return d, err
}
