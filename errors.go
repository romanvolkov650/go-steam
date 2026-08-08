package steam

import (
	"errors"
	"fmt"
)

var (
	// ErrNotLoggedIn is returned when an operation requires an active logged-in session.
	ErrNotLoggedIn = errors.New("steam: client not logged in")

	// ErrConfirmationRequired is returned when a trade offer or market listing requires 2FA mobile confirmation.
	ErrConfirmationRequired = errors.New("steam: 2FA mobile confirmation required")

	// ErrRateLimited is returned when Steam API / Market returns HTTP 429 Too Many Requests.
	ErrRateLimited = errors.New("steam: rate limit exceeded (HTTP 429)")

	// ErrItemNotFound is returned when a requested item cannot be found in inventory.
	ErrItemNotFound = errors.New("steam: item not found in inventory")

	// ErrBuyOrderAlreadyExists is returned when attempting to place a buy order on an item that already has an active buy order.
	ErrBuyOrderAlreadyExists = errors.New("steam: active buy order already exists for this item")

	// ErrSessionExpired is returned when session cookies are invalid or expired.
	ErrSessionExpired = errors.New("steam: session expired or invalid cookies")
)

// SteamAPIError represents a structured error returned by Steam HTTP endpoints.
type SteamAPIError struct {
	StatusCode int
	Message    string
}

func (e *SteamAPIError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("steam API error (HTTP %d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("steam API error: %s", e.Message)
}

func (e *SteamAPIError) Is(target error) bool {
	if target == ErrRateLimited && e.StatusCode == 429 {
		return true
	}
	if target == ErrSessionExpired && (e.StatusCode == 401 || e.StatusCode == 403) {
		return true
	}
	return false
}
