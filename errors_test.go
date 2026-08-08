package steam

import (
	"errors"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	errRateLimit := &SteamAPIError{StatusCode: 429, Message: "Too Many Requests"}
	if !errors.Is(errRateLimit, ErrRateLimited) {
		t.Errorf("expected errRateLimit to match ErrRateLimited")
	}

	errUnauthorized := &SteamAPIError{StatusCode: 401, Message: "Unauthorized"}
	if !errors.Is(errUnauthorized, ErrSessionExpired) {
		t.Errorf("expected errUnauthorized to match ErrSessionExpired")
	}

	if errors.Is(errRateLimit, ErrSessionExpired) {
		t.Errorf("errRateLimit should not match ErrSessionExpired")
	}
}
