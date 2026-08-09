package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// FullAccountDetails holds aggregated balance, avatar, and trade URL data for an account.
type FullAccountDetails struct {
	SteamID       string `json:"steam_id"`
	WalletBalance string `json:"wallet_balance"`
	TradeURL      string `json:"trade_url"`
	AvatarURL     string `json:"avatar_url"`
	LastUpdated   int64  `json:"last_updated"`
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

// GetAccountStatusWithContext fetches wallet balance with context support.
func (c *Client) GetAccountStatusWithContext(ctx context.Context) (*AccountStatus, error) {
	c.mu.RLock()
	steamID := c.Config.SteamID
	c.mu.RUnlock()

	status := &AccountStatus{
		SteamID:     steamID,
		LastUpdated: time.Now().Unix(),
	}

	// Fetch Wallet Balance
	balance, err := c.fetchWalletBalanceWithContext(ctx)
	if err == nil && balance != "" {
		status.WalletBalance = balance
	} else {
		status.WalletBalance = "0.00"
	}

	return status, nil
}

func (c *Client) fetchWalletBalance() (string, error) {
	return c.fetchWalletBalanceWithContext(context.Background())
}

func getCurrencySymbol(currencyID int) string {
	switch currencyID {
	case 1:
		return "$"
	case 2:
		return "£"
	case 3:
		return "€"
	case 5:
		return "₽"
	case 18:
		return "₴"
	case 37:
		return "₸"
	case 23:
		return "¥"
	case 17:
		return "TL"
	case 34:
		return "ARS$"
	case 7:
		return "R$"
	case 8:
		return "¥"
	case 20:
		return "CDN$"
	case 21:
		return "A$"
	default:
		return ""
	}
}

func extractBalanceFromHTML(bodyStr string) string {
	// 1. Direct Regex for "wallet_balance": 1456 / "wallet_balance": "1456"
	reBalDirect := regexp.MustCompile(`(?i)"wallet_balance"\s*:\s*"?(\d+)"?`)
	reCurrDirect := regexp.MustCompile(`(?i)"wallet_currency"\s*:\s*(\d+)`)

	if mBal := reBalDirect.FindStringSubmatch(bodyStr); len(mBal) > 1 {
		rawAmount, err := strconv.ParseFloat(mBal[1], 64)
		if err == nil {
			amountStr := fmt.Sprintf("%.2f", rawAmount/100.0)
			currencyID := 0
			if mCurr := reCurrDirect.FindStringSubmatch(bodyStr); len(mCurr) > 1 {
				currencyID, _ = strconv.Atoi(mCurr[1])
			}
			sym := getCurrencySymbol(currencyID)
			if sym != "" {
				return fmt.Sprintf("%s %s", amountStr, sym)
			}
			return amountStr
		}
	}

	// 2. HTML Tag Regex for header_wallet_balance or account_balance
	reTag := regexp.MustCompile(`(?i)(?:id="header_wallet_balance"|class="[^"]*account_balance[^"]*")[^>]*>([\s\S]*?)</`)
	if m := reTag.FindStringSubmatch(bodyStr); len(m) > 1 {
		stripHTML := regexp.MustCompile(`<[^>]*>`)
		cleanText := strings.TrimSpace(stripHTML.ReplaceAllString(m[1], ""))
		if cleanText != "" {
			return cleanText
		}
	}

	return ""
}

func (c *Client) fetchWalletBalanceWithContext(ctx context.Context) (string, error) {
	log.Printf("[BalanceFetch] [%s] Starting wallet balance fetch...", c.Config.Username)

	// 1. Query store.steampowered.com/account/
	reqAccount, err := c.newRequestWithContext(ctx, "GET", "https://store.steampowered.com/account/", nil, "https://store.steampowered.com/")
	if err == nil {
		reqAccount.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		respAccount, err := c.doRequestWithRetry(ctx, reqAccount)
		if err == nil {
			bodyBytes, _ := io.ReadAll(respAccount.Body)
			respAccount.Body.Close()
			bodyStr := string(bodyBytes)

			if idx := strings.Index(bodyStr, "header_wallet_balance"); idx != -1 {
				start := idx - 50
				if start < 0 {
					start = 0
				}
				end := idx + 150
				if end > len(bodyStr) {
					end = len(bodyStr)
				}
				log.Printf("[BalanceFetch] [%s] header_wallet_balance context in store/account: %s", c.Config.Username, bodyStr[start:end])
			}
			if idx := strings.Index(bodyStr, "account_balance"); idx != -1 {
				start := idx - 50
				if start < 0 {
					start = 0
				}
				end := idx + 150
				if end > len(bodyStr) {
					end = len(bodyStr)
				}
				log.Printf("[BalanceFetch] [%s] account_balance context in store/account: %s", c.Config.Username, bodyStr[start:end])
			}

			if val := extractBalanceFromHTML(bodyStr); val != "" {
				log.Printf("[BalanceFetch] [%s] Found balance in store/account page: %s", c.Config.Username, val)
				return val, nil
			}
		} else {
			log.Printf("[BalanceFetch] [%s] store/account HTTP error: %v", c.Config.Username, err)
		}
	}

	// 2. Query steamcommunity.com/market/
	reqMarket, err := c.newRequestWithContext(ctx, "GET", "https://steamcommunity.com/market/", nil, "https://steamcommunity.com/")
	if err == nil {
		reqMarket.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		reqMarket.Header.Set("Accept-Language", "en-US,en;q=0.9")

		respMarket, err := c.doRequestWithRetry(ctx, reqMarket)
		if err == nil {
			bodyBytes, _ := io.ReadAll(respMarket.Body)
			respMarket.Body.Close()
			bodyStr := string(bodyBytes)

			if idx := strings.Index(bodyStr, "header_wallet_balance"); idx != -1 {
				start := idx - 50
				if start < 0 {
					start = 0
				}
				end := idx + 150
				if end > len(bodyStr) {
					end = len(bodyStr)
				}
				log.Printf("[BalanceFetch] [%s] header_wallet_balance context in market: %s", c.Config.Username, bodyStr[start:end])
			}
			if idx := strings.Index(bodyStr, "g_rgWalletInfo"); idx != -1 {
				start := idx - 50
				if start < 0 {
					start = 0
				}
				end := idx + 200
				if end > len(bodyStr) {
					end = len(bodyStr)
				}
				log.Printf("[BalanceFetch] [%s] g_rgWalletInfo context in market: %s", c.Config.Username, bodyStr[start:end])
			}

			if val := extractBalanceFromHTML(bodyStr); val != "" {
				log.Printf("[BalanceFetch] [%s] Found balance in market page: %s", c.Config.Username, val)
				return val, nil
			}
		} else {
			log.Printf("[BalanceFetch] [%s] Market page HTTP error: %v", c.Config.Username, err)
		}
	}

	log.Printf("[BalanceFetch] [%s] Balance not found across all endpoints, returning 0.00", c.Config.Username)
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

// GetTradeURL fetches the account's own Steam Trade Offer URL.
func (c *Client) GetTradeURL() (string, error) {
	return c.GetTradeURLWithContext(context.Background())
}

// GetTradeURLWithContext fetches the account's own Steam Trade Offer URL with context support.
func (c *Client) GetTradeURLWithContext(ctx context.Context) (string, error) {
	c.mu.RLock()
	steamID := c.Config.SteamID
	c.mu.RUnlock()

	reqURL := "https://steamcommunity.com/my/tradeoffers/privacy"
	if steamID != "" {
		reqURL = fmt.Sprintf("https://steamcommunity.com/profiles/%s/tradeoffers/privacy", steamID)
	}

	req, err := c.newRequestWithContext(ctx, "GET", reqURL, nil, "https://steamcommunity.com/")
	if err != nil {
		return "", err
	}
	c.ensureSessionCookiesForURL(req.URL)
	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	bodyStr := string(bodyBytes)
	reTradeURL := regexp.MustCompile(`id="trade_offer_access_url"[^>]*value="([^"]+)"`)
	matches := reTradeURL.FindStringSubmatch(bodyStr)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]), nil
	}

	reTradeURLAlt := regexp.MustCompile(`https://steamcommunity\.com/tradeoffer/new/\?partner=\d+(?:&amp;|&)token=[a-zA-Z0-9_-]+`)
	if m := reTradeURLAlt.FindString(bodyStr); m != "" {
		return strings.ReplaceAll(m, "&amp;", "&"), nil
	}

	// Additional pattern check for inputs or JS variables
	reTradeURLValue := regexp.MustCompile(`https:\\?/\\?/steamcommunity\.com\\?/tradeoffer\\?/new\\?/\?partner=\d+[^"'\s<]+`)
	if m := reTradeURLValue.FindString(bodyStr); m != "" {
		clean := strings.ReplaceAll(m, `\/`, "/")
		clean = strings.ReplaceAll(clean, "&amp;", "&")
		return clean, nil
	}

	if idx := strings.Index(bodyStr, "trade_offer_access_url"); idx != -1 {
		start := idx - 50
		if start < 0 { start = 0 }
		end := idx + 200
		if end > len(bodyStr) { end = len(bodyStr) }
		log.Printf("[GetTradeURL] [%s] Found trade_offer_access_url snippet: %s", c.Config.Username, bodyStr[start:end])
	} else {
		sample := bodyStr
		if len(sample) > 500 {
			sample = sample[:500]
		}
		log.Printf("[GetTradeURL] [%s] trade_offer_access_url not found. Page head: %s", c.Config.Username, sample)
	}

	return "", fmt.Errorf("trade offer URL not found")
}

// GetAvatarURL fetches the account's profile avatar image URL.
func (c *Client) GetAvatarURL() (string, error) {
	return c.GetAvatarURLWithContext(context.Background())
}

// GetAvatarURLWithContext fetches the account's profile avatar image URL directly from playerAvatarAutoSizeInner HTML header block with context support.
func (c *Client) GetAvatarURLWithContext(ctx context.Context) (string, error) {
	c.mu.RLock()
	steamID := c.Config.SteamID
	c.mu.RUnlock()

	profileURL := "https://steamcommunity.com/my/profile"
	if steamID != "" {
		profileURL = fmt.Sprintf("https://steamcommunity.com/profiles/%s", steamID)
	}

	req, err := c.newRequestWithContext(ctx, "GET", profileURL, nil, "https://steamcommunity.com/")
	if err != nil {
		return "", err
	}
	c.ensureSessionCookiesForURL(req.URL)
	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	bodyStr := string(bodyBytes)

	// Target profile avatar block (playerAvatarAutoSizeInner / playerAvatarHeading)
	reAvatarInner := regexp.MustCompile(`(?s)class=["'][^"']*(?:playerAvatarAutoSizeInner|playerAvatarHeading)[^"']*["'][^>]*>.*?<(?:img|source)[^>]+(?:src|srcset)=["'](https://[^"'\s>]+)["']`)
	if matches := reAvatarInner.FindStringSubmatch(bodyStr); len(matches) > 1 {
		log.Printf("[GetAvatarURL] [%s] Extracted avatar from playerAvatarAutoSizeInner: %s", c.Config.Username, matches[1])
		return matches[1], nil
	}

	// Secondary check inside playerAvatarAutoSizeInner block for any img src
	reInnerImg := regexp.MustCompile(`(?s)class=["'][^"']*playerAvatarAutoSizeInner[^"']*["'][^>]*>.*?<img[^>]+src=["'](https://[^"'\s>]+)["']`)
	if matches := reInnerImg.FindStringSubmatch(bodyStr); len(matches) > 1 {
		log.Printf("[GetAvatarURL] [%s] Extracted avatar img from playerAvatarAutoSizeInner: %s", c.Config.Username, matches[1])
		return matches[1], nil
	}

	return "", fmt.Errorf("avatar URL not found for %s in profile header", c.Config.Username)
}

// FetchAccountDetailsWithContext aggregates wallet balance, trade URL, and avatar URL in a single context-aware call.
func (c *Client) FetchAccountDetailsWithContext(ctx context.Context) (*FullAccountDetails, error) {
	status, err := c.GetAccountStatusWithContext(ctx)
	if err != nil {
		return nil, err
	}

	details := &FullAccountDetails{
		SteamID:       status.SteamID,
		WalletBalance: status.WalletBalance,
		LastUpdated:   time.Now().Unix(),
	}

	if tradeURL, err := c.GetTradeURLWithContext(ctx); err == nil {
		details.TradeURL = tradeURL
	}

	if avatarURL, err := c.GetAvatarURLWithContext(ctx); err == nil {
		details.AvatarURL = avatarURL
	}

	return details, nil
}

