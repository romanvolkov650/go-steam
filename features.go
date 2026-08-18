package steam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reBalDirect       = regexp.MustCompile(`(?i)"wallet_balance"\s*:\s*"?(\d+)"?`)
	reCurrDirect      = regexp.MustCompile(`(?i)"wallet_currency"\s*:\s*(\d+)`)
	reTag             = regexp.MustCompile(`(?i)(?:id="header_wallet_balance"|class="[^"]*account_balance[^"]*")[^>]*>([\s\S]*?)</`)
	reHeaderWalletA   = regexp.MustCompile(`(?i)id="header_wallet_balance"[^>]*>([\s\S]*?)</a>`)
	reSpanPending     = regexp.MustCompile(`(?i)<span[^>]*data-tooltip-html="([^"]+)"[^>]*>([\s\S]*?)</span>`)
	rePendingAvail    = regexp.MustCompile(`(?i)\((available in [^)]+)\)`)
	rePendingAvailAlt = regexp.MustCompile(`(?i)\(([^)]+)\)`)
	reSplitBrSpan     = regexp.MustCompile(`(?i)<br\s*/?>|<span`)
	reStripHTML       = regexp.MustCompile(`<[^>]*>`)
	reTradeURL        = regexp.MustCompile(`id="trade_offer_access_url"[^>]*value="([^"]+)"`)
	reTradeURLAlt     = regexp.MustCompile(`https://steamcommunity\.com/tradeoffer/new/\?partner=\d+(?:&amp;|&)token=[a-zA-Z0-9_-]+`)
	reTradeURLValue   = regexp.MustCompile(`https:\\?/\\?/steamcommunity\.com\\?/tradeoffer\\?/new\\?/\?partner=\d+[^"'\s<]+`)
	reAvatarInner     = regexp.MustCompile(`(?s)class=["'][^"']*(?:playerAvatarAutoSizeInner|playerAvatarHeading)[^"']*["'][^>]*>.*?<(?:img|source)[^>]+(?:src|srcset)=["'](https://[^"'\s>]+)["']`)
	reAvatarImg       = regexp.MustCompile(`(?s)class=["'][^"']*playerAvatarAutoSizeInner[^"']*["'][^>]*>.*?<img[^>]+src=["'](https://[^"'\s>]+)["']`)
	reConfID          = regexp.MustCompile(`data-confid="(\d+)"`)
	reConfKey         = regexp.MustCompile(`data-key="(\d+)"`)
	reConfCreator     = regexp.MustCompile(`data-creator="(\d+)"`)
	rePersonaName     = regexp.MustCompile(`(?i)<span\s+class="actual_persona_name">([\s\S]*?)</span>`)
	reProfileSummary  = regexp.MustCompile(`(?s)<div\s+class="profile_summary(?:_body)?">\s*([\s\S]*?)\s*</div>`)
)

// UserProfile represents the public Steam profile details.
type UserProfile struct {
	Nickname    string `json:"nickname"`
	AvatarURL   string `json:"avatar_url"`
	Description string `json:"description"`
}

// AccountStatus holds fetched Steam account data (balance, inventories, bans).
type AccountStatus struct {
	SteamID             string `json:"steam_id"`
	WalletBalance       string `json:"wallet_balance"`                 // e.g. "5 968,85₴" or "$15.20"
	PendingBalance      string `json:"pending_balance,omitempty"`      // e.g. "440,52₴"
	PendingAvailability string `json:"pending_availability,omitempty"` // e.g. "available in 1-2 days"
	CS2Count            int    `json:"cs2_count"`                      // AppID 730
	Dota2Count          int    `json:"dota2_count"`                    // AppID 570
	TF2Count            int    `json:"tf2_count"`                      // AppID 440
	IsVACBanned         bool   `json:"is_vac_banned"`
	IsTradeBanned       bool   `json:"is_trade_banned"`
	IsLimited           bool   `json:"is_limited"` // $5 limit
	LastUpdated         int64  `json:"last_updated"`
	ErrorMessage        string `json:"error_message,omitempty"`
}

// FullAccountDetails holds aggregated balance, avatar, and trade URL data for an account.
type FullAccountDetails struct {
	SteamID       string `json:"steam_id"`
	WalletBalance string `json:"wallet_balance"`
	TradeURL      string `json:"trade_url"`
	AvatarURL     string `json:"avatar_url"`
	LastUpdated   int64  `json:"last_updated"`
}

// AccountData represents user store ownership and preferences from dynamicstore/userdata.
type AccountData struct {
	OwnedApps      []int          `json:"owned_apps"`
	OwnedPackages  []int          `json:"owned_packages"`
	WishlistedApps []int          `json:"wishlisted_apps"`
	IgnoredApps    map[string]int `json:"ignored_apps"`
	Tags           map[int]string `json:"tags"`
}

// HasCS2Prime reports whether the account owns the Counter-Strike 2 Prime Status Upgrade package (SubID 54029).
func (ad *AccountData) HasCS2Prime() bool {
	if ad == nil {
		return false
	}
	return ad.HasPackage(54029)
}

// HasApp reports whether the account owns the specified Steam AppID.
func (ad *AccountData) HasApp(appID int) bool {
	if ad == nil {
		return false
	}
	for _, id := range ad.OwnedApps {
		if id == appID {
			return true
		}
	}
	return false
}

// HasPackage reports whether the account owns the specified Steam PackageID (SubID).
func (ad *AccountData) HasPackage(pkgID int) bool {
	if ad == nil {
		return false
	}
	for _, id := range ad.OwnedPackages {
		if id == pkgID {
			return true
		}
	}
	return false
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

	// Fetch Wallet Balance Details (Available balance, Pending balance, Pending availability)
	bal, pendingBal, avail, err := c.fetchWalletBalanceWithContext(ctx)
	if err == nil {
		status.WalletBalance = bal
		status.PendingBalance = pendingBal
		status.PendingAvailability = avail
	} else {
		status.WalletBalance = "0.00"
	}

	// Fetch Inventory Counts for CS2 (730), Dota 2 (570), TF2 (440)
	status.CS2Count = c.fetchInventoryItemCountWithContext(ctx, "730", "2")
	status.Dota2Count = c.fetchInventoryItemCountWithContext(ctx, "570", "2")
	status.TF2Count = c.fetchInventoryItemCountWithContext(ctx, "440", "2")

	return status, nil
}



func extractWalletBalanceDetailsFromHTML(bodyStr string) (balance, pendingBalance, pendingAvailability string) {
	if m := reHeaderWalletA.FindStringSubmatch(bodyStr); len(m) > 1 {
		inner := m[1]

		if spanMatch := reSpanPending.FindStringSubmatch(inner); len(spanMatch) > 2 {
			tooltipAttr := html.UnescapeString(spanMatch[1])
			spanText := strings.TrimSpace(reStripHTML.ReplaceAllString(spanMatch[2], ""))

			pendingBalance = spanText
			if strings.HasPrefix(strings.ToLower(pendingBalance), "pending:") {
				pendingBalance = strings.TrimSpace(pendingBalance[8:])
			}

			if availMatch := rePendingAvail.FindStringSubmatch(tooltipAttr); len(availMatch) > 1 {
				pendingAvailability = availMatch[1]
			} else if availMatch := rePendingAvailAlt.FindStringSubmatch(tooltipAttr); len(availMatch) > 1 {
				pendingAvailability = availMatch[1]
			}
		}

		parts := reSplitBrSpan.Split(inner, 2)
		balance = strings.TrimSpace(reStripHTML.ReplaceAllString(parts[0], ""))
		if balance != "" {
			return balance, pendingBalance, pendingAvailability
		}
	}

	// Fallback 1: Direct Regex for "wallet_balance": 1456 / "wallet_balance": "1456"
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
				balance = fmt.Sprintf("%s %s", amountStr, sym)
			} else {
				balance = amountStr
			}
			return balance, pendingBalance, pendingAvailability
		}
	}

	// Fallback 2: General tag regex for account_balance
	if m := reTag.FindStringSubmatch(bodyStr); len(m) > 1 {
		cleanText := strings.TrimSpace(reStripHTML.ReplaceAllString(m[1], ""))
		if cleanText != "" {
			balance = cleanText
			return balance, pendingBalance, pendingAvailability
		}
	}

	return "", "", ""
}

func extractBalanceFromHTML(bodyStr string) string {
	bal, _, _ := extractWalletBalanceDetailsFromHTML(bodyStr)
	return bal
}

// GetWalletBalance fetches wallet balance details (balance, pendingBalance, pendingAvailability).
func (c *Client) GetWalletBalance() (balance, pendingBalance, pendingAvailability string, err error) {
	return c.GetWalletBalanceWithContext(context.Background())
}

// GetWalletBalanceWithContext fetches wallet balance details with context support.
func (c *Client) GetWalletBalanceWithContext(ctx context.Context) (balance, pendingBalance, pendingAvailability string, err error) {
	return c.fetchWalletBalanceWithContext(ctx)
}

func (c *Client) fetchWalletBalanceWithContext(ctx context.Context) (balance, pendingBalance, pendingAvailability string, err error) {
	// Query ONLY store.steampowered.com/account/
	reqAccount, err := c.newRequestWithContext(ctx, "GET", "https://store.steampowered.com/account/", nil, "https://store.steampowered.com/")
	if err != nil {
		return "0.00", "", "", err
	}

	reqAccount.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	bodyBytes, _, err := c.doRequestAndRead(ctx, reqAccount)
	if err != nil {
		return "0.00", "", "", err
	}

	bal, pendingBal, avail := extractWalletBalanceDetailsFromHTML(string(bodyBytes))
	if bal == "" {
		return "0.00", "", "", ErrSessionExpired
	}
	return bal, pendingBal, avail, nil
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

	bodyBytes, resp, err := c.doRequestAndRead(ctx, req)
	if err != nil || resp.StatusCode != http.StatusOK {
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

	deviceID := c.getDeviceID()

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

	bodyBytes, _, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return nil, err
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

	blocks := strings.Split(bodyStr, "mobileconf_list_entry")
	if len(blocks) <= 1 {
		blocks = []string{bodyStr}
	}

	for _, block := range blocks {
		confIDMatch := reConfID.FindStringSubmatch(block)
		keyMatch := reConfKey.FindStringSubmatch(block)
		creatorMatch := reConfCreator.FindStringSubmatch(block)

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

	deviceID := c.getDeviceID()

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

	bodyBytes, _, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return err
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
	bodyBytes, _, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return "", err
	}

	bodyStr := string(bodyBytes)
	matches := reTradeURL.FindStringSubmatch(bodyStr)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]), nil
	}

	if m := reTradeURLAlt.FindString(bodyStr); m != "" {
		return strings.ReplaceAll(m, "&amp;", "&"), nil
	}

	// Additional pattern check for inputs or JS variables
	if m := reTradeURLValue.FindString(bodyStr); m != "" {
		clean := strings.ReplaceAll(m, `\/`, "/")
		clean = strings.ReplaceAll(clean, "&amp;", "&")
		return clean, nil
	}

	return "", fmt.Errorf("trade offer URL not found")
}

// CheckTradeLimit checks if the trade URL shows any trade limit message for the current session.
func (c *Client) CheckTradeLimit(tradeURL string) (bool, string, error) {
	return c.CheckTradeLimitWithContext(context.Background(), tradeURL)
}

// CheckTradeLimitWithContext checks if the trade URL shows any trade limit message for the current session with context support.
func (c *Client) CheckTradeLimitWithContext(ctx context.Context, tradeURL string) (bool, string, error) {
	req, err := c.newRequestWithContext(ctx, "GET", tradeURL, nil, "https://steamcommunity.com/")
	if err != nil {
		return false, "", err
	}
	bodyBytes, _, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return false, "", err
	}

	bodyStr := string(bodyBytes)
	lowerBody := strings.ToLower(bodyStr)

	// Check for trade limit indicators
	if strings.Contains(lowerBody, "forgot and then reset") ||
		strings.Contains(lowerBody, "unable to trade") ||
		strings.Contains(lowerBody, "protect the items in your inventory") ||
		strings.Contains(lowerBody, "сбросили пароль") ||
		strings.Contains(lowerBody, "не сможете обмениваться") {

		var reason string
		reErrorMsg := regexp.MustCompile(`(?i)<div[^>]*id="error_msg"[^>]*>\s*([^<]+)\s*</div>`)
		matches := reErrorMsg.FindStringSubmatch(bodyStr)
		if len(matches) > 1 {
			reason = html.UnescapeString(strings.TrimSpace(matches[1]))
		} else {
			reErrorBox := regexp.MustCompile(`(?i)<div[^>]*class="[^"]*error_box[^"]*"[^>]*>\s*([^<]+)\s*</div>`)
			boxMatches := reErrorBox.FindStringSubmatch(bodyStr)
			if len(boxMatches) > 1 {
				reason = html.UnescapeString(strings.TrimSpace(boxMatches[1]))
			} else {
				if strings.Contains(lowerBody, "forgot and then reset") {
					reason = "You recently forgot and then reset your Steam account's password. In order to protect the items in your inventory, you will be unable to trade."
				} else if strings.Contains(lowerBody, "сбросили пароль") {
					reason = "Вы недавно забыли и сбросили пароль от аккаунта Steam. В целях защиты предметов обмен временно недоступен."
				} else {
					reason = "Trade is limited/restricted on this account"
				}
			}
		}
		reason = strings.Join(strings.Fields(reason), " ")
		return true, reason, nil
	}

	return false, "", nil
}

// GetUserProfile fetches the public Steam profile details.
func (c *Client) GetUserProfile() (*UserProfile, error) {
	return c.GetUserProfileWithContext(context.Background())
}

// GetUserProfileWithContext fetches the public Steam profile details with context support.
// If the profile has not been set up, it returns ErrProfileNotConfigured.
func (c *Client) GetUserProfileWithContext(ctx context.Context) (*UserProfile, error) {
	c.mu.RLock()
	steamID := c.Config.SteamID
	c.mu.RUnlock()

	profileURL := "https://steamcommunity.com/my/profile"
	if steamID != "" {
		profileURL = fmt.Sprintf("https://steamcommunity.com/profiles/%s", steamID)
	}

	req, err := c.newRequestWithContext(ctx, "GET", profileURL, nil, "https://steamcommunity.com/")
	if err != nil {
		return nil, err
	}
	bodyBytes, resp, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return nil, err
	}

	bodyStr := string(bodyBytes)

	// Check if redirected to home or contains edit welcomed link indicating unconfigured profile
	if (resp != nil && resp.Request != nil && strings.HasSuffix(resp.Request.URL.Path, "/home")) ||
		strings.Contains(bodyStr, "edit?welcomed=1") ||
		strings.Contains(bodyStr, "welcomed=1") {
		return nil, ErrProfileNotConfigured
	}

	profile := &UserProfile{}

	// Parse Nickname
	if matches := rePersonaName.FindStringSubmatch(bodyStr); len(matches) > 1 {
		profile.Nickname = html.UnescapeString(strings.TrimSpace(matches[1]))
	}

	// Parse Avatar URL
	if matches := reAvatarInner.FindStringSubmatch(bodyStr); len(matches) > 1 {
		profile.AvatarURL = matches[1]
	} else if matches := reAvatarImg.FindStringSubmatch(bodyStr); len(matches) > 1 {
		profile.AvatarURL = matches[1]
	}

	// Parse Description
	if matches := reProfileSummary.FindStringSubmatch(bodyStr); len(matches) > 1 {
		desc := reStripHTML.ReplaceAllString(matches[1], "")
		profile.Description = html.UnescapeString(strings.TrimSpace(desc))
	}

	if profile.Nickname == "" && profile.AvatarURL == "" {
		return nil, fmt.Errorf("failed to parse profile data for %s", c.Config.Username)
	}

	return profile, nil
}

// GetAvatarURL fetches the account's profile avatar image URL.
func (c *Client) GetAvatarURL() (string, error) {
	return c.GetAvatarURLWithContext(context.Background())
}

// GetAvatarURLWithContext fetches the account's profile avatar image URL directly with context support.
func (c *Client) GetAvatarURLWithContext(ctx context.Context) (string, error) {
	profile, err := c.GetUserProfileWithContext(ctx)
	if err != nil {
		if errors.Is(err, ErrProfileNotConfigured) {
			return "https://avatars.fastly.steamstatic.com/fef49e7fa7e1997310d705b2a6158ff8dc1cdfeb_full.jpg", nil
		}
		return "", err
	}
	if profile.AvatarURL == "" {
		return "https://avatars.fastly.steamstatic.com/fef49e7fa7e1997310d705b2a6158ff8dc1cdfeb_full.jpg", nil
	}
	return profile.AvatarURL, nil
}

// FetchAccountDetails aggregates wallet balance, trade URL, and avatar URL in a single call.
func (c *Client) FetchAccountDetails() (*FullAccountDetails, error) {
	return c.FetchAccountDetailsWithContext(context.Background())
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

// GetAccountData fetches the account's store data (owned games/apps, packages, wishlist, ignored apps, tags).
func (c *Client) GetAccountData() (*AccountData, error) {
	return c.GetAccountDataWithContext(context.Background())
}

// GetAccountDataWithContext fetches the account's store data with context support.
func (c *Client) GetAccountDataWithContext(ctx context.Context) (*AccountData, error) {
	c.mu.RLock()
	steamID := c.Config.SteamID
	c.mu.RUnlock()

	reqURL := "https://store.steampowered.com/dynamicstore/userdata/"
	if steamID != "" {
		accountID := SteamID64ToAccountID(steamID)
		if accountID > 0 {
			reqURL = fmt.Sprintf("https://store.steampowered.com/dynamicstore/userdata/?id=%d", accountID)
		} else {
			reqURL = fmt.Sprintf("https://store.steampowered.com/dynamicstore/userdata/?id=%s", steamID)
		}
	}

	req, err := c.newRequestWithContext(ctx, "GET", reqURL, nil, "https://store.steampowered.com/")
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	bodyBytes, _, err := c.doRequestAndRead(ctx, req)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Wishlist        *[]int `json:"rgWishlist"`
		OwnedPackages   *[]int `json:"rgOwnedPackages"`
		OwnedApps       *[]int `json:"rgOwnedApps"`
		RecommendedTags *[]struct {
			TagID int    `json:"tagid"`
			Name  string `json:"name"`
		} `json:"rgRecommendedTags"`
		IgnoredApps json.RawMessage `json:"rgIgnoredApps"`
	}

	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse userdata response: %w", err)
	}

	if raw.Wishlist == nil || raw.OwnedPackages == nil || raw.OwnedApps == nil || raw.RecommendedTags == nil || len(raw.IgnoredApps) == 0 || string(raw.IgnoredApps) == "null" {
		return nil, fmt.Errorf("malformed dynamicstore response")
	}

	tags := make(map[int]string, len(*raw.RecommendedTags))
	for _, tag := range *raw.RecommendedTags {
		tags[tag.TagID] = tag.Name
	}

	ignoredApps := make(map[string]int)
	trimmedIgnored := strings.TrimSpace(string(raw.IgnoredApps))
	if trimmedIgnored != "" && trimmedIgnored != "[]" && trimmedIgnored != "{}" && trimmedIgnored != "null" {
		_ = json.Unmarshal(raw.IgnoredApps, &ignoredApps)
	}

	return &AccountData{
		OwnedApps:      *raw.OwnedApps,
		OwnedPackages:  *raw.OwnedPackages,
		WishlistedApps: *raw.Wishlist,
		IgnoredApps:    ignoredApps,
		Tags:           tags,
	}, nil
}
