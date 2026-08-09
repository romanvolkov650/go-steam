package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"
)

// ClientConfig holds explicit credentials and parameters needed to instantiate a Steam client.
type ClientConfig struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	SharedSecret   string `json:"shared_secret"`
	IdentitySecret string `json:"identity_secret"`
	SteamID        string `json:"steam_id"`
	DeviceID       string `json:"device_id"`
	ProxyURL       string `json:"proxy_url"`
	RefreshToken   string `json:"refresh_token,omitempty"`
	AccessToken    string `json:"access_token,omitempty"`
}

// Client represents a Steam Web session client bound to an account.
type Client struct {
	mu sync.RWMutex

	Config ClientConfig

	HTTPClient *http.Client
	Jar        *cookiejar.Jar

	SessionID        string
	SteamLoginSecure string
	LoggedIn         bool
	WalletCurrency   int
}

// NewClient initializes a Steam client with explicit ClientConfig credentials and optional proxy.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.Username == "" && cfg.SteamID == "" {
		return nil, fmt.Errorf("username or steam_id is required to initialize Steam client")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	transport := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	if cfg.ProxyURL != "" {
		parsedProxy, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL '%s': %w", cfg.ProxyURL, err)
		}
		transport.Proxy = http.ProxyURL(parsedProxy)
	}

	httpClient := &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   30 * time.Second,
	}

	client := &Client{
		Config:     cfg,
		HTTPClient: httpClient,
		Jar:        jar,
	}

	client.SessionID = generateRandomSessionID()
	client.setSessionIDCookie()

	return client, nil
}

func (c *Client) setSessionIDCookie() {
	if c.SessionID == "" {
		return
	}
	ck := &http.Cookie{Name: "sessionid", Value: c.SessionID, Path: "/"}
	for _, u := range allSteamURLs {
		c.Jar.SetCookies(u, []*http.Cookie{ck})
	}
}

// CookieJSON is a serializable representation of an HTTP cookie matching standard steampy / Python http.cookiejar format.
type CookieJSON struct {
	Name     string                 `json:"name"`
	Value    string                 `json:"value"`
	Domain   string                 `json:"domain"`
	Path     string                 `json:"path"`
	Expires  *int64                 `json:"expires"`
	Secure   bool                   `json:"secure"`
	Discard  bool                   `json:"discard"`
	HttpOnly bool                   `json:"http_only,omitempty"`
	Rest     map[string]interface{} `json:"rest,omitempty"`
}

// UnmarshalJSON supports parsing both steampy format (int64/null expires, rest) and legacy go-steam format (ISO string expires).
func (c *CookieJSON) UnmarshalJSON(data []byte) error {
	type Alias CookieJSON
	aux := &struct {
		RawExpires interface{} `json:"expires"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.RawExpires != nil {
		switch v := aux.RawExpires.(type) {
		case float64:
			t := int64(v)
			if t > 0 {
				c.Expires = &t
			}
		case string:
			if t, err := time.Parse(time.RFC3339, v); err == nil && !t.IsZero() {
				sec := t.Unix()
				if sec > 0 {
					c.Expires = &sec
				}
			}
		}
	}

	if c.Rest != nil {
		if _, ok := c.Rest["HttpOnly"]; ok {
			c.HttpOnly = true
		}
	}

	return nil
}

func (c *Client) SetSessionCookies(sessionID, steamLoginSecure, refreshToken string) {
	c.mu.Lock()
	c.SessionID = sessionID
	c.SteamLoginSecure = strings.ReplaceAll(steamLoginSecure, "%7C", "|")
	c.LoggedIn = steamLoginSecure != ""
	steamID := c.Config.SteamID
	c.mu.Unlock()

	secureValue := strings.ReplaceAll(steamLoginSecure, "|", "%7C")
	refreshValue := strings.ReplaceAll(refreshToken, "|", "%7C")

	for _, u := range allSteamURLs {
		if sessionID != "" {
			ck := &http.Cookie{Name: "sessionid", Value: sessionID, Path: "/"}
			c.Jar.SetCookies(u, []*http.Cookie{ck})
		}
		if steamLoginSecure != "" {
			ck := &http.Cookie{Name: "steamLoginSecure", Value: secureValue, Path: "/", Secure: true, HttpOnly: true}
			c.Jar.SetCookies(u, []*http.Cookie{ck})
		}
		if refreshToken != "" {
			refVal := refreshValue
			if !strings.Contains(refVal, "%7C%7C") && !strings.Contains(refVal, "||") && steamID != "" {
				refVal = fmt.Sprintf("%s%%7C%%7C%s", steamID, refreshValue)
			}
			ck := &http.Cookie{Name: "steamRefresh_steam", Value: refVal, Path: "/", Secure: true, HttpOnly: true}
			c.Jar.SetCookies(u, []*http.Cookie{ck})
		}
	}
}

// ExportCookies exports all active session cookies from the CookieJar into steampy compatible JSON format.
func (c *Client) ExportCookies() ([]*CookieJSON, error) {
	seen := make(map[string]bool)
	var result []*CookieJSON

	addCookie := func(ck *http.Cookie, defaultDomain string) {
		domain := ck.Domain
		if domain == "" {
			domain = defaultDomain
		}
		path := ck.Path
		if path == "" {
			path = "/"
		}

		key := fmt.Sprintf("%s:%s:%s", domain, ck.Name, path)
		if seen[key] {
			return
		}
		seen[key] = true

		var exp *int64
		discard := true
		if !ck.Expires.IsZero() && ck.Expires.Unix() > 0 {
			unix := ck.Expires.Unix()
			exp = &unix
			discard = false
		}

		rest := make(map[string]interface{})
		if ck.HttpOnly {
			rest["HttpOnly"] = nil
		}
		if ck.SameSite != 0 {
			switch ck.SameSite {
			case http.SameSiteLaxMode:
				rest["SameSite"] = "Lax"
			case http.SameSiteStrictMode:
				rest["SameSite"] = "Strict"
			case http.SameSiteNoneMode:
				rest["SameSite"] = "None"
			}
		}

		ckJSON := &CookieJSON{
			Name:     ck.Name,
			Value:    ck.Value,
			Domain:   domain,
			Path:     path,
			Expires:  exp,
			Secure:   ck.Secure,
			Discard:  discard,
			HttpOnly: ck.HttpOnly,
		}
		if len(rest) > 0 {
			ckJSON.Rest = rest
		}

		result = append(result, ckJSON)
	}

	for _, u := range allSteamURLs {
		for _, ck := range c.Jar.Cookies(u) {
			addCookie(ck, u.Host)
		}
	}

	return result, nil
}

// ExportCookiesJSON returns the cookies formatted as a JSON string.
func (c *Client) ExportCookiesJSON() (string, error) {
	cookies, err := c.ExportCookies()
	if err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(cookies, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ImportCookies loads cookies from a list of CookieJSON structures into the client's CookieJar.
func (c *Client) ImportCookies(cookies []*CookieJSON) {
	for _, ck := range cookies {
		if ck.Name == "" || ck.Value == "" {
			continue
		}

		if ck.Name == "sessionid" && ck.Value != "" {
			c.mu.Lock()
			c.SessionID = ck.Value
			c.mu.Unlock()
		}
		if ck.Name == "steamLoginSecure" && ck.Value != "" {
			cleanVal := strings.ReplaceAll(ck.Value, "%7C", "|")
			c.mu.Lock()
			c.SteamLoginSecure = cleanVal
			c.LoggedIn = true

			if c.Config.SteamID == "" {
				parts := strings.Split(cleanVal, "|")
				if len(parts) > 0 && len(parts[0]) == 17 {
					c.Config.SteamID = parts[0]
				}
			}
			c.mu.Unlock()
		}

		val := ck.Value
		if ck.Name == "steamLoginSecure" || ck.Name == "steamRefresh_steam" {
			val = strings.ReplaceAll(val, "|", "%7C")
		}

		var expTime time.Time
		if ck.Expires != nil && *ck.Expires > 0 {
			expTime = time.Unix(*ck.Expires, 0)
		}

		isHttpOnly := ck.HttpOnly
		if ck.Rest != nil {
			if _, ok := ck.Rest["HttpOnly"]; ok {
				isHttpOnly = true
			}
		}

		// Force Path: "/" for all session cookies to match all URL paths (dynamic/userwallet, market, account, etc.)
		httpCookie := &http.Cookie{
			Name:     ck.Name,
			Value:    val,
			Path:     "/",
			Expires:  expTime,
			Secure:   ck.Secure,
			HttpOnly: isHttpOnly,
		}

		// Main session cookies (steamLoginSecure, sessionid, steamRefresh_steam) apply to ALL Steam domains
		if ck.Name == "sessionid" || ck.Name == "steamLoginSecure" || ck.Name == "steamRefresh_steam" {
			for _, u := range allSteamURLs {
				c.Jar.SetCookies(u, []*http.Cookie{httpCookie})
			}
		} else {
			domainLower := strings.ToLower(ck.Domain)
			if strings.Contains(domainLower, "steamcommunity.com") {
				c.Jar.SetCookies(steamCommunityURL, []*http.Cookie{httpCookie})
			} else if strings.Contains(domainLower, "steampowered.com") {
				c.Jar.SetCookies(steamStoreURL, []*http.Cookie{httpCookie})
				c.Jar.SetCookies(steamLoginURL, []*http.Cookie{httpCookie})
				c.Jar.SetCookies(steamAPIURL, []*http.Cookie{httpCookie})
				c.Jar.SetCookies(steamCheckoutURL, []*http.Cookie{httpCookie})
				c.Jar.SetCookies(steamHelpURL, []*http.Cookie{httpCookie})
			} else {
				for _, u := range allSteamURLs {
					c.Jar.SetCookies(u, []*http.Cookie{httpCookie})
				}
			}
		}
	}
}

// ImportCookiesJSON loads cookies from a JSON formatted string.
func (c *Client) ImportCookiesJSON(jsonStr string) error {
	var cookies []*CookieJSON
	if err := json.Unmarshal([]byte(jsonStr), &cookies); err != nil {
		return fmt.Errorf("failed to parse cookies JSON: %w", err)
	}
	c.ImportCookies(cookies)
	return nil
}

// SaveCookiesToFile saves all session cookies to a specified JSON file path.
func (c *Client) SaveCookiesToFile(filePath string) error {
	jsonStr, err := c.ExportCookiesJSON()
	if err != nil {
		return fmt.Errorf("failed to export cookies: %w", err)
	}
	if err := os.WriteFile(filePath, []byte(jsonStr), 0644); err != nil {
		return fmt.Errorf("failed to write cookies file '%s': %w", filePath, err)
	}
	return nil
}

// LoadCookiesFromFile loads session cookies directly from a JSON file path.
func (c *Client) LoadCookiesFromFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read cookies file '%s': %w", filePath, err)
	}
	return c.ImportCookiesJSON(string(data))
}

// Generate2FACode generates the current 5-character Steam Guard code using SharedSecret.
func (c *Client) Generate2FACode() (string, error) {
	if c.Config.SharedSecret == "" {
		return "", fmt.Errorf("shared_secret is empty for account %s", c.Config.Username)
	}
	return GenerateTwoFactorCode(c.Config.SharedSecret, 0)
}

// LoginWithRefreshToken authorizes session across Steam domains using stored RefreshToken.
func (c *Client) LoginWithRefreshToken() error {
	return c.LoginWithRefreshTokenWithContext(context.Background())
}

// LoginWithRefreshTokenWithContext authorizes session across Steam domains using stored RefreshToken with context support.
func (c *Client) LoginWithRefreshTokenWithContext(ctx context.Context) error {
	c.mu.RLock()
	refreshToken := c.Config.RefreshToken
	sessionID := c.SessionID
	steamID := c.Config.SteamID
	c.mu.RUnlock()

	if refreshToken == "" {
		return fmt.Errorf("refresh_token is empty")
	}

	finalizeData := url.Values{
		"nonce":     {refreshToken},
		"sessionid": {sessionID},
		"redir":     {"https://steamcommunity.com/login/home/?goto="},
	}

	finReq, err := c.newAjaxPostRequestWithContext(ctx, "https://login.steampowered.com/jwt/finalizelogin", finalizeData, "https://steamcommunity.com/")
	if err != nil {
		return err
	}

	finResp, err := c.doRequestWithRetry(ctx, finReq)
	if err != nil {
		return fmt.Errorf("finalizelogin request failed: %w", err)
	}
	defer finResp.Body.Close()

	finBody, readErr := io.ReadAll(finResp.Body)
	if readErr != nil {
		return fmt.Errorf("failed to read finalizelogin response: %w", readErr)
	}

	var finResult struct {
		SteamID      string `json:"steamID"`
		TransferInfo []struct {
			URL    string                 `json:"url"`
			Params map[string]interface{} `json:"params"`
		} `json:"transfer_info"`
	}

	if err := json.Unmarshal(finBody, &finResult); err == nil && len(finResult.TransferInfo) > 0 {
		if finResult.SteamID != "" {
			c.mu.Lock()
			c.Config.SteamID = finResult.SteamID
			steamID = finResult.SteamID
			c.mu.Unlock()
		}

		// Perform /login/settoken transfer for each domain (store, community, help, checkout) with retries
		for _, info := range finResult.TransferInfo {
			if info.URL == "" {
				continue
			}
			tData := url.Values{}
			for k, v := range info.Params {
				tData.Set(k, fmt.Sprintf("%v", v))
			}
			tData.Set("steamID", steamID)

			for attempt := 0; attempt < 3; attempt++ {
				tReq, err := c.newAjaxPostRequestWithContext(ctx, info.URL, tData, "https://steamcommunity.com/")
				if err != nil {
					continue
				}
				tResp, err := c.doRequestWithRetry(ctx, tReq)
				if err == nil {
					tResp.Body.Close()
					break
				}
				time.Sleep(300 * time.Millisecond)
			}
		}

		// Ensure essential session cookies (steamLoginSecure, sessionid) exist for steamcommunity.com
		c.ensureSessionCookiesForURL(steamCommunityURL)

		c.mu.Lock()
		c.LoggedIn = true
		c.mu.Unlock()
		return nil
	}

	return fmt.Errorf("failed to finalize login with refresh token: %s", string(finBody))
}

// Login executes modern Steam Web Auth API or uses RefreshToken from maFile.
func (c *Client) Login() error {
	return c.LoginWithContext(context.Background())
}

// LoginWithContext executes modern Steam Web Auth API with context support.
func (c *Client) LoginWithContext(ctx context.Context) error {
	c.mu.RLock()
	refreshToken := c.Config.RefreshToken
	username := c.Config.Username
	password := c.Config.Password
	sharedSecret := c.Config.SharedSecret
	c.mu.RUnlock()

	if refreshToken != "" {
		if err := c.LoginWithRefreshTokenWithContext(ctx); err == nil && c.LoggedIn {
			return nil
		}
		c.mu.Lock()
		c.Config.RefreshToken = ""
		c.mu.Unlock()
	}

	if username == "" || password == "" {
		return fmt.Errorf("username and password are required for login")
	}

	var clientID, requestID, steamID string
	var lastBeginBody []byte
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				if lastErr != nil {
					return fmt.Errorf("%w (auth detail: %v)", ctx.Err(), lastErr)
				}
				return ctx.Err()
			case <-time.After(1 * time.Second):
			}
		}

		mod, exp, ts, err := c.fetchRSAPublicKeyWithContext(ctx, username)
		if err != nil {
			lastErr = fmt.Errorf("fetch RSA key error: %w", err)
			continue
		}

		clientID, requestID, steamID, lastBeginBody, err = c.beginAuthSessionWithContext(ctx, username, password, mod, exp, ts)
		if err == nil && clientID != "" {
			break
		}
		if err != nil {
			lastErr = fmt.Errorf("begin auth session error: %w", err)
		} else if len(lastBeginBody) > 0 {
			lastErr = fmt.Errorf("begin auth session failed: %s", string(lastBeginBody))
		}
	}

	if clientID == "" {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("begin auth session failed: %s", string(lastBeginBody))
	}

	c.mu.Lock()
	c.Config.SteamID = steamID
	c.mu.Unlock()

	if sharedSecret == "" {
		return fmt.Errorf("shared_secret is required for 2FA authentication")
	}

	twoFactorCode, err := c.Generate2FACode()
	if err != nil {
		return fmt.Errorf("failed to generate 2FA code: %w", err)
	}

	if err := c.updateAuthSession2FAWithContext(ctx, clientID, steamID, twoFactorCode); err != nil {
		return err
	}

	rToken, aToken, err := c.pollAuthSessionStatusWithContext(ctx, clientID, requestID)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.Config.RefreshToken = rToken
	c.Config.AccessToken = aToken
	sessionID := c.SessionID
	c.mu.Unlock()

	if err := c.LoginWithRefreshTokenWithContext(ctx); err != nil {
		jwtSteamLoginSecure := fmt.Sprintf("%s||%s", steamID, aToken)
		c.SetSessionCookies(sessionID, jwtSteamLoginSecure, rToken)
	}

	return nil
}

func (c *Client) fetchRSAPublicKeyWithContext(ctx context.Context, username string) (mod, exp, timestamp string, err error) {
	rsaReqURL := fmt.Sprintf("https://api.steampowered.com/IAuthenticationService/GetPasswordRSAPublicKey/v1/?account_name=%s", url.QueryEscape(username))
	req, err := c.newRequestWithContext(ctx, "GET", rsaReqURL, nil, "https://steamcommunity.com/")
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Origin", "https://steamcommunity.com")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", err
	}

	var rsaResult struct {
		Response struct {
			PublicKeyMod string `json:"publickey_mod"`
			PublicKeyExp string `json:"publickey_exp"`
			Timestamp    string `json:"timestamp"`
		} `json:"response"`
	}

	if err := json.Unmarshal(bodyBytes, &rsaResult); err != nil || rsaResult.Response.PublicKeyMod == "" {
		return "", "", "", fmt.Errorf("invalid RSA key response")
	}

	return rsaResult.Response.PublicKeyMod, rsaResult.Response.PublicKeyExp, rsaResult.Response.Timestamp, nil
}

func (c *Client) beginAuthSessionWithContext(ctx context.Context, username, password, modHex, expHex, timestamp string) (clientID, requestID, steamID string, rawBody []byte, err error) {
	encryptedPassword, err := encryptPassword(password, modHex, expHex)
	if err != nil {
		return "", "", "", nil, err
	}

	beginData := url.Values{
		"device_friendly_name": {"steampy"},
		"account_name":         {username},
		"encrypted_password":   {encryptedPassword},
		"encryption_timestamp": {timestamp},
		"remember_login":       {"true"},
		"persistence":          {"1"},
		"website_id":           {"Community"},
	}

	beginReq, err := c.newAjaxPostRequestWithContext(ctx, "https://api.steampowered.com/IAuthenticationService/BeginAuthSessionViaCredentials/v1/", beginData, "https://steamcommunity.com/")
	if err != nil {
		return "", "", "", nil, err
	}

	beginResp, err := c.doRequestWithRetry(ctx, beginReq)
	if err != nil {
		return "", "", "", nil, err
	}
	defer beginResp.Body.Close()

	rawBody, readErr := io.ReadAll(beginResp.Body)
	if readErr != nil {
		return "", "", "", nil, readErr
	}

	var beginResult struct {
		Response struct {
			ClientID  string `json:"client_id"`
			RequestID string `json:"request_id"`
			SteamID   string `json:"steamid"`
		} `json:"response"`
	}

	if err := json.Unmarshal(rawBody, &beginResult); err != nil || beginResult.Response.ClientID == "" {
		return "", "", "", rawBody, fmt.Errorf("failed to begin auth session")
	}

	return beginResult.Response.ClientID, beginResult.Response.RequestID, beginResult.Response.SteamID, rawBody, nil
}

func (c *Client) updateAuthSession2FAWithContext(ctx context.Context, clientID, steamID, twoFactorCode string) error {
	updateData := url.Values{
		"client_id": {clientID},
		"steamid":   {steamID},
		"code":      {twoFactorCode},
		"code_type": {"3"},
	}

	updateReq, err := c.newAjaxPostRequestWithContext(ctx, "https://api.steampowered.com/IAuthenticationService/UpdateAuthSessionWithSteamGuardCode/v1/", updateData, "https://steamcommunity.com/")
	if err != nil {
		return err
	}

	updateResp, err := c.doRequestWithRetry(ctx, updateReq)
	if err != nil {
		return fmt.Errorf("update auth session failed: %w", err)
	}
	defer updateResp.Body.Close()

	return nil
}

func (c *Client) pollAuthSessionStatusWithContext(ctx context.Context, clientID, requestID string) (refreshToken, accessToken string, err error) {
	pollData := url.Values{
		"client_id":  {clientID},
		"request_id": {requestID},
	}

	var pollResult struct {
		Response struct {
			RefreshToken string `json:"refresh_token"`
			AccessToken  string `json:"access_token"`
		} `json:"response"`
	}

	var lastPollErr error
	var lastPollBody string

	for attempt := 0; attempt < 5; attempt++ {
		select {
		case <-ctx.Done():
			if lastPollErr != nil {
				return "", "", fmt.Errorf("%w (poll error: %v)", ctx.Err(), lastPollErr)
			}
			return "", "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}

		pollReq, err := c.newAjaxPostRequestWithContext(ctx, "https://api.steampowered.com/IAuthenticationService/PollAuthSessionStatus/v1/", pollData, "https://steamcommunity.com/")
		if err != nil {
			lastPollErr = err
			continue
		}

		pollResp, err := c.doRequestWithRetry(ctx, pollReq)
		if err != nil {
			lastPollErr = err
			continue
		}

		pollBody, readErr := io.ReadAll(pollResp.Body)
		pollResp.Body.Close()

		if readErr != nil {
			lastPollErr = readErr
			continue
		}

		lastPollBody = string(pollBody)
		if json.Unmarshal(pollBody, &pollResult) == nil && pollResult.Response.RefreshToken != "" {
			return pollResult.Response.RefreshToken, pollResult.Response.AccessToken, nil
		}
		if len(lastPollBody) > 0 {
			lastPollErr = fmt.Errorf("poll auth status response: %s", lastPollBody)
		}
	}

	if lastPollErr != nil {
		return "", "", fmt.Errorf("poll auth status failed: %w", lastPollErr)
	}

	return "", "", fmt.Errorf("poll auth status timed out / failed")
}
