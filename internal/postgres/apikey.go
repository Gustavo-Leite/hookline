package postgres

import (
	"context"
	"errors"
	"fmt"

	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gustavo-Leite/hookline/internal/apikey"
)

const apiKeyColumns = `id, application_id, name, prefix, last_used_at, expires_at, revoked_at, created_at`

type APIKeyStore struct {
	pool *pgxpool.Pool
}

func NewAPIKeyStore(pool *pgxpool.Pool) *APIKeyStore {
	return &APIKeyStore{pool: pool}
}

func (s *APIKeyStore) Create(ctx context.Context, applicationID uuid.UUID, name string, key apikey.Key) (apikey.Record, error) {
	query := `
		INSERT INTO api_keys (application_id, name, prefix, token_hash)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + apiKeyColumns

	return scanAPIKey(s.pool.QueryRow(ctx, query, applicationID, name, key.Prefix, key.Hash))
}

func (s *APIKeyStore) FindByHash(ctx context.Context, hash []byte) (apikey.Record, error) {
	query := `SELECT ` + apiKeyColumns + ` FROM api_keys WHERE token_hash = $1`

	record, err := scanAPIKey(s.pool.QueryRow(ctx, query, hash))
	if errors.Is(err, pgx.ErrNoRows) {
		return apikey.Record{}, apikey.ErrNotFound
	}

	return record, err
}

func scanAPIKey(row pgx.Row) (apikey.Record, error) {
	var r apikey.Record

	err := row.Scan(
		&r.ID, &r.ApplicationID, &r.Name, &r.Prefix,
		&r.LastUsedAt, &r.ExpiresAt, &r.RevokedAt, &r.CreatedAt,
	)
	if err != nil {
		return apikey.Record{}, fmt.Errorf("postgres: scanning api key: %w", err)
	}

	return r, nil
}
