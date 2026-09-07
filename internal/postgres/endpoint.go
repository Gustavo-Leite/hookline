package postgres

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gustavo-Leite/hookline/internal/endpoint"
)

const endpointColumns = `id, application_id, url, description, secret, event_types, disabled_at, created_at, updated_at`

type scanner interface {
	Scan(dest ...any) error
}

type EndpointStore struct {
	pool *pgxpool.Pool
}

func NewEndpointStore(pool *pgxpool.Pool) *EndpointStore {
	return &EndpointStore{pool: pool}
}

func (s *EndpointStore) Create(ctx context.Context, e endpoint.Endpoint) (endpoint.Endpoint, error) {
	query := `
		INSERT INTO endpoints (application_id, url, description, secret, event_types)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + endpointColumns

	row := s.pool.QueryRow(ctx, query, e.ApplicationID, e.URL, e.Description, e.Secret, e.EventTypes)

	created, err := scanEndpoint(row)
	if err != nil {
		return endpoint.Endpoint{}, fmt.Errorf("postgres: creating endpoint: %w", err)
	}

	return created, nil
}

func (s *EndpointStore) List(ctx context.Context, applicationID uuid.UUID) ([]endpoint.Endpoint, error) {
	query := `SELECT ` + endpointColumns + ` FROM endpoints WHERE application_id = $1 ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, query, applicationID)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing endpoints: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (endpoint.Endpoint, error) {
		return scanEndpoint(row)
	})
}

func (s *EndpointStore) Get(ctx context.Context, applicationID, id uuid.UUID) (endpoint.Endpoint, error) {
	query := `SELECT ` + endpointColumns + ` FROM endpoints WHERE application_id = $1 AND id = $2`

	found, err := scanEndpoint(s.pool.QueryRow(ctx, query, applicationID, id))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return endpoint.Endpoint{}, endpoint.ErrNotFound
	case err != nil:
		return endpoint.Endpoint{}, fmt.Errorf("postgres: getting endpoint: %w", err)
	}

	return found, nil
}

func (s *EndpointStore) Update(ctx context.Context, applicationID, id uuid.UUID, params endpoint.UpdateParams) (endpoint.Endpoint, error) {
	query := `
		UPDATE endpoints SET
			url         = COALESCE($3, url),
			description = COALESCE($4, description),
			event_types = COALESCE($5, event_types),
			disabled_at = CASE
				WHEN $6::boolean IS NULL THEN disabled_at
				WHEN $6::boolean         THEN COALESCE(disabled_at, now())
				ELSE NULL
			END,
			updated_at  = now()
		WHERE application_id = $1 AND id = $2
		RETURNING ` + endpointColumns

	row := s.pool.QueryRow(ctx, query, applicationID, id, params.URL, params.Description, params.EventTypes, params.Disabled)

	updated, err := scanEndpoint(row)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return endpoint.Endpoint{}, endpoint.ErrNotFound
	case err != nil:
		return endpoint.Endpoint{}, fmt.Errorf("postgres: updating endpoint: %w", err)
	}

	return updated, nil
}

func (s *EndpointStore) Delete(ctx context.Context, applicationID, id uuid.UUID) error {
	query := `DELETE FROM endpoints WHERE application_id = $1 AND id = $2`

	tag, err := s.pool.Exec(ctx, query, applicationID, id)
	if err != nil {
		return fmt.Errorf("postgres: deleting endpoint: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return endpoint.ErrNotFound
	}

	return nil
}

func scanEndpoint(row scanner) (endpoint.Endpoint, error) {
	var e endpoint.Endpoint

	err := row.Scan(
		&e.ID, &e.ApplicationID, &e.URL, &e.Description, &e.Secret,
		&e.EventTypes, &e.DisabledAt, &e.CreatedAt, &e.UpdatedAt,
	)

	return e, err
}
