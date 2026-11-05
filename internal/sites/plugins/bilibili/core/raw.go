//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// rawGet performs a GET against api.bilibili.com and returns the raw body.
// It is used for binary danmaku segments, whose response has no JSON envelope.
func (c *Client) rawGet(ctx context.Context, path string, params url.Values, signed bool) ([]byte, string, error) {
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
	return c.doRaw(ctx, u)
}

// rawGetURL is for absolute CDN/subtitle URLs that do not use the API envelope.
func (c *Client) rawGetURL(ctx context.Context, rawURL string, params url.Values) ([]byte, string, error) {
	if enc := params.Encode(); enc != "" {
		rawURL += "?" + enc
	}
	return c.doRaw(ctx, rawURL)
}

func (c *Client) doRaw(ctx context.Context, rawURL string) ([]byte, string, error) {
	for attempt := 0; ; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Referer", "https://www.bilibili.com/")
		req.Header.Set("Origin", "https://www.bilibili.com")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
		if c.explicitCookie {
			if h := c.cookieHeader(); h != "" {
				req.Header.Set("Cookie", h)
			}
		}

		resp, err := c.http.Do(req)
		if err != nil {
			if attempt >= 2 {
				return nil, "", err
			}
			if sleepErr := sleep(ctx, time.Duration(attempt+1)*time.Second); sleepErr != nil {
				return nil, "", sleepErr
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		contentType := resp.Header.Get("Content-Type")
		resp.Body.Close()
		if readErr != nil {
			return nil, contentType, readErr
		}
		if resp.StatusCode == http.StatusPreconditionFailed || resp.StatusCode == http.StatusTooManyRequests {
			return nil, contentType, fmt.Errorf("%w: http %d", ErrRiskControl, resp.StatusCode)
		}
		if resp.StatusCode >= 500 {
			if attempt >= 2 {
				return nil, contentType, &APIError{Code: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
			}
			if sleepErr := sleep(ctx, time.Duration(attempt+1)*time.Second); sleepErr != nil {
				return nil, contentType, sleepErr
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, contentType, &APIError{Code: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
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
		return body, contentType, nil
	}
}

// decodeAPIError recognizes a JSON API error returned where binary/JSON data
// was expected. It returns nil when the body is not a bilibili error envelope.
func decodeAPIError(body []byte) error {
	if len(bytes.TrimSpace(body)) == 0 || bytes.TrimSpace(body)[0] != '{' {
		return nil
	}
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil
	}
	if env.Code == 0 {
		return nil
	}
	return codeError(env.Code, env.Message)
}

func parseContentRangeTotal(v string) int64 {
	// Content-Range: bytes 0-0/12345
	slash := -1
	for i := len(v) - 1; i >= 0; i-- {
		if v[i] == '/' {
			slash = i
			break
		}
	}
	if slash < 0 || slash+1 >= len(v) {
		return 0
	}
	n, _ := strconv.ParseInt(v[slash+1:], 10, 64)
	return n
}
