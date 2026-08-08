package steam

import (
	"testing"
)

func TestCookieExportImport(t *testing.T) {
	client, err := NewClient(ClientConfig{Username: "testuser", SteamID: "76561198000000000"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	client.SetSessionCookies("testsessionid123", "76561198000000000||testloginsecuretoken", "testrefreshtoken")

	jsonStr, err := client.ExportCookiesJSON()
	if err != nil {
		t.Fatalf("ExportCookiesJSON failed: %v", err)
	}
	if jsonStr == "" {
		t.Fatalf("Expected non-empty JSON string")
	}

	newClient, err := NewClient(ClientConfig{Username: "testuser", SteamID: "76561198000000000"})
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}

	if err := newClient.ImportCookiesJSON(jsonStr); err != nil {
		t.Fatalf("ImportCookiesJSON failed: %v", err)
	}

	if newClient.SessionID != "testsessionid123" {
		t.Errorf("Expected SessionID 'testsessionid123', got '%s'", newClient.SessionID)
	}
	if newClient.SteamLoginSecure != "76561198000000000||testloginsecuretoken" {
		t.Errorf("Expected SteamLoginSecure token restored, got '%s'", newClient.SteamLoginSecure)
	}
	if !newClient.LoggedIn {
		t.Errorf("Expected LoggedIn to be true after importing session cookies")
	}

	// Test SaveCookiesToFile and LoadCookiesFromFile
	tmpFile := t.TempDir() + "/cookies.json"
	if err := client.SaveCookiesToFile(tmpFile); err != nil {
		t.Fatalf("SaveCookiesToFile failed: %v", err)
	}

	fileClient, _ := NewClient(ClientConfig{Username: "testuser", SteamID: "76561198000000000"})
	if err := fileClient.LoadCookiesFromFile(tmpFile); err != nil {
		t.Fatalf("LoadCookiesFromFile failed: %v", err)
	}

	if fileClient.SessionID != "testsessionid123" {
		t.Errorf("Expected SessionID 'testsessionid123' from file, got '%s'", fileClient.SessionID)
	}
}
