package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/Gustavo-Leite/hookline/internal/apikey"
	"github.com/Gustavo-Leite/hookline/internal/config"
	"github.com/Gustavo-Leite/hookline/internal/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 3 || os.Args[1] != "create-application" {
		return errors.New("usage: hookline-admin create-application <name>")
	}
	name := os.Args[2]

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

	applicationID, err := postgres.NewApplicationStore(pool).Create(ctx, name)
	if err != nil {
		return err
	}

	environment := apikey.EnvTest
	if cfg.AppEnv == "production" {
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
