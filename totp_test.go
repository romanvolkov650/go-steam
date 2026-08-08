package steam

import (
	"testing"
)

func TestGenerateTwoFactorCode(t *testing.T) {
	// Sample shared secret (base64 encoded key)
	sharedSecret := "p101mHjU039qXyWvJ/4u+YtK7bY="
	timestamp := int64(1700000000)

	code, err := GenerateTwoFactorCode(sharedSecret, timestamp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(code) != 5 {
		t.Fatalf("expected 5-character code, got length %d (%s)", len(code), code)
	}

	t.Logf("Generated 2FA Code for t=%d: %s", timestamp, code)
}

func TestGenerateConfirmationHash(t *testing.T) {
	identitySecret := "wE6W9+m1+2XyWvJ/4u+YtK7bY18="
	timestamp := int64(1700000000)
	tag := "conf"

	hash, err := GenerateConfirmationHash(identitySecret, tag, timestamp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash == "" {
		t.Fatalf("expected non-empty hash")
	}

	t.Logf("Generated Confirmation Hash for tag '%s': %s", tag, hash)
}
