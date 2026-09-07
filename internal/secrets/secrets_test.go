package secrets

import (
	"errors"
	"strings"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()

	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}

	return c
}

func TestEncryptThenDecrypt(t *testing.T) {
	const plaintext = "whsec_tW2O_9-gAijcIHRHcLVEaU6s1R4ZR72JkaRl57Tn4Ek"

	c := newTestCipher(t)

	stored, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if strings.Contains(stored, plaintext) {
		t.Fatal("the stored value contains the plaintext")
	}

	decrypted, err := c.Decrypt(stored)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypt = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptIsNotDeterministic(t *testing.T) {
	c := newTestCipher(t)

	first, _ := c.Encrypt("whsec_same")
	second, _ := c.Encrypt("whsec_same")

	if first == second {
		t.Error("encrypting the same value twice produced the same ciphertext, so the nonce is being reused")
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	c := newTestCipher(t)

	stored, _ := c.Encrypt("whsec_original")

	tampered := stored[:len(stored)-2] + "AA"
	if _, err := c.Decrypt(tampered); !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("error = %v, want %v", err, ErrInvalidCiphertext)
	}
}

func TestDecryptRejectsAnotherKey(t *testing.T) {
	stored, _ := newTestCipher(t).Encrypt("whsec_original")

	if _, err := newTestCipher(t).Decrypt(stored); !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("error = %v, want %v", err, ErrInvalidCiphertext)
	}
}

func TestDecryptPassesThroughLegacyPlaintext(t *testing.T) {
	const legacy = "whsec_written_before_encryption_existed"

	decrypted, err := newTestCipher(t).Decrypt(legacy)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if decrypted != legacy {
		t.Errorf("Decrypt = %q, want the value unchanged", decrypted)
	}

	if IsEncrypted(legacy) {
		t.Error("a legacy value was reported as encrypted")
	}
}

func TestNewCipherRejectsBadKeys(t *testing.T) {
	for _, key := range []string{"", "not-base64!", "c2hvcnQ="} {
		if _, err := NewCipher(key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("key %q: error = %v, want %v", key, err, ErrInvalidKey)
		}
	}
}
