package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
	"uuid"
)

const (
	EnvLive = "live"
	EnvTest = "test"

	tokenBytes   = 32
	prefixLength = 12
)

var (
	ErrNotFound = errors.New("apikey: not found")
	ErrRevoked  = errors.New("apikey: revoked")
	ErrExpired  = errors.New("apikey: expired")
)

type Key struct {
	Plaintext string
	Prefix    string
	Hash      []byte
}

type Record struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Name          string
	Prefix        string
	LastUsedAt    *time.Time
	ExpiresAt     *time.Time
	RevokedAt     *time.Time
	CreatedAt     time.Time
}

func (r Record) Validate(now time.Time) error {
	if r.RevokedAt != nil {
		return ErrRevoked
	}
	if r.ExpiresAt != nil && !r.ExpiresAt.After(now) {
		return ErrExpired
	}
	return nil
}

func Generate(environment string) (Key, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return Key{}, fmt.Errorf("apikey: reading random bytes: %w", err)
	}

	plaintext := fmt.Sprintf("hl_%s_%s", environment, base64.RawURLEncoding.EncodeToString(buf))

	return Key{
		Plaintext: plaintext,
		Prefix:    plaintext[:prefixLength],
		Hash:      Hash(plaintext),
	}, nil
}

func Hash(plaintext string) []byte {
	sum := sha256.Sum256([]byte(plaintext))
	return sum[:]
}
