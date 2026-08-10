package steam

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TrackingCookieJar wraps a standard http.CookieJar and additionally stores
// the full *http.Cookie objects (including attributes like Secure, HttpOnly, Expires)
// because the standard cookie jar strips these attributes when calling Cookies().
type TrackingCookieJar struct {
	jar http.CookieJar

	mu      sync.RWMutex
	cookies map[string]*http.Cookie
}

func NewTrackingCookieJar() (*TrackingCookieJar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &TrackingCookieJar{
		jar:     jar,
		cookies: make(map[string]*http.Cookie),
	}, nil
}

func (t *TrackingCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	// First, pass to the underlying jar to handle logic (expiration, etc)
	t.jar.SetCookies(u, cookies)

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, c := range cookies {
		// Key by Domain + Name + Path
		domain := c.Domain
		if domain == "" {
			domain = u.Host
		}
		path := c.Path
		if path == "" {
			path = "/"
		}
		key := domain + ":" + c.Name + ":" + path

		if c.MaxAge < 0 || (!c.Expires.IsZero() && c.Expires.Unix() < 0) {
			// Cookie is being deleted
			delete(t.cookies, key)
		} else {
			// Store a clone of the cookie so we don't mutate caller's copy
			clone := *c
			clone.Domain = domain
			clone.Path = path
			t.cookies[key] = &clone
		}
	}
}

func (t *TrackingCookieJar) Cookies(u *url.URL) []*http.Cookie {
	return t.jar.Cookies(u)
}

func (t *TrackingCookieJar) GetAllCookies() []*http.Cookie {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []*http.Cookie
	for _, c := range t.cookies {
		clone := *c
		result = append(result, &clone)
	}
	return result
}

// ClearAuthCookies removes all session and authentication cookies from the jar
// to prevent session cross-contamination when changing accounts or logins,
// matching steampy's _clear_auth_cookies behavior.
func (t *TrackingCookieJar) ClearAuthCookies() {
	t.mu.Lock()
	defer t.mu.Unlock()

	authNames := map[string]bool{
		"sessionid":          true,
		"steamLoginSecure":   true,
		"steamRefresh_steam": true,
		"steamCountry":       true,
		"steamRememberLogin": true,
	}

	// We expire cookies in the underlying jar by setting them with a past expiration date
	expiredTime := time.Unix(1, 0)

	// Group tracking cookies by domain/path to clear them in the underlying jar
	cookiesToClearByHost := make(map[string][]*http.Cookie)

	for key, ck := range t.cookies {
		if authNames[ck.Name] {
			delete(t.cookies, key)

			expiredCk := &http.Cookie{
				Name:    ck.Name,
				Value:   "",
				Path:    ck.Path,
				Domain:  ck.Domain,
				Expires: expiredTime,
				MaxAge:  -1,
			}

			// We need a proper URL scheme for Jar.SetCookies
			scheme := "https"
			host := ck.Domain
			if strings.HasPrefix(host, ".") {
				host = host[1:]
			}
			uStr := scheme + "://" + host + ck.Path
			cookiesToClearByHost[uStr] = append(cookiesToClearByHost[uStr], expiredCk)
		}
	}

	for uStr, cks := range cookiesToClearByHost {
		if u, err := url.Parse(uStr); err == nil {
			t.jar.SetCookies(u, cks)
		}
	}
}
