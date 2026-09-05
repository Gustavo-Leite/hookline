package config

import "testing"

const (
	databaseURL = "postgres://user:pass@localhost:5432/hookline?sslmode=disable"
	redisURL    = "redis://localhost:6379/0"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", databaseURL)
	t.Setenv("REDIS_URL", redisURL)
}

func TestLoadAppliesDefaults(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_ENV", "")
	t.Setenv("HTTP_PORT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.AppEnv != "development" {
		t.Errorf("AppEnv = %q, want %q", cfg.AppEnv, "development")
	}
	if cfg.HTTPPort != "8080" {
		t.Errorf("HTTPPort = %q, want %q", cfg.HTTPPort, "8080")
	}
}

func TestLoadReadsEnvironment(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("HTTP_PORT", "9000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.AppEnv != "production" {
		t.Errorf("AppEnv = %q, want %q", cfg.AppEnv, "production")
	}
	if cfg.HTTPPort != "9000" {
		t.Errorf("HTTPPort = %q, want %q", cfg.HTTPPort, "9000")
	}
	if cfg.DatabaseURL != databaseURL {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, databaseURL)
	}
	if cfg.RedisURL != redisURL {
		t.Errorf("RedisURL = %q, want %q", cfg.RedisURL, redisURL)
	}
}

func TestLoadRequiresConnectionStrings(t *testing.T) {
	tests := []struct {
		name        string
		databaseURL string
		redisURL    string
	}{
		{name: "missing DATABASE_URL", redisURL: redisURL},
		{name: "missing REDIS_URL", databaseURL: databaseURL},
		{name: "missing both"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", tt.databaseURL)
			t.Setenv("REDIS_URL", tt.redisURL)

			if _, err := Load(); err == nil {
				t.Error("Load() returned no error, want one")
			}
		})
	}
}
