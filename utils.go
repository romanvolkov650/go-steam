package steam

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	steamCommunityURL, _ = url.Parse("https://steamcommunity.com")
	steamStoreURL, _     = url.Parse("https://store.steampowered.com")
	steamLoginURL, _     = url.Parse("https://login.steampowered.com")
	steamAPIURL, _       = url.Parse("https://api.steampowered.com")
	steamCheckoutURL, _  = url.Parse("https://checkout.steampowered.com")
	steamHelpURL, _      = url.Parse("https://help.steampowered.com")
	steamTVURL, _        = url.Parse("https://steam.tv")

	allSteamURLs = []*url.URL{
		steamCommunityURL,
		steamStoreURL,
		steamLoginURL,
		steamAPIURL,
		steamCheckoutURL,
		steamHelpURL,
		steamTVURL,
	}
)

// FlexibleBool unmarshals JSON booleans from boolean (true/false) or numeric (1/0) values.
type FlexibleBool bool

func (b *FlexibleBool) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "1" || s == "true" {
		*b = true
		return nil
	}
	if s == "0" || s == "false" || s == "null" || s == "" {
		*b = false
		return nil
	}
	var val interface{}
	if err := json.Unmarshal(data, &val); err == nil {
		switch v := val.(type) {
		case bool:
			*b = FlexibleBool(v)
		case float64:
			*b = FlexibleBool(v == 1)
		}
	}
	return nil
}

// FlexibleUint64 unmarshals JSON numbers regardless of whether they are formatted as a number (123) or string ("123").
type FlexibleUint64 uint64

func (f *FlexibleUint64) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*f = 0
		return nil
	}

	s := string(data)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}

	if s == "" {
		*f = 0
		return nil
	}

	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return err
	}

	*f = FlexibleUint64(val)
	return nil
}

func generateRandomSessionID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func encryptPassword(password, modHex, expHex string) (string, error) {
	modBytes, ok := new(big.Int).SetString(modHex, 16)
	if !ok {
		return "", fmt.Errorf("invalid modHex")
	}
	expInt, err := strconv.ParseInt(expHex, 16, 64)
	if err != nil {
		return "", fmt.Errorf("invalid expHex")
	}

	pub := &rsa.PublicKey{
		N: modBytes,
		E: int(expInt),
	}

	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(password))
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(encrypted), nil
}

func accountIDToSteamID64(accountID uint32) string {
	const steamID64Base = 76561197960265728
	return strconv.FormatUint(steamID64Base+uint64(accountID), 10)
}

// SteamID64ToAccountID converts a 64-bit SteamID string into a 32-bit AccountID uint32.
func SteamID64ToAccountID(steamID64 string) uint32 {
	const steamID64Base = 76561197960265728
	val, err := strconv.ParseUint(steamID64, 10, 64)
	if err != nil || val <= steamID64Base {
		return 0
	}
	return uint32(val - steamID64Base)
}

// ParseTradeURL parses a Steam trade offer link into partner SteamID64 and token.
func ParseTradeURL(tradeURL string) (string, string, error) {
	u, err := url.Parse(tradeURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid trade url: %w", err)
	}
	q := u.Query()
	partner3 := q.Get("partner")
	token := q.Get("token")

	if partner3 == "" {
		return "", "", fmt.Errorf("missing 'partner' parameter in trade url")
	}

	accountID, err := strconv.ParseUint(partner3, 10, 32)
	if err != nil {
		return "", "", fmt.Errorf("invalid partner id '%s': %w", partner3, err)
	}

	partner64 := accountIDToSteamID64(uint32(accountID))
	return partner64, token, nil
}

type ReqType int

const (
	ReqTypeNavigate ReqType = iota
	ReqTypeAjaxXHR
	ReqTypeFetch
)

func setBrowserHeaders(req *http.Request, reqType ReqType) {
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("sec-ch-ua", `"Not(A:Brand";v="99", "Google Chrome";v="151", "Chromium";v="151"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	req.Header.Set("accept-language", "en-US,en;q=0.9,ru;q=0.8")
	req.Header.Set("accept-encoding", "gzip, deflate, br")

	// Determine sec-fetch-site based on URL and Referer
	secFetchSite := "none"
	referer := req.Header.Get("Referer")
	if referer != "" && req.URL != nil {
		refURL, err := url.Parse(referer)
		if err == nil {
			if refURL.Host == req.URL.Host {
				secFetchSite = "same-origin"
			} else {
				if strings.HasSuffix(refURL.Host, "steampowered.com") && strings.HasSuffix(req.URL.Host, "steampowered.com") {
					secFetchSite = "same-site"
				} else if strings.HasSuffix(refURL.Host, "steamcommunity.com") && strings.HasSuffix(req.URL.Host, "steamcommunity.com") {
					secFetchSite = "same-site"
				} else {
					secFetchSite = "cross-site"
				}
			}
		}
	}
	req.Header.Set("sec-fetch-site", secFetchSite)

	switch reqType {
	case ReqTypeNavigate:
		req.Header.Set("accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
		req.Header.Set("upgrade-insecure-requests", "1")
		req.Header.Set("sec-fetch-mode", "navigate")
		req.Header.Set("sec-fetch-dest", "document")
		req.Header.Set("sec-fetch-user", "?1")
	case ReqTypeAjaxXHR:
		req.Header.Set("accept", "*/*")
		req.Header.Set("sec-fetch-mode", "cors")
		req.Header.Set("sec-fetch-dest", "empty")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	case ReqTypeFetch:
		req.Header.Set("accept", "*/*")
		req.Header.Set("sec-fetch-mode", "cors")
		req.Header.Set("sec-fetch-dest", "empty")
	}
}

// newRequest creates an http.Request with common headers attached.
func (c *Client) newRequest(method, reqURL string, body io.Reader, referer string) (*http.Request, error) {
	return c.newRequestWithContext(context.Background(), method, reqURL, body, referer)
}

// newRequestWithContext creates an http.Request bound to ctx with common headers attached.
func (c *Client) newRequestWithContext(ctx context.Context, method, reqURL string, body io.Reader, referer string) (*http.Request, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, err
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	setBrowserHeaders(req, ReqTypeNavigate)
	req.Header.Set("Connection", "keep-alive")
	if req.URL != nil && req.URL.Host != "" {
		req.Header.Set("Host", req.URL.Host)
		req.Host = req.URL.Host
	}
	return req, nil
}

// newAjaxPostRequest creates a POST http.Request with urlencoded body and standard Steam AJAX headers.
func (c *Client) newAjaxPostRequest(reqURL string, formData url.Values, referer string) (*http.Request, error) {
	return c.newAjaxPostRequestWithContext(context.Background(), reqURL, formData, referer)
}

// newAjaxPostRequestWithContext creates a POST http.Request bound to ctx with urlencoded body and standard Steam AJAX headers.
func (c *Client) newAjaxPostRequestWithContext(ctx context.Context, reqURL string, formData url.Values, referer string) (*http.Request, error) {
	var body io.Reader
	if formData != nil {
		body = strings.NewReader(formData.Encode())
	}
	req, err := c.newRequestWithContext(ctx, "POST", reqURL, body, referer)
	if err != nil {
		return nil, err
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	setBrowserHeaders(req, ReqTypeAjaxXHR)
	req.Header.Set("Origin", "https://steamcommunity.com")
	req.Header.Set("Connection", "keep-alive")
	if req.URL != nil && req.URL.Host != "" {
		req.Header.Set("Host", req.URL.Host)
		req.Host = req.URL.Host
	}
	return req, nil
}

// newFetchRequestWithContext creates an http.Request bound to ctx with standard modern Fetch API headers.
func (c *Client) newFetchRequestWithContext(ctx context.Context, method, reqURL string, body io.Reader, referer string) (*http.Request, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, err
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	setBrowserHeaders(req, ReqTypeFetch)
	req.Header.Set("Connection", "keep-alive")
	if req.URL != nil && req.URL.Host != "" {
		req.Header.Set("Host", req.URL.Host)
		req.Host = req.URL.Host
	}
	return req, nil
}

// doRequestWithRetry executes an HTTP request, automatically retrying transient errors (429, 502, 503, 504) with exponential backoff.
//
// Cookie handling strategy:
//   - http.Client.Jar automatically applies cookies from the jar for the request URL
//     and stores Set-Cookie responses — this is the primary mechanism.
//   - Auth cookies are set on 2 primary domains during login and synced via syncSessionCookies().
//   - We do NOT inject cookies on every request; the jar handles it.
func (c *Client) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	maxRetries := 3
	backoff := 500 * time.Millisecond

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt == maxRetries {
				return nil, err
			}
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504 {
			if attempt == maxRetries {
				return resp, nil
			}
			resp.Body.Close()
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("request failed after retries")
}

// readResponseBody reads the full response body, transparently handling gzip decompression if Content-Encoding is gzip.
func readResponseBody(resp *http.Response) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	defer resp.Body.Close()

	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gzReader.Close()
		return io.ReadAll(gzReader)
	}

	return io.ReadAll(resp.Body)
}

// ensureSessionCookiesForURL injects steamcommunity.com session cookies (sessionid,
// steamLoginSecure) into the jar for the given URL **only when they are missing**.
//
// Important: sessionid is ONLY propagated to the steamcommunity.com origin.
// Other Steam subdomains (store, help, checkout, steam.tv) each receive their own
// domain-specific sessionid via Set-Cookie from /login/settoken responses, which
// the http.Client Jar stores automatically. We must not overwrite those with the
// community sessionid, as that would break per-domain authentication.
func (c *Client) ensureSessionCookiesForURL(u *url.URL) {
	if u == nil || c.Jar == nil {
		return
	}
	c.mu.RLock()
	sessionID := c.SessionID
	steamLoginSecure := c.SteamLoginSecure
	c.mu.RUnlock()

	if sessionID == "" && steamLoginSecure == "" {
		return
	}

	isCommunity := strings.Contains(u.Host, "steamcommunity.com")

	cookies := c.Jar.Cookies(u)
	hasSessionID := false
	hasSteamLogin := false

	for _, ck := range cookies {
		if ck.Name == "sessionid" && ck.Value != "" {
			hasSessionID = true
		}
		if ck.Name == "steamLoginSecure" && ck.Value != "" {
			hasSteamLogin = true
		}
	}

	// sessionid is typically organically fetched by InitSession.
	// We only sync the primary SessionID to steamcommunity.com if it's missing in the jar
	// (e.g. for legacy tests or manually constructed clients).
	if !hasSessionID && sessionID != "" && isCommunity {
		c.Jar.SetCookies(u, []*http.Cookie{{Name: "sessionid", Value: sessionID, Path: "/"}})
	}
	if !hasSteamLogin && steamLoginSecure != "" {
		secVal := strings.ReplaceAll(steamLoginSecure, "|", "%7C")
		c.Jar.SetCookies(u, []*http.Cookie{{Name: "steamLoginSecure", Value: secVal, Path: "/", Secure: true, HttpOnly: true}})
	}
}

// approveConfirmationForID polls confirmations and approves the confirmation matching targetID.
func (c *Client) approveConfirmationForID(targetID string) error {
	return c.approveConfirmationForIDWithContext(context.Background(), targetID)
}

// approveConfirmationForIDWithContext polls confirmations and approves the confirmation matching targetID with context support.
func (c *Client) approveConfirmationForIDWithContext(ctx context.Context, targetID string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}

		confs, err := c.GetConfirmationsWithContext(ctx)
		if err != nil {
			lastErr = fmt.Errorf("failed to fetch confirmations: %w", err)
			continue
		}

		if len(confs) == 0 {
			lastErr = fmt.Errorf("no pending confirmations found")
			continue
		}

		for _, conf := range confs {
			if conf.CreatorID == targetID || strings.Contains(conf.Title, targetID) {
				if err := c.SendConfirmationActionWithContext(ctx, conf, "allow"); err != nil {
					return fmt.Errorf("failed to confirm ID %s: %w", targetID, err)
				}
				return nil
			}
		}

		// Fallback: approve first pending confirmation
		if len(confs) > 0 {
			if err := c.SendConfirmationActionWithContext(ctx, confs[0], "allow"); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no pending confirmation found for ID %s", targetID)
}

// GenerateDeviceID generates a deterministic Steam Android device ID for a given steamID64.
// Matches Steam Desktop Authenticator (SDA) and steampy standard format: android:XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX.
func GenerateDeviceID(steamID string) string {
	if steamID == "" {
		return "android:00000000-0000-0000-0000-000000000000"
	}
	h := sha1.Sum([]byte(steamID))
	hexed := hex.EncodeToString(h[:])
	return fmt.Sprintf("android:%s-%s-%s-%s-%s",
		hexed[:8], hexed[8:12], hexed[12:16], hexed[16:20], hexed[20:32])
}

func (c *Client) getDeviceID() string {
	c.mu.RLock()
	devID := c.Config.DeviceID
	steamID := c.Config.SteamID
	c.mu.RUnlock()

	if devID != "" {
		return devID
	}
	if steamID != "" {
		genID := GenerateDeviceID(steamID)
		c.mu.Lock()
		c.Config.DeviceID = genID
		c.mu.Unlock()
		return genID
	}
	return "android:00000000-0000-0000-0000-000000000000"
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

// doRequestAndRead executes an HTTP request with retry logic and reads the full response body.
func (c *Client) doRequestAndRead(ctx context.Context, req *http.Request) ([]byte, *http.Response, error) {
	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	body, err := readResponseBody(resp)
	if err != nil {
		return nil, resp, err
	}
	return body, resp, nil
}

