package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/Gustavo-Leite/hookline/internal/apikey"
	"github.com/Gustavo-Leite/hookline/internal/config"
	"github.com/Gustavo-Leite/hookline/internal/postgres"
	"github.com/Gustavo-Leite/hookline/internal/secrets"
)

const usage = `usage:
  hookline-admin generate-key
  hookline-admin create-application <name>
  hookline-admin list-keys <application-id>
  hookline-admin revoke-key <key-id>`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "generate-key" {
		key, err := secrets.GenerateKey()
		if err != nil {
			return err
		}

		fmt.Printf("SECRET_ENCRYPTION_KEY=%s\n", key)

		return nil
	}

	if len(os.Args) != 3 {
		return errors.New(usage)
	}

	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch os.Args[1] {
	case "create-application":
		return createApplication(ctx, pool, cfg.AppEnv, os.Args[2])
	case "list-keys":
		return listKeys(ctx, pool, os.Args[2])
	case "revoke-key":
		return revokeKey(ctx, pool, os.Args[2])
	default:
		return errors.New(usage)
	}
}

func createApplication(ctx context.Context, pool *pgxpool.Pool, appEnv, name string) error {
	applicationID, err := postgres.NewApplicationStore(pool).Create(ctx, name)
	if err != nil {
		return err
	}

	environment := apikey.EnvTest
	if appEnv == "production" {
		environment = apikey.EnvLive
	}

	key, err := apikey.Generate(environment)
	if err != nil {
		return err
	}

	if _, err := postgres.NewAPIKeyStore(pool).Create(ctx, applicationID, "default", key); err != nil {
		return err
	}

	fmt.Printf("application: %s (%s)\n", name, applicationID)
	fmt.Printf("api key:     %s\n\n", key.Plaintext)
	fmt.Println("This key is shown once and cannot be recovered. Store it now.")

	return nil
}

func listKeys(ctx context.Context, pool *pgxpool.Pool, rawApplicationID string) error {
	applicationID, err := uuid.Parse(rawApplicationID)
	if err != nil {
		return fmt.Errorf("%q is not a valid application id", rawApplicationID)
	}

	keys, err := postgres.NewAPIKeyStore(pool).ListByApplication(ctx, applicationID)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		fmt.Println("no api keys for this application")
		return nil
	}

	out := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(out, "ID\tNAME\tPREFIX\tCREATED\tLAST USED\tREVOKED")

	for _, key := range keys {
		_, _ = fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\t%s\n",
			key.ID, key.Name, key.Prefix,
			key.CreatedAt.Format(time.DateOnly),
			formatTime(key.LastUsedAt),
			formatTime(key.RevokedAt),
		)
	}

	return out.Flush()
}

func revokeKey(ctx context.Context, pool *pgxpool.Pool, rawKeyID string) error {
	keyID, err := uuid.Parse(rawKeyID)
	if err != nil {
		return fmt.Errorf("%q is not a valid key id", rawKeyID)
	}

	revoked, err := postgres.NewAPIKeyStore(pool).Revoke(ctx, keyID)
	if errors.Is(err, apikey.ErrNotFound) {
		return fmt.Errorf("no api key with id %s", keyID)
	}
	if err != nil {
		return err
	}

	fmt.Printf("revoked %s (%s) at %s\n", revoked.Prefix, revoked.Name, revoked.RevokedAt.Format(time.RFC3339))

	return nil
}

func formatTime(t *time.Time) string {
	if t == nil {
		return "-"
	}

	return t.Format(time.RFC3339)
}
