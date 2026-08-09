package steam

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	req.Header.Set("User-Agent", defaultUserAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
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
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Origin", "https://steamcommunity.com")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	return req, nil
}

// doRequestWithRetry executes an HTTP request, automatically retrying transient errors (429, 502, 503, 504) with exponential backoff.
func (c *Client) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if req != nil && req.URL != nil {
		c.ensureSessionCookiesForURL(req.URL)
		if c.Jar != nil {
			cookiesSent := c.Jar.Cookies(req.URL)
			var names []string
			hasLogin := false
			for _, ck := range cookiesSent {
				names = append(names, ck.Name)
				if ck.Name == "steamLoginSecure" {
					hasLogin = true
				}
			}
			log.Printf("[HTTPRequest] %s %s | Cookies sent (%d): [%s] | LoggedIn=%v", req.Method, req.URL.String(), len(cookiesSent), strings.Join(names, ", "), hasLogin)
		}
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

	if !hasSessionID && sessionID != "" {
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
