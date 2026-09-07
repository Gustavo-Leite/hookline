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

type APIKeyFinder interface {
	FindByHash(ctx context.Context, hash []byte) (apikey.Record, error)
}

type contextKey int

const (
	applicationIDKey contextKey = iota
	requestIDKey
)

func Authenticate(keys APIKeyFinder) func(http.Handler) http.Handler {
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
				slog.Error("looking up api key", "error", err)
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

			ctx := context.WithValue(r.Context(), applicationIDKey, record.ApplicationID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ApplicationID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(applicationIDKey).(uuid.UUID)
	return id, ok
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
