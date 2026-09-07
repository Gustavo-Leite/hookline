package endpoint

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"time"
	"uuid"
)

const secretBytes = 32

var (
	ErrNotFound   = errors.New("endpoint: not found")
	ErrInvalidURL = errors.New("endpoint: url must be an absolute https url")
)

type Endpoint struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	URL           string
	Description   string
	Secret        string
	EventTypes    []string
	DisabledAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type UpdateParams struct {
	URL         *string
	Description *string
	EventTypes  *[]string
	Disabled    *bool
}

func (p UpdateParams) IsEmpty() bool {
	return p.URL == nil && p.Description == nil && p.EventTypes == nil && p.Disabled == nil
}

func GenerateSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("endpoint: reading random bytes: %w", err)
	}

	return "whsec_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func ValidateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidURL
	}

	if parsed.Scheme != "https" || parsed.Host == "" {
		return ErrInvalidURL
	}

	return nil
}
