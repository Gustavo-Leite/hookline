package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	AppEnv      string
	HTTPPort    string
	DatabaseURL string
	RedisURL    string

	AllowPrivateDeliveryTargets bool

	RateLimitPerMinute int
	RateLimitBurst     int
}

func Load() (*Config, error) {
	cfg := &Config{
		AppEnv:      getEnv("APP_ENV", "development"),
		HTTPPort:    getEnv("HTTP_PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		RedisURL:    os.Getenv("REDIS_URL"),

		AllowPrivateDeliveryTargets: os.Getenv("ALLOW_PRIVATE_DELIVERY_TARGETS") == "true",

		RateLimitPerMinute: getEnvInt("RATE_LIMIT_PER_MINUTE", 600),
		RateLimitBurst:     getEnvInt("RATE_LIMIT_BURST", 60),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}
	if cfg.RedisURL == "" {
		return nil, fmt.Errorf("config: REDIS_URL is required")
	}

	return cfg, nil
}

func getEnvInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value < 1 {
		return fallback
	}

	return value
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
