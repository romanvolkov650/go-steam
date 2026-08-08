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
	cookies := []*http.Cookie{
		{Name: "sessionid", Value: c.SessionID, Domain: "steamcommunity.com", Path: "/"},
		{Name: "sessionid", Value: c.SessionID, Domain: "store.steampowered.com", Path: "/"},
	}

	c.Jar.SetCookies(steamCommunityURL, cookies)
	c.Jar.SetCookies(steamStoreURL, cookies)
}

// CookieJSON is a serializable representation of an HTTP cookie for JSON export/import.
type CookieJSON struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires,omitempty"`
	Secure   bool      `json:"secure"`
	HttpOnly bool      `json:"http_only"`
}

func (c *Client) SetSessionCookies(sessionID, steamLoginSecure, refreshToken string) {
	c.mu.Lock()
	c.SessionID = sessionID
	c.SteamLoginSecure = steamLoginSecure
	c.LoggedIn = steamLoginSecure != ""
	steamID := c.Config.SteamID
	c.mu.Unlock()

	cleanLoginSecure := strings.ReplaceAll(steamLoginSecure, "%7C", "|")
	cleanRefresh := strings.ReplaceAll(refreshToken, "%7C", "|")

	cookiesCommunity := []*http.Cookie{}
	cookiesStore := []*http.Cookie{}

	if sessionID != "" {
		cookiesCommunity = append(cookiesCommunity, &http.Cookie{Name: "sessionid", Value: sessionID, Domain: "steamcommunity.com", Path: "/"})
		cookiesStore = append(cookiesStore, &http.Cookie{Name: "sessionid", Value: sessionID, Domain: "store.steampowered.com", Path: "/"})
	}
	if steamLoginSecure != "" {
		cookiesCommunity = append(cookiesCommunity, &http.Cookie{Name: "steamLoginSecure", Value: cleanLoginSecure, Domain: "steamcommunity.com", Path: "/", Secure: true, HttpOnly: true})
		cookiesStore = append(cookiesStore, &http.Cookie{Name: "steamLoginSecure", Value: cleanLoginSecure, Domain: "store.steampowered.com", Path: "/", Secure: true, HttpOnly: true})
	}
	if refreshToken != "" {
		refreshValue := cleanRefresh
		if !strings.Contains(refreshValue, "||") && steamID != "" {
			refreshValue = fmt.Sprintf("%s||%s", steamID, cleanRefresh)
		}
		cookiesCommunity = append(cookiesCommunity, &http.Cookie{Name: "steamRefresh_steam", Value: refreshValue, Domain: "steamcommunity.com", Path: "/", Secure: true, HttpOnly: true})
		cookiesStore = append(cookiesStore, &http.Cookie{Name: "steamRefresh_steam", Value: refreshValue, Domain: "store.steampowered.com", Path: "/", Secure: true, HttpOnly: true})
	}

	c.Jar.SetCookies(steamCommunityURL, cookiesCommunity)
	c.Jar.SetCookies(steamStoreURL, cookiesStore)
}

// ExportCookies exports all active session cookies from the CookieJar into JSON format.
func (c *Client) ExportCookies() ([]*CookieJSON, error) {
	commCookies := c.Jar.Cookies(steamCommunityURL)
	storeCookies := c.Jar.Cookies(steamStoreURL)

	seen := make(map[string]bool)
	var result []*CookieJSON

	for _, ck := range append(commCookies, storeCookies...) {
		domain := ck.Domain
		if domain == "" {
			domain = "steamcommunity.com"
		}
		path := ck.Path
		if path == "" {
			path = "/"
		}

		key := fmt.Sprintf("%s:%s:%s", domain, ck.Name, path)
		if seen[key] {
			continue
		}
		seen[key] = true

		result = append(result, &CookieJSON{
			Name:     ck.Name,
			Value:    ck.Value,
			Domain:   domain,
			Path:     path,
			Expires:  ck.Expires,
			Secure:   ck.Secure,
			HttpOnly: ck.HttpOnly,
		})
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
	var commCookies []*http.Cookie
	var storeCookies []*http.Cookie

	for _, ck := range cookies {
		path := ck.Path
		if path == "" {
			path = "/"
		}

		domain := strings.TrimPrefix(ck.Domain, ".")

		httpCookie := &http.Cookie{
			Name:     ck.Name,
			Value:    ck.Value,
			Domain:   domain,
			Path:     path,
			Expires:  ck.Expires,
			Secure:   ck.Secure,
			HttpOnly: ck.HttpOnly,
		}

		if ck.Name == "sessionid" && ck.Value != "" {
			c.mu.Lock()
			c.SessionID = ck.Value
			c.mu.Unlock()
		}
		if ck.Name == "steamLoginSecure" && ck.Value != "" {
			c.mu.Lock()
			c.SteamLoginSecure = ck.Value
			c.LoggedIn = true

			if c.Config.SteamID == "" {
				cleanVal := strings.ReplaceAll(ck.Value, "%7C", "|")
				parts := strings.Split(cleanVal, "|")
				if len(parts) > 0 && len(parts[0]) == 17 {
					c.Config.SteamID = parts[0]
				}
			}
			c.mu.Unlock()
		}

		if strings.Contains(ck.Domain, "steamcommunity.com") {
			commCookies = append(commCookies, httpCookie)
		} else if strings.Contains(ck.Domain, "steampowered.com") {
			storeCookies = append(storeCookies, httpCookie)
		} else {
			commCookies = append(commCookies, httpCookie)
			storeCookies = append(storeCookies, httpCookie)
		}
	}

	c.Jar.SetCookies(steamCommunityURL, commCookies)
	c.Jar.SetCookies(steamStoreURL, storeCookies)
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

		// Perform /login/settoken transfer for each domain (store, community, help, checkout)
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
			if err == nil {
				tResp, err := c.HTTPClient.Do(tReq)
				if err == nil {
					tResp.Body.Close()
				}
			}
		}
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

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}

		mod, exp, ts, err := c.fetchRSAPublicKeyWithContext(ctx, username)
		if err != nil {
			continue
		}

		clientID, requestID, steamID, lastBeginBody, err = c.beginAuthSessionWithContext(ctx, username, password, mod, exp, ts)
		if err == nil && clientID != "" {
			break
		}
	}

	if clientID == "" {
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

	for attempt := 0; attempt < 5; attempt++ {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}

		pollReq, err := c.newAjaxPostRequestWithContext(ctx, "https://api.steampowered.com/IAuthenticationService/PollAuthSessionStatus/v1/", pollData, "https://steamcommunity.com/")
		if err != nil {
			continue
		}

		pollResp, err := c.doRequestWithRetry(ctx, pollReq)
		if err != nil {
			continue
		}

		pollBody, readErr := io.ReadAll(pollResp.Body)
		pollResp.Body.Close()

		if readErr == nil && json.Unmarshal(pollBody, &pollResult) == nil && pollResult.Response.RefreshToken != "" {
			return pollResult.Response.RefreshToken, pollResult.Response.AccessToken, nil
		}
	}

	return "", "", fmt.Errorf("poll auth status timed out / failed")
}
