package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AccountStatus holds fetched Steam account data (balance, inventories, bans).
type AccountStatus struct {
	SteamID        string `json:"steam_id"`
	WalletBalance  string `json:"wallet_balance"` // e.g. "1250.50 RUB" or "$15.20"
	CS2Count       int    `json:"cs2_count"`      // AppID 730
	Dota2Count     int    `json:"dota2_count"`    // AppID 570
	TF2Count       int    `json:"tf2_count"`      // AppID 440
	IsVACBanned    bool   `json:"is_vac_banned"`
	IsTradeBanned  bool   `json:"is_trade_banned"`
	IsLimited      bool   `json:"is_limited"` // $5 limit
	LastUpdated    int64  `json:"last_updated"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

// Confirmation represents a pending 2FA Mobile Confirmation (trade / market listing).
type Confirmation struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Title     string `json:"title"`
	Headline  string `json:"headline"`
	Summary   string `json:"summary"`
	CreatorID string `json:"creator_id"`
	Icon      string `json:"icon,omitempty"`
}

// GetAccountStatus fetches wallet balance and inventory item counts for CS2, Dota 2, TF2.
func (c *Client) GetAccountStatus() (*AccountStatus, error) {
	return c.GetAccountStatusWithContext(context.Background())
}

// GetAccountStatusWithContext fetches wallet balance and inventory item counts for CS2, Dota 2, TF2 with context support.
func (c *Client) GetAccountStatusWithContext(ctx context.Context) (*AccountStatus, error) {
	c.mu.RLock()
	steamID := c.Config.SteamID
	c.mu.RUnlock()

	status := &AccountStatus{
		SteamID:     steamID,
		LastUpdated: time.Now().Unix(),
	}

	// 1. Fetch Wallet Balance
	balance, err := c.fetchWalletBalanceWithContext(ctx)
	if err == nil && balance != "" {
		status.WalletBalance = balance
	} else {
		status.WalletBalance = "0.00"
	}

	// 2. Fetch inventory counts for CS2 (730), Dota2 (570), TF2 (440)
	if steamID != "" {
		status.CS2Count = c.fetchInventoryItemCountWithContext(ctx, "730", "2")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}

		status.Dota2Count = c.fetchInventoryItemCountWithContext(ctx, "570", "2")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}

		status.TF2Count = c.fetchInventoryItemCountWithContext(ctx, "440", "2")
	}

	return status, nil
}

func (c *Client) fetchWalletBalance() (string, error) {
	return c.fetchWalletBalanceWithContext(context.Background())
}

func (c *Client) fetchWalletBalanceWithContext(ctx context.Context) (string, error) {
	// 1. Query steamcommunity.com/market/ to extract g_rgWalletInfo (contains wallet_currency and balance)
	reqMarket, err := c.newRequestWithContext(ctx, "GET", "https://steamcommunity.com/market/", nil, "https://steamcommunity.com/")
	if err == nil {
		reqMarket.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		reqMarket.Header.Set("Accept-Language", "en-US,en;q=0.9")

		respMarket, err := c.doRequestWithRetry(ctx, reqMarket)
		if err == nil {
			bodyBytes, _ := io.ReadAll(respMarket.Body)
			respMarket.Body.Close()
			bodyStr := string(bodyBytes)

			// Search for g_rgWalletInfo = {...};
			reWallet := regexp.MustCompile(`(?s)g_rgWalletInfo\s*=\s*(\{.*?\});`)
			matches := reWallet.FindStringSubmatch(bodyStr)
			if len(matches) > 1 {
				var walletInfo struct {
					WalletBalance  interface{} `json:"wallet_balance"`
					WalletCurrency int         `json:"wallet_currency"`
				}
				if err := json.Unmarshal([]byte(matches[1]), &walletInfo); err == nil {
					if walletInfo.WalletCurrency > 0 {
						c.mu.Lock()
						c.WalletCurrency = walletInfo.WalletCurrency
						c.mu.Unlock()
					}
					if walletInfo.WalletBalance != nil {
						var rawAmount float64
						switch v := walletInfo.WalletBalance.(type) {
						case float64:
							rawAmount = v
						case string:
							rawAmount, _ = strconv.ParseFloat(v, 64)
						}
						return fmt.Sprintf("%.2f", rawAmount/100.0), nil
					}
				}
			}

			// Header balance fallback on market page
			reHeader := regexp.MustCompile(`id="header_wallet_balance"[^>]*>\s*([^<]+)\s*`)
			if m := reHeader.FindStringSubmatch(bodyStr); len(m) > 1 {
				val := strings.TrimSpace(m[1])
				if val != "" {
					return val, nil
				}
			}
		}
	}

	// 2. Fallback store.steampowered.com
	reqStore, err := c.newRequestWithContext(ctx, "GET", "https://store.steampowered.com/", nil, "https://steamcommunity.com/")
	if err != nil {
		return "", err
	}
	reqStore.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	reqStore.Header.Set("Accept-Language", "en-US,en;q=0.9")

	respStore, err := c.doRequestWithRetry(ctx, reqStore)
	if err != nil {
		return "", err
	}
	defer respStore.Body.Close()

	bodyBytes, err := io.ReadAll(respStore.Body)
	if err != nil {
		return "", err
	}

	bodyStr := string(bodyBytes)

	reHeader := regexp.MustCompile(`id="header_wallet_balance"[^>]*>\s*([^<]+)\s*`)
	if m := reHeader.FindStringSubmatch(bodyStr); len(m) > 1 {
		return strings.TrimSpace(m[1]), nil
	}

	return "0.00", nil
}

func (c *Client) fetchInventoryItemCount(appID, contextID string) int {
	return c.fetchInventoryItemCountWithContext(context.Background(), appID, contextID)
}

func (c *Client) fetchInventoryItemCountWithContext(ctx context.Context, appID, contextID string) int {
	c.mu.RLock()
	steamID := c.Config.SteamID
	c.mu.RUnlock()

	reqURL := fmt.Sprintf("https://steamcommunity.com/inventory/%s/%s/%s?l=english&count=1000&preserve_bbcode=1&raw_asset_properties=1", steamID, appID, contextID)
	req, err := c.newRequestWithContext(ctx, "GET", reqURL, nil, fmt.Sprintf("https://steamcommunity.com/profiles/%s/inventory", steamID))
	if err != nil {
		return 0
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}

	var inventoryResp struct {
		TotalInventoryCount int           `json:"total_inventory_count"`
		Assets              []interface{} `json:"assets"`
	}

	if err := json.Unmarshal(bodyBytes, &inventoryResp); err == nil {
		if inventoryResp.TotalInventoryCount > 0 {
			return inventoryResp.TotalInventoryCount
		}
		return len(inventoryResp.Assets)
	}

	return 0
}

// GetConfirmations fetches pending 2FA mobile confirmations for trades or market sales.
func (c *Client) GetConfirmations() ([]*Confirmation, error) {
	return c.GetConfirmationsWithContext(context.Background())
}

// GetConfirmationsWithContext fetches pending 2FA mobile confirmations for trades or market sales with context support.
func (c *Client) GetConfirmationsWithContext(ctx context.Context) ([]*Confirmation, error) {
	c.mu.RLock()
	username := c.Config.Username
	identitySecret := c.Config.IdentitySecret
	deviceID := c.Config.DeviceID
	steamID := c.Config.SteamID
	c.mu.RUnlock()

	if identitySecret == "" {
		return nil, fmt.Errorf("identity_secret missing for account %s", username)
	}

	t := time.Now().Unix()
	hash, err := GenerateConfirmationHash(identitySecret, "conf", t)
	if err != nil {
		return nil, err
	}

	if deviceID == "" {
		deviceID = "android:00000000-0000-0000-0000-000000000000"
	}

	params := url.Values{
		"p":   {deviceID},
		"a":   {steamID},
		"k":   {hash},
		"t":   {strconv.FormatInt(t, 10)},
		"m":   {"android"},
		"tag": {"conf"},
	}

	confURL := "https://steamcommunity.com/mobileconf/getlist?" + params.Encode()
	req, err := c.newRequestWithContext(ctx, "GET", confURL, nil, "")
	if err != nil {
		return nil, err
	}

	mobileCookies := []*http.Cookie{
		{Name: "mobileClientVersion", Value: "0 (2.0.20)", Domain: "steamcommunity.com", Path: "/"},
		{Name: "mobileClient", Value: "android", Domain: "steamcommunity.com", Path: "/"},
	}
	c.Jar.SetCookies(steamCommunityURL, mobileCookies)

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read confirmations response: %w", err)
	}

	var jsonResp struct {
		Success       bool `json:"success"`
		Confirmations []struct {
			ID        string      `json:"id"`
			Nonce     string      `json:"nonce"`
			CreatorID interface{} `json:"creator_id"`
			Headline  string      `json:"headline"`
			Summary   []string    `json:"summary"`
		} `json:"conf"`
	}

	if err := json.Unmarshal(bodyBytes, &jsonResp); err == nil && jsonResp.Success {
		var confirmations []*Confirmation
		for _, item := range jsonResp.Confirmations {
			creatorStr := ""
			switch v := item.CreatorID.(type) {
			case string:
				creatorStr = v
			case float64:
				creatorStr = strconv.FormatUint(uint64(v), 10)
			}
			headline := item.Headline
			if headline == "" && len(item.Summary) > 0 {
				headline = item.Summary[0]
			}

			confirmations = append(confirmations, &Confirmation{
				ID:        item.ID,
				Key:       item.Nonce,
				CreatorID: creatorStr,
				Title:     headline,
			})
		}
		return confirmations, nil
	}

	// Fallback to HTML parsing if getlist is not JSON
	bodyStr := string(bodyBytes)
	confirmations := []*Confirmation{}

	reConfID := regexp.MustCompile(`data-confid="(\d+)"`)
	reKey := regexp.MustCompile(`data-key="(\d+)"`)
	reCreator := regexp.MustCompile(`data-creator="(\d+)"`)

	blocks := strings.Split(bodyStr, "mobileconf_list_entry")
	if len(blocks) <= 1 {
		blocks = []string{bodyStr}
	}

	for _, block := range blocks {
		confIDMatch := reConfID.FindStringSubmatch(block)
		keyMatch := reKey.FindStringSubmatch(block)
		creatorMatch := reCreator.FindStringSubmatch(block)

		if len(confIDMatch) > 1 && len(keyMatch) > 1 {
			creatorID := ""
			if len(creatorMatch) > 1 {
				creatorID = creatorMatch[1]
			}
			confirmations = append(confirmations, &Confirmation{
				ID:        confIDMatch[1],
				Key:       keyMatch[1],
				CreatorID: creatorID,
				Title:     "Trade Confirmation #" + confIDMatch[1],
			})
		}
	}

	return confirmations, nil
}

// SendConfirmationAction accepts ("allow") or rejects ("cancel") a pending mobile confirmation.
func (c *Client) SendConfirmationAction(conf *Confirmation, action string) error {
	return c.SendConfirmationActionWithContext(context.Background(), conf, action)
}

// SendConfirmationActionWithContext accepts ("allow") or rejects ("cancel") a pending mobile confirmation with context support.
func (c *Client) SendConfirmationActionWithContext(ctx context.Context, conf *Confirmation, action string) error {
	c.mu.RLock()
	username := c.Config.Username
	identitySecret := c.Config.IdentitySecret
	deviceID := c.Config.DeviceID
	steamID := c.Config.SteamID
	c.mu.RUnlock()

	if identitySecret == "" {
		return fmt.Errorf("identity_secret missing for account %s", username)
	}

	t := time.Now().Unix()
	hash, err := GenerateConfirmationHash(identitySecret, action, t)
	if err != nil {
		return err
	}

	if deviceID == "" {
		deviceID = "android:00000000-0000-0000-0000-000000000000"
	}

	params := url.Values{
		"op":  {action},
		"p":   {deviceID},
		"a":   {steamID},
		"k":   {hash},
		"t":   {strconv.FormatInt(t, 10)},
		"m":   {"android"},
		"tag": {action},
		"cid": {conf.ID},
		"ck":  {conf.Key},
	}

	actionURL := "https://steamcommunity.com/mobileconf/ajaxop?" + params.Encode()
	req, err := c.newRequestWithContext(ctx, "GET", actionURL, nil, "")
	if err != nil {
		return err
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read confirmation action response: %w", err)
	}
	var actionResp struct {
		Success bool `json:"success"`
	}

	if err := json.Unmarshal(bodyBytes, &actionResp); err == nil && actionResp.Success {
		return nil
	}

	return fmt.Errorf("confirmation action '%s' failed: %s", action, string(bodyBytes))
}
