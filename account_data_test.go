package steam

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestGetAccountData_Success(t *testing.T) {
	jsonResponse := `{
		"rgWishlist": [730, 570],
		"rgOwnedPackages": [100, 200, 300],
		"rgOwnedApps": [730, 570, 440, 10],
		"rgRecommendedTags": [
			{"tagid": 19, "name": "Action"},
			{"tagid": 1663, "name": "FPS"}
		],
		"rgIgnoredApps": {
			"20": 0,
			"30": 0
		}
	}`

	client, err := NewClient(ClientConfig{
		Username: "testuser",
		SteamID:  "76561198000000001",
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	var capturedURL string
	client.HTTPClient.Transport = mockRoundTripper(func(req *http.Request) (*http.Response, error) {
		capturedURL = req.URL.String()
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(jsonResponse)),
			Request:    req,
		}, nil
	})

	accountData, err := client.GetAccountData()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// Verify query URL contains account ID
	expectedAccountID := SteamID64ToAccountID("76561198000000001")
	if !strings.Contains(capturedURL, "id=39734273") && expectedAccountID > 0 {
		t.Errorf("expected URL to contain account ID %d, got %s", expectedAccountID, capturedURL)
	}

	// Verify OwnedApps
	expectedOwnedApps := []int{730, 570, 440, 10}
	if !reflect.DeepEqual(accountData.OwnedApps, expectedOwnedApps) {
		t.Errorf("expected owned apps %v, got %v", expectedOwnedApps, accountData.OwnedApps)
	}

	// Verify OwnedPackages
	expectedPackages := []int{100, 200, 300}
	if !reflect.DeepEqual(accountData.OwnedPackages, expectedPackages) {
		t.Errorf("expected owned packages %v, got %v", expectedPackages, accountData.OwnedPackages)
	}

	// Verify WishlistedApps
	expectedWishlist := []int{730, 570}
	if !reflect.DeepEqual(accountData.WishlistedApps, expectedWishlist) {
		t.Errorf("expected wishlisted apps %v, got %v", expectedWishlist, accountData.WishlistedApps)
	}

	// Verify Tags
	expectedTags := map[int]string{
		19:   "Action",
		1663: "FPS",
	}
	if !reflect.DeepEqual(accountData.Tags, expectedTags) {
		t.Errorf("expected tags %v, got %v", expectedTags, accountData.Tags)
	}

	// Verify IgnoredApps
	expectedIgnored := map[string]int{
		"20": 0,
		"30": 0,
	}
	if !reflect.DeepEqual(accountData.IgnoredApps, expectedIgnored) {
		t.Errorf("expected ignored apps %v, got %v", expectedIgnored, accountData.IgnoredApps)
	}
}

func TestGetAccountData_EmptyIgnoredApps(t *testing.T) {
	jsonResponse := `{
		"rgWishlist": [],
		"rgOwnedPackages": [],
		"rgOwnedApps": [730],
		"rgRecommendedTags": [],
		"rgIgnoredApps": []
	}`

	client, err := NewClient(ClientConfig{Username: "testuser"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	client.HTTPClient.Transport = mockRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(jsonResponse)),
			Request:    req,
		}, nil
	})

	accountData, err := client.GetAccountData()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if len(accountData.OwnedApps) != 1 || accountData.OwnedApps[0] != 730 {
		t.Errorf("expected owned apps [730], got %v", accountData.OwnedApps)
	}
	if len(accountData.IgnoredApps) != 0 {
		t.Errorf("expected empty ignored apps, got %v", accountData.IgnoredApps)
	}
	if len(accountData.Tags) != 0 {
		t.Errorf("expected empty tags, got %v", accountData.Tags)
	}
}

func TestGetAccountData_MalformedResponse(t *testing.T) {
	testCases := []struct {
		name string
		json string
	}{
		{"empty object", `{}`},
		{"missing owned apps", `{"rgWishlist":[],"rgOwnedPackages":[],"rgRecommendedTags":[],"rgIgnoredApps":{}}`},
		{"null wishlist", `{"rgWishlist":null,"rgOwnedPackages":[],"rgOwnedApps":[],"rgRecommendedTags":[],"rgIgnoredApps":{}}`},
		{"invalid json", `invalid-json`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewClient(ClientConfig{Username: "testuser"})
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			client.HTTPClient.Transport = mockRoundTripper(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(tc.json)),
					Request:    req,
				}, nil
			})

			_, err = client.GetAccountData()
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestGetAccountData_ContextCancellation(t *testing.T) {
	client, err := NewClient(ClientConfig{Username: "testuser"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = client.GetAccountDataWithContext(ctx)
	if err == nil {
		t.Fatal("expected error due to cancelled context, got nil")
	}
}
