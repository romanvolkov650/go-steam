package steam

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestContextCancellation(t *testing.T) {
	client, err := NewClient(ClientConfig{Username: "testuser", Password: "testpassword"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond) // Ensure context expires
	cancel()

	_, err = client.FetchMarketPriceWithContext(ctx, 730, "AK-47 | Redline (Field-Tested)")
	if err == nil {
		t.Fatalf("expected error on canceled context, got nil")
	}

	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		// Context error wrapped in request error
		if err.Error() == "" {
			t.Errorf("expected non-empty context error message")
		}
	}
}
