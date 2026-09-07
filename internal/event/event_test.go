package event

import (
	"bytes"
	"testing"
)

func TestHashPayloadIsDeterministic(t *testing.T) {
	a := HashPayload("user.created", []byte(`{"id":42}`))
	b := HashPayload("user.created", []byte(`{"id":42}`))

	if !bytes.Equal(a, b) {
		t.Error("the same type and payload produced different hashes")
	}
}

func TestHashPayloadCoversTypeAndPayload(t *testing.T) {
	base := HashPayload("user.created", []byte(`{"id":42}`))

	if bytes.Equal(base, HashPayload("user.deleted", []byte(`{"id":42}`))) {
		t.Error("a different event type produced the same hash")
	}

	if bytes.Equal(base, HashPayload("user.created", []byte(`{"id":43}`))) {
		t.Error("a different payload produced the same hash")
	}
}

func TestHashPayloadSeparatesTypeFromPayload(t *testing.T) {
	if bytes.Equal(HashPayload("ab", []byte("c")), HashPayload("a", []byte("bc"))) {
		t.Error("type and payload are concatenated without a separator")
	}
}
