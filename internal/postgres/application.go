package postgres

import (
	"context"
	"fmt"

	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ApplicationStore struct {
	pool *pgxpool.Pool
}

func NewApplicationStore(pool *pgxpool.Pool) *ApplicationStore {
	return &ApplicationStore{pool: pool}
}

func (s *ApplicationStore) Create(ctx context.Context, name string) (uuid.UUID, error) {
	query := `INSERT INTO applications (name) VALUES ($1) RETURNING id`

	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, query, name).Scan(&id); err != nil {
		return uuid.Nil(), fmt.Errorf("postgres: creating application: %w", err)
	}

	return id, nil
}
