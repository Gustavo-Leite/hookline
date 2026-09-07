package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	KeyBytes = 32

	version       = "v1"
	versionPrefix = version + "."
)

var (
	ErrInvalidKey        = errors.New("secrets: the encryption key must be 32 random bytes, base64 encoded")
	ErrInvalidCiphertext = errors.New("secrets: stored value is not decryptable")
)

type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(encodedKey string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
	if err != nil || len(key) != KeyBytes {
		return nil, ErrInvalidKey
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: creating cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: creating gcm: %w", err)
	}

	return &Cipher{aead: aead}, nil
}

func GenerateKey() (string, error) {
	key := make([]byte, KeyBytes)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("secrets: reading random bytes: %w", err)
	}

	return base64.StdEncoding.EncodeToString(key), nil
}

func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secrets: reading nonce: %w", err)
	}

	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)

	return versionPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *Cipher) Decrypt(stored string) (string, error) {
	encoded, found := strings.CutPrefix(stored, versionPrefix)
	if !found {
		return stored, nil
	}

	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(sealed) < c.aead.NonceSize() {
		return "", ErrInvalidCiphertext
	}

	nonce, ciphertext := sealed[:c.aead.NonceSize()], sealed[c.aead.NonceSize():]

	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrInvalidCiphertext
	}

	return string(plaintext), nil
}

func IsEncrypted(stored string) bool {
	return strings.HasPrefix(stored, versionPrefix)
}
