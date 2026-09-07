package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"uuid"

	"github.com/Gustavo-Leite/hookline/internal/apikey"
)

type APIKeyStore interface {
	FindByHash(ctx context.Context, hash []byte) (apikey.Record, error)
	TouchLastUsed(ctx context.Context, id uuid.UUID) error
}

const lastUsedInterval = 5 * time.Minute

type contextKey int

const (
	applicationIDKey contextKey = iota
	requestIDKey
)

func Authenticate(keys APIKeyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				deny(w, "missing or malformed Authorization header")
				return
			}

			record, err := keys.FindByHash(r.Context(), apikey.Hash(token))
			switch {
			case errors.Is(err, apikey.ErrNotFound):
				deny(w, "invalid api key")
				return
			case err != nil:
				slog.ErrorContext(r.Context(), "looking up api key", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
				return
			}

			switch err := record.Validate(time.Now()); {
			case errors.Is(err, apikey.ErrRevoked):
				deny(w, "api key revoked")
				return
			case errors.Is(err, apikey.ErrExpired):
				deny(w, "api key expired")
				return
			}

			recordUsage(r.Context(), keys, record)

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), applicationIDKey, record.ApplicationID)))
		})
	}
}

func ApplicationID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(applicationIDKey).(uuid.UUID)
	return id, ok
}

func recordUsage(ctx context.Context, keys APIKeyStore, record apikey.Record) {
	if record.LastUsedAt != nil && time.Since(*record.LastUsedAt) < lastUsedInterval {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		if err := keys.TouchLastUsed(ctx, record.ID); err != nil {
			slog.ErrorContext(ctx, "recording api key usage", "error", err)
		}
	}()
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

func deny(w http.ResponseWriter, reason string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="hookline"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": reason})
}
