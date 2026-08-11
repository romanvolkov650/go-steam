package steam

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
)

type mockRoundTripper func(req *http.Request) (*http.Response, error)

func (m mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m(req)
}

func TestGetUserProfile_Configured(t *testing.T) {
	htmlSnippet := `
<html>
<head>
	<span class="actual_persona_name">Roman Volkov</span>
</head>
<body>
	<div class="playerAvatarAutoSizeInner">
		<img src="https://avatars.akamai.steamstatic.com/test_avatar.jpg">
	</div>
	<div class="profile_summary">
		This is my profile summary info.
	</div>
</body>
</html>`

	client, err := NewClient(ClientConfig{Username: "testuser"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	client.HTTPClient.Transport = mockRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(htmlSnippet)),
			Request:    req,
		}, nil
	})

	profile, err := client.GetUserProfile()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if profile.Nickname != "Roman Volkov" {
		t.Errorf("expected nickname 'Roman Volkov', got '%s'", profile.Nickname)
	}
	if profile.AvatarURL != "https://avatars.akamai.steamstatic.com/test_avatar.jpg" {
		t.Errorf("expected avatar URL 'https://avatars.akamai.steamstatic.com/test_avatar.jpg', got '%s'", profile.AvatarURL)
	}
	if profile.Description != "This is my profile summary info." {
		t.Errorf("expected description 'This is my profile summary info.', got '%s'", profile.Description)
	}
}

func TestGetUserProfile_Unconfigured(t *testing.T) {
	htmlSnippet := `
<html>
<head>
	<title>Steam Community :: Welcome</title>
</head>
<body>
	<a href="https://steamcommunity.com/profiles/76561199868126417/edit?welcomed=1">Set Up Profile</a>
</body>
</html>`

	client, err := NewClient(ClientConfig{Username: "testuser"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	client.HTTPClient.Transport = mockRoundTripper(func(req *http.Request) (*http.Response, error) {
		// Mock redirect target
		req.URL.Path = "/profiles/76561199868126417/home"
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewBufferString(htmlSnippet)),
			Request:    req,
		}, nil
	})

	_, err = client.GetUserProfile()
	if !errors.Is(err, ErrProfileNotConfigured) {
		t.Fatalf("expected ErrProfileNotConfigured error, got %v", err)
	}
}
