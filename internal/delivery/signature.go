package delivery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"uuid"
)

const (
	SignatureHeader = "Hookline-Signature"
	TimestampHeader = "Hookline-Timestamp"
	EventIDHeader   = "Hookline-Event-Id"
	EventTypeHeader = "Hookline-Event-Type"

	signatureVersion = "v1"

	MaxClockSkew = 5 * time.Minute
)

var (
	ErrSignatureMalformed = errors.New("delivery: malformed signature header")
	ErrSignatureMismatch  = errors.New("delivery: signature does not match")
	ErrSignatureExpired   = errors.New("delivery: signature timestamp is outside the accepted window")
)

func Sign(secret string, eventID uuid.UUID, timestamp time.Time, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(signedContent(eventID, timestamp, payload))

	return signatureVersion + "," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func Verify(secret, header string, eventID uuid.UUID, timestamp, now time.Time, payload []byte) error {
	version, encoded, found := strings.Cut(header, ",")
	if !found || version != signatureVersion || encoded == "" {
		return ErrSignatureMalformed
	}

	if now.Sub(timestamp).Abs() > MaxClockSkew {
		return ErrSignatureExpired
	}

	provided, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ErrSignatureMalformed
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(signedContent(eventID, timestamp, payload))

	if !hmac.Equal(provided, mac.Sum(nil)) {
		return ErrSignatureMismatch
	}

	return nil
}

func signedContent(eventID uuid.UUID, timestamp time.Time, payload []byte) []byte {
	prefix := fmt.Sprintf("%s.%s.", eventID, strconv.FormatInt(timestamp.Unix(), 10))

	content := make([]byte, 0, len(prefix)+len(payload))
	content = append(content, prefix...)

	return append(content, payload...)
}
