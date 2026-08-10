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

func TestSteampyCookieImport(t *testing.T) {
	steampyJSON := `[
		{
			"name": "sessionid",
			"value": "ab7a216fa67603f775969316",
			"domain": "steamcommunity.com",
			"path": "/",
			"expires": null,
			"secure": false,
			"discard": true,
			"rest": { "HttpOnly": null }
		},
		{
			"name": "steamLoginSecure",
			"value": "76561199873908311%7C%7CeyAidHlwIjogIkpXVCIsICJhbGciOiAiRWREU0EiIH0",
			"domain": "steamcommunity.com",
			"path": "/",
			"expires": 1815769392,
			"secure": true,
			"discard": false,
			"rest": { "HttpOnly": null }
		}
	]`

	client, err := NewClient(ClientConfig{Username: "steampyuser"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if err := client.ImportCookiesJSON(steampyJSON); err != nil {
		t.Fatalf("Failed to import steampy JSON: %v", err)
	}

	if client.SessionID != "ab7a216fa67603f775969316" {
		t.Errorf("Expected sessionid 'ab7a216fa67603f775969316', got '%s'", client.SessionID)
	}

	if client.SteamLoginSecure != "76561199873908311||eyAidHlwIjogIkpXVCIsICJhbGciOiAiRWREU0EiIH0" {
		t.Errorf("Expected unescaped steamLoginSecure, got '%s'", client.SteamLoginSecure)
	}

	if !client.LoggedIn {
		t.Errorf("Expected LoggedIn to be true")
	}
}

func TestDomainIsolatedCookies(t *testing.T) {
	multiDomainJSON := `[
		{
			"name": "sessionid",
			"value": "community_session_123",
			"domain": "steamcommunity.com",
			"path": "/"
		},
		{
			"name": "sessionid",
			"value": "store_session_456",
			"domain": "store.steampowered.com",
			"path": "/"
		},
		{
			"name": "browserid",
			"value": "browser_id_789",
			"domain": "steamcommunity.com",
			"path": "/"
		}
	]`

	client, err := NewClient(ClientConfig{Username: "testuser"})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if err := client.ImportCookiesJSON(multiDomainJSON); err != nil {
		t.Fatalf("Failed to import multi-domain cookies: %v", err)
	}

	communitySession := client.GetSessionIDForURL(steamCommunityURL)
	if communitySession != "community_session_123" {
		t.Errorf("Expected community session 'community_session_123', got '%s'", communitySession)
	}

	storeSession := client.GetSessionIDForURL(steamStoreURL)
	if storeSession != "store_session_456" {
		t.Errorf("Expected store session 'store_session_456', got '%s'", storeSession)
	}

	exported, err := client.ExportCookies()
	if err != nil {
		t.Fatalf("Failed to export cookies: %v", err)
	}

	hasBrowserID := false
	for _, ck := range exported {
		if ck.Name == "browserid" && ck.Value == "browser_id_789" {
			hasBrowserID = true
		}
	}
	if !hasBrowserID {
		t.Errorf("Expected browserid cookie to be preserved in export")
	}
}
