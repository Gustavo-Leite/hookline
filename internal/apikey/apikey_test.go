package apikey

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	key, err := Generate(EnvLive)
	if err != nil {
		t.Fatalf("Generate() returned error: %v", err)
	}

	if !strings.HasPrefix(key.Plaintext, "hl_live_") {
		t.Errorf("Plaintext = %q, want it to start with %q", key.Plaintext, "hl_live_")
	}

	if key.Prefix != key.Plaintext[:prefixLength] {
		t.Errorf("Prefix = %q, want the first %d characters of the key", key.Prefix, prefixLength)
	}

	if len(key.Hash) != sha256.Size {
		t.Errorf("len(Hash) = %d, want %d", len(key.Hash), sha256.Size)
	}

	if !bytes.Equal(key.Hash, Hash(key.Plaintext)) {
		t.Error("Hash is not the hash of Plaintext")
	}
}

func TestGenerateNeverRepeats(t *testing.T) {
	const iterations = 1000

	seen := make(map[string]struct{}, iterations)
	for range iterations {
		key, err := Generate(EnvTest)
		if err != nil {
			t.Fatalf("Generate() returned error: %v", err)
		}

		if _, duplicate := seen[key.Plaintext]; duplicate {
			t.Fatalf("Generate() returned a duplicate key: %q", key.Plaintext)
		}
		seen[key.Plaintext] = struct{}{}
	}
}

func TestHash(t *testing.T) {
	const key = "hl_live_ZXhhbXBsZQ"

	if !bytes.Equal(Hash(key), Hash(key)) {
		t.Error("Hash is not deterministic")
	}

	if bytes.Equal(Hash(key), Hash(key+"x")) {
		t.Error("different inputs produced the same hash")
	}
}
