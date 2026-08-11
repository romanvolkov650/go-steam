package steam

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imroc/req/v3"
)

const (
	defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"
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
	Config     ClientConfig
	HTTPClient *http.Client
	Jar        *TrackingCookieJar

	mu               sync.RWMutex
	LoggedIn         bool
	SessionID        string
	SteamLoginSecure string
	WalletCurrency   int
}

// NewClient initializes a Steam client with explicit ClientConfig credentials and optional proxy.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.Username == "" && cfg.SteamID == "" {
		return nil, fmt.Errorf("username or steam_id is required to initialize Steam client")
	}

	jar, err := NewTrackingCookieJar()
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	reqClient := req.C().ImpersonateChrome()
	if cfg.ProxyURL != "" {
		reqClient.SetProxyURL(cfg.ProxyURL)
	}

	httpClient := &http.Client{
		Transport: reqClient.GetTransport(),
		Jar:       jar,
		Timeout:   30 * time.Second,
	}

	client := &Client{
		Config:     cfg,
		HTTPClient: httpClient,
		Jar:        jar,
	}

	return client, nil
}

// GetSessionIDForURL returns the sessionid cookie for a specific Steam domain URL from the CookieJar,
// falling back to c.SessionID if no cookie is present.
func (c *Client) GetSessionIDForURL(u *url.URL) string {
	if u == nil {
		u = steamCommunityURL
	}
	if c.Jar != nil {
		for _, ck := range c.Jar.Cookies(u) {
			if ck.Name == "sessionid" && ck.Value != "" {
				return ck.Value
			}
		}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.SessionID
}

// GetSessionID returns the sessionid cookie for steamcommunity.com from the CookieJar.
func (c *Client) GetSessionID() string {
	return c.GetSessionIDForURL(steamCommunityURL)
}

func (c *Client) setSessionIDCookie() {
	c.mu.RLock()
	sessionID := c.SessionID
	c.mu.RUnlock()
	if sessionID == "" {
		return
	}
	ck := &http.Cookie{Name: "sessionid", Value: sessionID, Path: "/"}
	c.Jar.SetCookies(steamCommunityURL, []*http.Cookie{ck})
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
	if c.Jar != nil {
		c.Jar.ClearAuthCookies()
	}

	c.mu.Lock()
	if sessionID != "" {
		c.SessionID = sessionID
	}
	c.SteamLoginSecure = strings.ReplaceAll(steamLoginSecure, "%7C", "|")
	c.LoggedIn = steamLoginSecure != ""
	steamID := c.Config.SteamID
	c.mu.Unlock()

	secureValue := strings.ReplaceAll(steamLoginSecure, "|", "%7C")
	refreshValue := strings.ReplaceAll(refreshToken, "|", "%7C")

	// Only set auth cookies on the 2 primary domains (like steampy's STEAM_COOKIE_DOMAINS)
	authDomains := []*url.URL{steamCommunityURL, steamStoreURL}

	for _, u := range authDomains {
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

// syncSessionCookies synchronizes auth cookies between steamcommunity.com and store.steampowered.com.
// Replicates steampy's set_sessionid_cookies() logic: if a cookie exists in one domain but not the other, copy it.
func (c *Client) syncSessionCookies() {
	if c.Jar == nil {
		return
	}

	authNames := []string{"steamLoginSecure", "sessionid", "steamRefresh_steam", "steamCountry"}

	communityAll := c.Jar.Cookies(steamCommunityURL)
	storeAll := c.Jar.Cookies(steamStoreURL)

	communityMap := make(map[string]string)
	for _, ck := range communityAll {
		communityMap[ck.Name] = ck.Value
	}
	storeMap := make(map[string]string)
	for _, ck := range storeAll {
		storeMap[ck.Name] = ck.Value
	}

	for _, name := range authNames {
		cVal := communityMap[name]
		sVal := storeMap[name]

		if cVal == "" && sVal == "" {
			continue
		}

		// Determine canonical value: prefer whichever domain has it
		canonical := cVal
		if canonical == "" {
			canonical = sVal
		}

		isSecure := name == "steamLoginSecure" || name == "steamRefresh_steam"
		isHTTPOnly := name == "steamLoginSecure" || name == "steamRefresh_steam"

		if cVal == "" {
			c.Jar.SetCookies(steamCommunityURL, []*http.Cookie{{
				Name: name, Value: canonical, Path: "/", Secure: isSecure, HttpOnly: isHTTPOnly,
			}})
		}
		if sVal == "" {
			c.Jar.SetCookies(steamStoreURL, []*http.Cookie{{
				Name: name, Value: canonical, Path: "/", Secure: isSecure, HttpOnly: isHTTPOnly,
			}})
		}
	}
}

// ExportCookies exports all active session cookies from the CookieJar into steampy compatible JSON format.
func (c *Client) ExportCookies() ([]*CookieJSON, error) {
	var result []*CookieJSON
	seen := make(map[string]bool)

	for _, ck := range c.Jar.GetAllCookies() {
		// Domain might start with a dot, strip it for grouping/identification if needed
		// but preserve original for export.
		domain := ck.Domain
		path := ck.Path
		if path == "" {
			path = "/"
		}

		key := fmt.Sprintf("%s:%s:%s", domain, ck.Name, path)
		if seen[key] {
			continue
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
	if c.Jar != nil {
		c.Jar.ClearAuthCookies()
	}

	for _, ck := range cookies {
		if ck.Name == "" || ck.Value == "" {
			continue
		}

		if ck.Name == "sessionid" && ck.Value != "" {
			c.mu.Lock()
			if strings.Contains(strings.ToLower(ck.Domain), "steamcommunity.com") || c.SessionID == "" {
				c.SessionID = ck.Value
			}
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
		if ck.Name == "steamRefresh_steam" && ck.Value != "" {
			cleanVal := strings.ReplaceAll(ck.Value, "%7C", "|")
			c.mu.Lock()
			c.Config.RefreshToken = cleanVal
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

		domainLower := strings.ToLower(ck.Domain)
		if strings.Contains(domainLower, "steamcommunity.com") {
			c.Jar.SetCookies(steamCommunityURL, []*http.Cookie{httpCookie})
		} else if strings.Contains(domainLower, "store.steampowered.com") {
			c.Jar.SetCookies(steamStoreURL, []*http.Cookie{httpCookie})
		} else if strings.Contains(domainLower, "login.steampowered.com") {
			c.Jar.SetCookies(steamLoginURL, []*http.Cookie{httpCookie})
		} else if strings.Contains(domainLower, "api.steampowered.com") {
			c.Jar.SetCookies(steamAPIURL, []*http.Cookie{httpCookie})
		} else if strings.Contains(domainLower, "checkout.steampowered.com") {
			c.Jar.SetCookies(steamCheckoutURL, []*http.Cookie{httpCookie})
		} else if strings.Contains(domainLower, "help.steampowered.com") {
			c.Jar.SetCookies(steamHelpURL, []*http.Cookie{httpCookie})
		} else if strings.Contains(domainLower, "steampowered.com") {
			c.Jar.SetCookies(steamStoreURL, []*http.Cookie{httpCookie})
			c.Jar.SetCookies(steamLoginURL, []*http.Cookie{httpCookie})
			c.Jar.SetCookies(steamAPIURL, []*http.Cookie{httpCookie})
			c.Jar.SetCookies(steamCheckoutURL, []*http.Cookie{httpCookie})
			c.Jar.SetCookies(steamHelpURL, []*http.Cookie{httpCookie})
		} else {
			if ck.Name == "steamLoginSecure" || ck.Name == "steamRefresh_steam" {
				for _, u := range allSteamURLs {
					c.Jar.SetCookies(u, []*http.Cookie{httpCookie})
				}
			} else {
				c.Jar.SetCookies(steamCommunityURL, []*http.Cookie{httpCookie})
			}
		}
	}

	// Replicate steampy's behavior of setting steamRememberLogin to true for auth domains
	rememberCookie := &http.Cookie{
		Name:   "steamRememberLogin",
		Value:  "true",
		Path:   "/",
		Secure: true,
	}
	c.Jar.SetCookies(steamCommunityURL, []*http.Cookie{rememberCookie})
	c.Jar.SetCookies(steamStoreURL, []*http.Cookie{rememberCookie})
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
		return fmt.Errorf("failed to finalize login with refresh token: %w", err)
	}

	finBody, readErr := readResponseBody(finResp)
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

		// Perform /login/settoken transfer for each domain — single attempt each (like steampy)
		for _, info := range finResult.TransferInfo {
			if info.URL == "" {
				continue
			}
			tData := url.Values{}
			for k, v := range info.Params {
				tData.Set(k, fmt.Sprintf("%v", v))
			}
			tData.Set("steamID", steamID)

			tReq, err := c.newAjaxPostRequestWithContext(ctx, info.URL, tData, "https://steamcommunity.com/")
			if err != nil {
				continue
			}
			tResp, err := c.doRequestWithRetry(ctx, tReq)
			if err == nil {
				tResp.Body.Close()
			}
		}

		// Synchronize auth cookies between community and store domains (like steampy's set_sessionid_cookies)
		c.syncSessionCookies()

		c.mu.Lock()
		c.LoggedIn = true
		c.mu.Unlock()
		return nil
	}

	return fmt.Errorf("failed to finalize login with refresh token: %s", string(finBody))
}

// InitSession performs a lightweight initialization by visiting key Steam domains
// to organically fetch initial cookies like sessionid, browserid, and steamCountry
// directly from Steam servers via Set-Cookie, exactly as a real browser does.
func (c *Client) InitSession(ctx context.Context) error {
	domains := []string{
		"https://steamcommunity.com/",
		"https://store.steampowered.com/",
		"https://help.steampowered.com/en/",
		"https://steam.tv/",
	}

	for _, d := range domains {
		req, err := c.newRequestWithContext(ctx, "GET", d, nil, "")
		if err != nil {
			continue
		}
		resp, err := c.HTTPClient.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}
	return nil
}

// Login executes modern Steam Web Auth API or uses RefreshToken from maFile.
func (c *Client) Login() error {
	return c.LoginWithContext(context.Background())
}

// LoginWithContext executes modern Steam Web Auth API with context support.
// Login flow is modeled after steampy (artemiyDev/steampy) to avoid creating
// multiple sessions that trigger Steam account restrictions.
func (c *Client) LoginWithContext(ctx context.Context) error {
	c.mu.RLock()
	existingRefreshToken := c.Config.RefreshToken
	username := c.Config.Username
	password := c.Config.Password
	sharedSecret := c.Config.SharedSecret
	alreadyLoggedIn := c.LoggedIn
	c.mu.RUnlock()

	// If already logged in, check if session is still alive before re-creating (like steampy)
	if alreadyLoggedIn {
		if alive, _ := c.IsSessionAliveWithContext(ctx); alive {
			return nil
		}
	}

	// If we already have a refresh token (e.g. from maFile), try refreshing first
	if existingRefreshToken != "" {
		if err := c.LoginWithRefreshTokenWithContext(ctx); err == nil {
			return nil
		}
		// If refresh token expired or failed, clear it and proceed to credentials auth
		c.mu.Lock()
		c.Config.RefreshToken = ""
		c.mu.Unlock()
	}

	if username == "" || password == "" {
		return fmt.Errorf("username and password are required for login")
	}
	if sharedSecret == "" {
		return fmt.Errorf("shared_secret is required for 2FA authentication")
	}

	// Clear auth cookies before credentials login (like steampy's _clear_auth_cookies)
	if c.Jar != nil {
		c.Jar.ClearAuthCookies()
	}

	// Set steamRememberLogin before credentials login (like steampy)
	rememberCk := &http.Cookie{Name: "steamRememberLogin", Value: "true", Path: "/"}
	c.Jar.SetCookies(steamCommunityURL, []*http.Cookie{rememberCk})
	c.Jar.SetCookies(steamStoreURL, []*http.Cookie{rememberCk})

	// Fetch RSA public key (retry up to 5 times, like steampy's MAX_RSA_ATTEMPTS)
	var rsaMod, rsaExp, rsaTs string
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
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
		// GET steamcommunity.com before RSA fetch (like steampy's _fetch_rsa_params)
		if req, err := c.newRequestWithContext(ctx, "GET", "https://steamcommunity.com/", nil, ""); err == nil {
			if resp, err := c.HTTPClient.Do(req); err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}

		var err error
		rsaMod, rsaExp, rsaTs, err = c.fetchRSAPublicKeyWithContext(ctx, username)
		if err == nil {
			break
		}
		lastErr = fmt.Errorf("fetch RSA key error: %w", err)
	}
	if rsaMod == "" {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("failed to fetch RSA public key")
	}

	// BeginAuthSession — single attempt, no retry (each attempt creates a new client_id on Steam)
	clientID, requestID, steamID, lastBeginBody, err := c.beginAuthSessionWithContext(ctx, username, password, rsaMod, rsaExp, rsaTs)
	if err != nil {
		return fmt.Errorf("begin auth session error: %w", err)
	}
	if clientID == 0 {
		return fmt.Errorf("begin auth session failed: %s", string(lastBeginBody))
	}

	c.mu.Lock()
	c.Config.SteamID = steamID
	c.mu.Unlock()

	// Generate 2FA code — single attempt, current time, no offset (like steampy)
	twoFactorCode, err := GenerateTwoFactorCode(sharedSecret, 0)
	if err != nil {
		return fmt.Errorf("failed to generate 2FA code: %w", err)
	}

	// Submit Steam Guard code — only code_type=3 (like steampy)
	if err := c.updateAuthSession2FAWithContext(ctx, clientID, steamID, twoFactorCode); err != nil {
		return err
	}

	// Poll for tokens (like steampy's _poll_session_status)
	rToken, aToken, err := c.pollAuthSessionStatusWithContext(ctx, clientID, requestID)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.Config.RefreshToken = rToken
	c.Config.AccessToken = aToken
	c.mu.Unlock()

	// Finalize login with the new refresh token
	if err := c.LoginWithRefreshTokenWithContext(ctx); err != nil {
		// Fallback: manually set cookies from tokens
		c.mu.RLock()
		sessionID := c.SessionID
		c.mu.RUnlock()
		jwtSteamLoginSecure := fmt.Sprintf("%s||%s", steamID, aToken)
		c.SetSessionCookies(sessionID, jwtSteamLoginSecure, rToken)
	}

	return nil
}

// Logout terminates the current Steam session by calling /login/logout/.
func (c *Client) Logout() error {
	return c.LogoutWithContext(context.Background())
}

// LogoutWithContext terminates the current Steam session with context support.
func (c *Client) LogoutWithContext(ctx context.Context) error {
	c.mu.RLock()
	sessionID := c.SessionID
	c.mu.RUnlock()

	data := url.Values{
		"sessionid": {sessionID},
	}

	// Create a standard navigate POST request (mimicking browser form submit to steamcommunity.com/login/logout/)
	body := strings.NewReader(data.Encode())
	req, err := c.newRequestWithContext(ctx, "POST", "https://steamcommunity.com/login/logout/", body, "https://steamcommunity.com/")
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://steamcommunity.com")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	// Check if session is still alive (post-check validation)
	alive, err := c.IsSessionAliveWithContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to verify session status after logout: %w", err)
	}

	if alive {
		return fmt.Errorf("logout unsuccessful: session is still alive")
	}

	c.mu.Lock()
	c.SessionID = ""
	c.SteamLoginSecure = ""
	c.LoggedIn = false
	c.mu.Unlock()

	if c.Jar != nil {
		c.Jar.ClearAuthCookies()
	}

	return nil
}

// IsSessionAlive checks whether the current Steam session is still authenticated.
// Modeled after steampy's _check_steam_session / is_session_alive.
func (c *Client) IsSessionAlive() (bool, error) {
	return c.IsSessionAliveWithContext(context.Background())
}

// IsSessionAliveWithContext checks session liveness with context support.
func (c *Client) IsSessionAliveWithContext(ctx context.Context) (bool, error) {
	// Check store account page — redirect to /login means session is dead
	storeReq, err := c.newRequestWithContext(ctx, "GET", "https://store.steampowered.com/account/", nil, "")
	if err != nil {
		return false, err
	}
	storeResp, err := c.HTTPClient.Do(storeReq)
	if err != nil {
		return false, err
	}
	storeBody, _ := readResponseBody(storeResp)
	_ = storeBody

	storeURL := ""
	if storeResp.Request != nil && storeResp.Request.URL != nil {
		storeURL = strings.ToLower(storeResp.Request.URL.String())
	}
	if strings.Contains(storeURL, "/login") {
		return false, nil
	}

	// Check community page for g_steamID marker
	communityReq, err := c.newRequestWithContext(ctx, "GET", "https://steamcommunity.com/", nil, "")
	if err != nil {
		// Store check passed, consider alive
		return storeResp.StatusCode == http.StatusOK, nil
	}
	communityResp, err := c.HTTPClient.Do(communityReq)
	if err != nil {
		return storeResp.StatusCode == http.StatusOK, nil
	}
	communityBody, _ := readResponseBody(communityResp)

	communityURL := ""
	if communityResp.Request != nil && communityResp.Request.URL != nil {
		communityURL = strings.ToLower(communityResp.Request.URL.String())
	}
	if strings.Contains(communityURL, "/login") {
		return false, nil
	}

	// Look for g_steamID marker (like steampy)
	if regexp.MustCompile(`g_steamID = "\d+";`).Match(communityBody) {
		return true, nil
	}

	// Fallback: check if username appears on community page
	c.mu.RLock()
	username := c.Config.Username
	c.mu.RUnlock()
	if username != "" && strings.Contains(strings.ToLower(string(communityBody)), strings.ToLower(username)) {
		return true, nil
	}

	// Last resort: trust store status code
	return storeResp.StatusCode == http.StatusOK, nil
}

// fetchRSAPublicKeyWithContext fetches the Steam RSA public key for the given username.
// Uses protobuf-encoded request identical to the Chrome browser:
//
//	GET /GetPasswordRSAPublicKey/v1?origin=...&input_protobuf_encoded=<pb>
//
// Returns publickey_mod (hex), publickey_exp (hex), and timestamp (raw uint64 as string).
func (c *Client) fetchRSAPublicKeyWithContext(ctx context.Context, username string) (mod, exp, timestamp string, err error) {
	// Build protobuf: field [1] account_name (string)
	pb := pbString(1, username)
	encoded := base64.StdEncoding.EncodeToString(pb)

	rsaReqURL := fmt.Sprintf(
		"https://api.steampowered.com/IAuthenticationService/GetPasswordRSAPublicKey/v1?origin=%s&input_protobuf_encoded=%s",
		url.QueryEscape("https://steamcommunity.com"),
		url.QueryEscape(encoded),
	)
	req, err := c.newRequestWithContext(ctx, "GET", rsaReqURL, nil, "https://steamcommunity.com/")
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Origin", "https://steamcommunity.com")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to fetch RSA key: %w", err)
	}

	body, err := readResponseBody(resp)
	if err != nil {
		return "", "", "", err
	}

	// Steam returns binary protobuf when query uses input_protobuf_encoded.
	// CAuthentication_GetPasswordRSAPublicKey_Response:
	//   [1] publickey_mod (string hex)
	//   [2] publickey_exp (string hex)
	//   [3] timestamp (uint64)
	fields, err := pbDecode(body)
	if err != nil {
		// Fallback: try JSON (old API format)
		var rsaResult struct {
			Response struct {
				PublicKeyMod string `json:"publickey_mod"`
				PublicKeyExp string `json:"publickey_exp"`
				Timestamp    string `json:"timestamp"`
			} `json:"response"`
		}
		if jsonErr := json.Unmarshal(body, &rsaResult); jsonErr == nil && rsaResult.Response.PublicKeyMod != "" {
			return rsaResult.Response.PublicKeyMod, rsaResult.Response.PublicKeyExp, rsaResult.Response.Timestamp, nil
		}
		return "", "", "", fmt.Errorf("invalid RSA key response (pb: %v)", err)
	}

	mod = pbGetString(fields, 1)
	exp = pbGetString(fields, 2)
	ts := pbGetUint64(fields, 3)
	if mod == "" || exp == "" {
		return "", "", "", fmt.Errorf("invalid RSA key response: missing fields")
	}
	return mod, exp, strconv.FormatUint(ts, 10), nil
}

// beginAuthSessionWithContext begins a Steam credential auth session.
// Uses protobuf-encoded multipart/form-data identical to the Chrome browser:
//
//	POST /BeginAuthSessionViaCredentials/v1
//	Content-Type: multipart/form-data
//	input_protobuf_encoded=<base64(pb)>
//
// Returns clientID (uint64), requestID (raw 16 bytes), steamID (string).
func (c *Client) beginAuthSessionWithContext(ctx context.Context, username, password, modHex, expHex, timestamp string) (clientID uint64, requestID []byte, steamID string, rawBody []byte, err error) {
	encryptedPassword, err := encryptPassword(password, modHex, expHex)
	if err != nil {
		return 0, nil, "", nil, err
	}

	// Parse timestamp: RSA response returns it as uint64 string.
	ts, _ := strconv.ParseUint(timestamp, 10, 64)

	// Build nested device_details message:
	//   field [1] device_friendly_name = User-Agent (matches browser)
	//   field [2] platform_type = 2 (Web)
	deviceDetails := append(pbString(1, defaultUserAgent), pbVarint(2, 2)...)

	// Build CAuthentication_BeginAuthSessionViaCredentials_Request:
	//   [2] account_name
	//   [3] encrypted_password
	//   [4] encryption_timestamp (uint64)
	//   [5] remember_login = 1
	//   [7] persistence = 1
	//   [8] website_id = "Community"
	//   [9] device_details (nested)
	//   [11] language = 0
	var pb []byte
	pb = append(pb, pbString(2, username)...)
	pb = append(pb, pbString(3, encryptedPassword)...)
	pb = append(pb, pbVarint(4, ts)...)
	pb = append(pb, pbVarint(5, 1)...) // remember_login
	pb = append(pb, pbVarint(7, 1)...) // persistence
	pb = append(pb, pbString(8, "Community")...)
	pb = append(pb, pbNested(9, deviceDetails)...)
	pb = append(pb, pbVarint(11, 0)...) // language

	beginReq, err := c.newMultipartProtoRequest(
		ctx,
		"https://api.steampowered.com/IAuthenticationService/BeginAuthSessionViaCredentials/v1",
		pb,
		"https://steamcommunity.com/",
	)
	if err != nil {
		return 0, nil, "", nil, err
	}

	beginResp, err := c.doRequestWithRetry(ctx, beginReq)
	if err != nil {
		return 0, nil, "", nil, err
	}

	rawBody, readErr := readResponseBody(beginResp)
	if readErr != nil {
		return 0, nil, "", nil, readErr
	}

	if beginResp.StatusCode != http.StatusOK {
		return 0, nil, "", rawBody, fmt.Errorf("begin auth session HTTP %d (x-eresult: %s): %s",
			beginResp.StatusCode, beginResp.Header.Get("X-eresult"), string(rawBody))
	}
	if eresult := beginResp.Header.Get("X-eresult"); eresult != "" && eresult != "1" {
		return 0, nil, "", rawBody, fmt.Errorf("begin auth session failed with eresult %s: %s", eresult, string(rawBody))
	}

	// CAuthentication_BeginAuthSessionViaCredentials_Response:
	//   [1] client_id  (uint64 varint)
	//   [2] request_id (bytes, 16 raw bytes)
	//   [3] interval   (float32)
	//   [5] steamid    (uint64 varint)
	fields, err := pbDecode(rawBody)
	if err != nil {
		return 0, nil, "", rawBody, fmt.Errorf("failed to begin auth session: %w", err)
	}

	clientID = pbGetUint64(fields, 1)
	requestID = pbGetBytes(fields, 2)
	sidUint := pbGetUint64(fields, 5)
	if sidUint != 0 {
		steamID = strconv.FormatUint(sidUint, 10)
	}

	if clientID == 0 {
		return 0, nil, "", rawBody, fmt.Errorf("failed to begin auth session: empty client_id")
	}
	return clientID, requestID, steamID, rawBody, nil
}

// updateAuthSession2FAWithContext submits the Steam Guard TOTP code for the auth session.
// Uses protobuf-encoded multipart/form-data identical to the Chrome browser.
// Only uses code_type=3 (DeviceConfirmation), matching steampy's behavior.
func (c *Client) updateAuthSession2FAWithContext(ctx context.Context, clientID uint64, steamID, twoFactorCode string) error {
	sidUint, _ := strconv.ParseUint(steamID, 10, 64)

	// CAuthentication_UpdateAuthSessionWithSteamGuardCode_Request:
	//   [1] client_id  (uint64 varint)
	//   [2] steamid    (fixed64 wire type 1)
	//   [3] code       (string)
	//   [4] code_type  = 3 (DeviceConfirmation, like steampy)
	var pb []byte
	pb = append(pb, pbVarint(1, clientID)...)
	if sidUint > 0 {
		pb = append(pb, pbInt64(2, sidUint)...)
	}
	pb = append(pb, pbString(3, twoFactorCode)...)
	pb = append(pb, pbVarint(4, 3)...) // code_type=3 only

	updateReq, err := c.newMultipartProtoRequest(
		ctx,
		"https://api.steampowered.com/IAuthenticationService/UpdateAuthSessionWithSteamGuardCode/v1",
		pb,
		"https://steamcommunity.com/",
	)
	if err != nil {
		return err
	}

	updateResp, err := c.doRequestWithRetry(ctx, updateReq)
	if err != nil {
		return fmt.Errorf("update auth session failed: %w", err)
	}

	body, _ := readResponseBody(updateResp)
	if updateResp.StatusCode != http.StatusOK {
		return fmt.Errorf("update auth session HTTP %d: %s (x-eresult: %s)",
			updateResp.StatusCode, string(body), updateResp.Header.Get("X-eresult"))
	}
	if eresult := updateResp.Header.Get("X-eresult"); eresult != "" && eresult != "1" {
		return fmt.Errorf("update auth session failed with eresult %s: %s", eresult, string(body))
	}

	return nil
}

// pollAuthSessionStatusOnce executes a single PollAuthSessionStatus request to register session state.
func (c *Client) pollAuthSessionStatusOnce(ctx context.Context, clientID uint64, requestID []byte) (refreshToken, accessToken string, err error) {
	var pb []byte
	pb = append(pb, pbVarint(1, clientID)...)
	if len(requestID) > 0 {
		pb = append(pb, pbBytes(2, requestID)...)
	}

	pollReq, err := c.newMultipartProtoRequest(
		ctx,
		"https://api.steampowered.com/IAuthenticationService/PollAuthSessionStatus/v1",
		pb,
		"https://steamcommunity.com/",
	)
	if err != nil {
		return "", "", err
	}

	pollResp, err := c.doRequestWithRetry(ctx, pollReq)
	if err != nil {
		return "", "", err
	}

	body, readErr := readResponseBody(pollResp)
	if readErr != nil {
		return "", "", readErr
	}

	if eresult := pollResp.Header.Get("X-eresult"); eresult != "" && eresult != "1" {
		return "", "", fmt.Errorf("poll auth status eresult: %s", eresult)
	}

	fields, pbErr := pbDecode(body)
	if pbErr == nil {
		rToken := pbGetString(fields, 3)
		aToken := pbGetString(fields, 4)
		if rToken != "" {
			return rToken, aToken, nil
		}
	}
	return "", "", nil
}

// pollAuthSessionStatusWithContext polls Steam until the auth session produces tokens.
// Uses protobuf-encoded multipart/form-data identical to the Chrome browser.
//
// CPollAuthSessionStatus_Response fields:
//
//	[3] refresh_token (string JWT)
//	[4] access_token  (string JWT)
func (c *Client) pollAuthSessionStatusWithContext(ctx context.Context, clientID uint64, requestID []byte) (refreshToken, accessToken string, err error) {
	var lastPollErr error

	for attempt := 0; attempt < 15; attempt++ {
		select {
		case <-ctx.Done():
			if lastPollErr != nil {
				return "", "", fmt.Errorf("%w (poll error: %v)", ctx.Err(), lastPollErr)
			}
			return "", "", ctx.Err()
		case <-time.After(1 * time.Second):
		}

		rToken, aToken, err := c.pollAuthSessionStatusOnce(ctx, clientID, requestID)
		if err == nil && rToken != "" {
			return rToken, aToken, nil
		}
		if err != nil {
			lastPollErr = err
			if strings.Contains(err.Error(), "eresult:") && !strings.Contains(err.Error(), "84") {
				return "", "", err
			}
		} else {
			lastPollErr = fmt.Errorf("poll auth status pending (attempt %d)", attempt+1)
		}
	}

	if lastPollErr != nil {
		return "", "", fmt.Errorf("poll auth status failed: %w", lastPollErr)
	}

	return "", "", fmt.Errorf("poll auth status timed out / failed")
}
