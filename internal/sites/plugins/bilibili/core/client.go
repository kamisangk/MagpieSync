//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"magpiesync/internal/netproxy"
)

const (
	apiBase      = "https://api.bilibili.com"
	passportBase = "https://passport.bilibili.com"
	userAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

type Client struct {
	http    *http.Client
	jar     http.CookieJar
	limiter *rate.Limiter

	mu  sync.Mutex
	wbi *wbiKeys

	cookiesMu      sync.Mutex
	lastCookies    map[string]string
	explicitCookie bool
}

func NewGuest() *Client {
	c, _ := newClientWithProxy(nil, 1.5, "")
	return c
}

// NewGuestWithProxy creates a guest client routed through an HTTP(S) or SOCKS5
// proxy. An empty proxy means a direct connection.
func NewGuestWithProxy(proxyURL string) (*Client, error) {
	return newClientWithProxy(nil, 1.5, proxyURL)
}

func NewWithCookies(cookies map[string]string, ratePerSecond float64) *Client {
	c, _ := newClientWithProxy(cookies, ratePerSecond, "")
	c.explicitCookie = true
	return c
}

// NewWithCookiesWithProxy is NewWithCookies with an optional proxy.
func NewWithCookiesWithProxy(cookies map[string]string, ratePerSecond float64, proxyURL string) (*Client, error) {
	c, err := newClientWithProxy(cookies, ratePerSecond, proxyURL)
	if err != nil {
		return nil, err
	}
	c.explicitCookie = true
	return c, nil
}

func newClientWithProxy(cookies map[string]string, rps float64, proxyURL string) (*Client, error) {
	jar, _ := cookiejar.New(nil)
	if rps <= 0 {
		rps = 1.5
	}
	tr, err := netproxy.Transport(proxyURL)
	if err != nil {
		return nil, err
	}
	// netproxy.Transport returns (nil, nil) for an empty proxy; a typed-nil
	// *http.Transport stored into http.Client would panic on the first request.
	// Fall back to a clone of the default transport, like netproxy.NewClient.
	if tr == nil {
		tr = http.DefaultTransport.(*http.Transport).Clone()
	}
	c := &Client{
		http:        &http.Client{Timeout: 25 * time.Second, Jar: jar, Transport: tr},
		jar:         jar,
		limiter:     rate.NewLimiter(rate.Limit(rps), 3),
		lastCookies: make(map[string]string),
	}
	if cookies != nil {
		c.seedCookies(cookies)
	}
	return c, nil
}

func (c *Client) seedCookies(cookies map[string]string) {
	for _, host := range []string{"api.bilibili.com", "www.bilibili.com", "passport.bilibili.com", "space.bilibili.com"} {
		u := &url.URL{Scheme: "https", Host: host}
		list := make([]*http.Cookie, 0, len(cookies))
		for k, v := range cookies {
			if k == "" || v == "" {
				continue
			}
			list = append(list, &http.Cookie{Name: k, Value: v, Path: "/"})
		}
		c.jar.SetCookies(u, list)
	}
}

// EnsureBuvid obtains a device fingerprint when the QR login cookies do not
// include one. Some bilibili endpoints reject cookie-only sessions without it.
func (c *Client) EnsureBuvid(ctx context.Context) {
	if c.CookieMap()["buvid3"] != "" {
		return
	}
	var d struct {
		B3 string `json:"b_3"`
		B4 string `json:"b_4"`
	}
	if err := c.apiGet(ctx, "/x/frontend/finger/spi", nil, false, &d); err != nil {
		return
	}
	extra := make(map[string]string, 3)
	if d.B3 != "" {
		extra["buvid3"] = d.B3
	}
	if d.B4 != "" {
		extra["buvid4"] = d.B4
	}
	extra["b_nut"] = strconv.FormatInt(time.Now().Unix(), 10)
	c.seedCookies(extra)
}

func (c *Client) CookieMap() map[string]string {
	out := make(map[string]string)
	for _, host := range []string{"api.bilibili.com", "www.bilibili.com", "passport.bilibili.com"} {
		u := &url.URL{Scheme: "https", Host: host}
		for _, ck := range c.jar.Cookies(u) {
			out[ck.Name] = ck.Value
		}
	}
	c.cookiesMu.Lock()
	for k, v := range c.lastCookies {
		out[k] = v
	}
	c.cookiesMu.Unlock()
	return out
}

func (c *Client) cookieHeader() string {
	cookies := c.CookieMap()
	parts := make([]string, 0, len(cookies))
	for k, v := range cookies {
		if k != "" && v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, "; ")
}

func (c *Client) apiGet(ctx context.Context, path string, params url.Values, signed bool, out any) error {
	if params == nil {
		params = url.Values{}
	}
	if signed {
		c.ensureWBI(ctx)
		if key := c.mixinKey(); key != "" {
			signParams(params, key, time.Now().Unix())
		}
	}
	u := apiBase + path
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	return c.doJSON(ctx, u, out)
}

func (c *Client) getAbs(ctx context.Context, rawURL string, params url.Values, out any) error {
	if enc := params.Encode(); enc != "" {
		rawURL += "?" + enc
	}
	return c.doJSON(ctx, rawURL, out)
}

func (c *Client) mixinKey() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wbi == nil {
		return ""
	}
	return c.wbi.mixinKey
}

func (c *Client) ensureWBI(ctx context.Context) {
	c.mu.Lock()
	ok := c.wbi != nil
	c.mu.Unlock()
	if ok {
		return
	}
	_, _ = c.Nav(ctx)
}

func (c *Client) doJSON(ctx context.Context, rawURL string, out any) error {
	for attempt := 0; ; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Referer", "https://www.bilibili.com/")
		req.Header.Set("Origin", "https://www.bilibili.com")
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
		if c.explicitCookie {
			if h := c.cookieHeader(); h != "" {
				req.Header.Set("Cookie", h)
			}
		}

		resp, err := c.http.Do(req)
		if err != nil {
			if attempt >= 2 {
				return err
			}
			if sleepErr := sleep(ctx, time.Duration(attempt+1)*time.Second); sleepErr != nil {
				return sleepErr
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if setCookies := resp.Cookies(); len(setCookies) > 0 {
			c.cookiesMu.Lock()
			for _, ck := range setCookies {
				if ck.Name != "" && ck.Value != "" {
					c.lastCookies[ck.Name] = ck.Value
				}
			}
			c.cookiesMu.Unlock()
		}

		switch {
		case resp.StatusCode == http.StatusPreconditionFailed || resp.StatusCode == http.StatusTooManyRequests:
			return fmt.Errorf("%w: http %d", ErrRiskControl, resp.StatusCode)
		case resp.StatusCode >= 500:
			if attempt >= 2 {
				return &APIError{Code: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
			}
			if sleepErr := sleep(ctx, time.Duration(attempt+1)*time.Second); sleepErr != nil {
				return sleepErr
			}
			continue
		case resp.StatusCode != http.StatusOK:
			return &APIError{Code: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
		}

		var env struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			return fmt.Errorf("decode bilibili response: %w", err)
		}
		if env.Code != 0 {
			return codeError(env.Code, env.Message)
		}
		if out != nil && len(env.Data) > 0 && !bytes.Equal(env.Data, []byte("null")) {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return fmt.Errorf("decode bilibili data: %w", err)
			}
		}
		return nil
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
