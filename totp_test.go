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

func TestGenerateDeviceID(t *testing.T) {
	steamID := "76561198000000000"
	devID1 := GenerateDeviceID(steamID)
	devID2 := GenerateDeviceID(steamID)

	if devID1 != devID2 {
		t.Errorf("expected deterministic device ID, got %s and %s", devID1, devID2)
	}

	if len(devID1) != 44 { // android:8-4-4-4-12 = 8 + 36 = 44
		t.Errorf("expected device ID length 44, got %d (%s)", len(devID1), devID1)
	}

	client, err := NewClient(ClientConfig{SteamID: steamID, Username: "testuser"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	gotID := client.getDeviceID()
	if gotID != devID1 {
		t.Errorf("expected client.getDeviceID() to match GenerateDeviceID, got %s vs %s", gotID, devID1)
	}
	if client.Config.DeviceID != devID1 {
		t.Errorf("expected client.Config.DeviceID to be cached, got %s", client.Config.DeviceID)
	}
}
