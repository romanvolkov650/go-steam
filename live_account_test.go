package steam

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type liveTestConfig struct {
	MAFilePath string `json:"ma_file_path"`
	Password   string `json:"password"`
	ProxyURL   string `json:"proxy_url"`
}

func loadLiveTestConfig() (maFilePath, password, proxyURL string) {
	maFilePath = os.Getenv("STEAM_MAFILE_PATH")
	password = os.Getenv("STEAM_PASSWORD")
	proxyURL = os.Getenv("STEAM_PROXY")

	if data, err := os.ReadFile("live_config.json"); err == nil {
		var cfg liveTestConfig
		if jsonErr := json.Unmarshal(data, &cfg); jsonErr == nil {
			if cfg.MAFilePath != "" {
				maFilePath = cfg.MAFilePath
			}
			if cfg.Password != "" {
				password = cfg.Password
			}
			if cfg.ProxyURL != "" {
				proxyURL = cfg.ProxyURL
			}
		}
	}

	if maFilePath == "" {
		matches, err := filepath.Glob("*.maFile")
		if err == nil && len(matches) > 0 {
			maFilePath = matches[0]
		}
	}

	return maFilePath, password, proxyURL
}

func TestLiveAccountSessionAndFeatures(t *testing.T) {
	maFilePath, password, proxyURL := loadLiveTestConfig()
	if maFilePath == "" {
		t.Skip("Skipping live account test: no .maFile or live_config.json found")
	}

	if _, err := os.Stat(maFilePath); os.IsNotExist(err) {
		t.Skipf("Skipping live account test: maFile '%s' not found", maFilePath)
	}

	maFile, err := LoadMAFile(maFilePath)
	if err != nil {
		t.Fatalf("LoadMAFile error for '%s': %v", maFilePath, err)
	}

	cfg := maFile.ToClientConfig(password, proxyURL)

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}

	ctx := context.Background()

	cookiesFile := "cookies_steamuhd.json"
	loaded := false
	if _, statErr := os.Stat(cookiesFile); statErr == nil {
		if loadErr := client.LoadCookiesFromFile(cookiesFile); loadErr == nil {
			if alive, _ := client.IsSessionAliveWithContext(ctx); alive {
				t.Log("Successfully loaded cookies, session is alive. Skipping credentials login.")
				loaded = true
			}
		}
	}

	if !loaded {
		t.Logf("Logging in for %s (SteamID: %s)...", cfg.Username, cfg.SteamID)
		err = client.LoginWithContext(ctx)
		if err != nil {
			t.Fatalf("Login failed for %s: %v", cfg.Username, err)
		}
		t.Logf("Login SUCCESS! LoggedIn=%v, SteamID=%s, SessionID=%s", client.LoggedIn, client.Config.SteamID, client.SessionID)
	}

	// 1. Get Trade Offer URL
	tradeURL, err := client.GetTradeURLWithContext(ctx)
	if err != nil {
		t.Fatalf("GetTradeURLWithContext failed: %v", err)
	}
	t.Logf(">>> Trade Offer URL: %s <<<", tradeURL)

	// 2. Get Account Status (Balance & Inventories)
	status, err := client.GetAccountStatusWithContext(ctx)
	if err != nil {
		t.Fatalf("GetAccountStatusWithContext failed: %v", err)
	}
	t.Logf(">>> Account Status: WalletBalance='%s', PendingBalance='%s', PendingAvailability='%s', CS2Count=%d, Dota2Count=%d, TF2Count=%d <<<",
		status.WalletBalance, status.PendingBalance, status.PendingAvailability, status.CS2Count, status.Dota2Count, status.TF2Count)

	// 3. Get Full Account Details
	details, err := client.FetchAccountDetailsWithContext(ctx)
	if err != nil {
		t.Fatalf("FetchAccountDetailsWithContext failed: %v", err)
	}
	t.Logf(">>> Account Details: TradeURL='%s', AvatarURL='%s' <<<", details.TradeURL, details.AvatarURL)

	// 4. Save cookies to file
	cookiesFile = filepath.Join(os.TempDir(), "live_account_cookies.json")
	if err := client.SaveCookiesToFile(cookiesFile); err != nil {
		t.Fatalf("SaveCookiesToFile failed: %v", err)
	}
	t.Logf("Saved cookies to %s", cookiesFile)

	// 5. Create a BRAND NEW client without username/password/2FA, loading ONLY JSON cookies
	newClient, err := NewClient(ClientConfig{
		SteamID:  cfg.SteamID,
		ProxyURL: proxyURL,
	})
	if err != nil {
		t.Fatalf("NewClient for cookie test failed: %v", err)
	}

	if err := newClient.LoadCookiesFromFile(cookiesFile); err != nil {
		t.Fatalf("LoadCookiesFromFile failed: %v", err)
	}
	t.Logf("Loaded cookies into new client! LoggedIn=%v, SteamID=%s, SessionID=%s",
		newClient.LoggedIn, newClient.Config.SteamID, newClient.SessionID)

	// 6. Test Trade URL using loaded cookies
	loadedTradeURL, err := newClient.GetTradeURLWithContext(ctx)
	if err != nil {
		t.Fatalf("GetTradeURLWithContext with loaded cookies failed: %v", err)
	}
	t.Logf(">>> Loaded Cookies Trade Offer URL: %s <<<", loadedTradeURL)

	if loadedTradeURL != tradeURL {
		t.Errorf("Trade URL mismatch! Original: %s, Loaded: %s", tradeURL, loadedTradeURL)
	}

	// 7. Test Account Status using loaded cookies
	loadedStatus, err := newClient.GetAccountStatusWithContext(ctx)
	if err != nil {
		t.Fatalf("GetAccountStatusWithContext with loaded cookies failed: %v", err)
	}
	t.Logf(">>> Loaded Cookies Account Status: WalletBalance='%s', PendingBalance='%s', PendingAvailability='%s', CS2Count=%d <<<",
		loadedStatus.WalletBalance, loadedStatus.PendingBalance, loadedStatus.PendingAvailability, loadedStatus.CS2Count)

	if loadedStatus.WalletBalance != status.WalletBalance {
		t.Errorf("Wallet balance mismatch! Original: %s, Loaded: %s", status.WalletBalance, loadedStatus.WalletBalance)
	}

	// 8. Test Market Listings and Buy Orders
	listings, err := client.GetMyMarketListingsWithContext(ctx)
	if err != nil {
		t.Logf("GetMyMarketListings warning: %v", err)
	} else {
		t.Logf("Active Market Listings count: %d", len(listings))
	}

	buyOrders, err := client.GetMyBuyOrdersWithContext(ctx)
	if err != nil {
		t.Logf("GetMyBuyOrders warning: %v", err)
	} else {
		t.Logf("Active Buy Orders count: %d", len(buyOrders))
	}

	// 9. Test Mobile Confirmations
	confs, err := client.GetConfirmationsWithContext(ctx)
	if err != nil {
		t.Logf("GetConfirmations warning: %v", err)
	} else {
		t.Logf("Pending 2FA Mobile Confirmations count: %d", len(confs))
	}
}
