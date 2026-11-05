//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bilibili

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"magpiesync/internal/sites"
	"magpiesync/internal/sites/plugins/bilibili/core"
)

func (driver) CookieLogin(ctx context.Context, in sites.CookieInput) (*sites.LoginResult, error) {
	cookies := bili.ParseCookieString(in.RawCookie)
	if cookies["SESSDATA"] == "" {
		return nil, &sites.AuthError{Kind: sites.AuthBadRequest, Message: "SESSDATA is required"}
	}
	client, err := bili.NewWithCookiesWithProxy(cookies, in.Rate, in.ProxyURL)
	if err != nil {
		return nil, &sites.AuthError{Kind: sites.AuthBadRequest, Message: err.Error(), Err: err}
	}
	nav, err := client.Nav(ctx)
	if err != nil {
		slog.Warn("cookie profile check failed", "site", "bilibili", "cookies", cookieNames(cookies), "error", err)
		if bili.IsAuth(err) {
			return nil, &sites.AuthError{Kind: sites.AuthUnauthorized, Message: "cookie is invalid or expired", Err: err}
		}
		return nil, &sites.AuthError{Kind: sites.AuthBadGateway, Message: err.Error(), Err: err}
	}
	raw, err := json.Marshal(bili.Credential{Cookies: client.CookieMap()})
	if err != nil {
		return nil, err
	}
	return &sites.LoginResult{
		UID:        strconv.FormatInt(nav.Mid, 10),
		Name:       nav.Uname,
		AvatarURL:  nav.Face,
		Credential: raw,
	}, nil
}

func (driver) StartQR(ctx context.Context, proxyURL string, rate float64) (*sites.QRStart, error) {
	client, err := bili.NewGuestWithProxy(proxyURL)
	if err != nil {
		return nil, &sites.AuthError{Kind: sites.AuthBadGateway, Message: err.Error(), Err: err}
	}
	client.EnsureBuvid(ctx)
	sess, err := client.QRGenerate(ctx)
	if err != nil {
		return nil, &sites.AuthError{Kind: sites.AuthBadGateway, Message: "failed to create QR code: " + err.Error(), Err: err}
	}
	return &sites.QRStart{
		Key:       sess.Key,
		URL:       sess.URL,
		ExpiresAt: time.Unix(sess.ExpiresAt, 0),
		Session:   &qrSession{client: client, key: sess.Key, proxy: proxyURL, rate: rate},
	}, nil
}

type qrSession struct {
	client *bili.Client
	key    string
	proxy  string
	rate   float64
}

func (q *qrSession) Poll(ctx context.Context) (string, *sites.LoginResult, error) {
	state, err := q.client.QRPoll(ctx, q.key)
	if err != nil {
		return "", nil, &sites.AuthError{Kind: sites.AuthBadGateway, Message: "failed to poll QR code: " + err.Error(), Err: err}
	}
	if state != "confirmed" {
		return state, nil, nil
	}

	cookies := q.client.CookieMap()
	if cookies["SESSDATA"] == "" {
		return "", nil, &sites.AuthError{Kind: sites.AuthBadGateway, Message: "login succeeded but SESSDATA is missing"}
	}
	q.client.EnsureBuvid(ctx)
	cookies = q.client.CookieMap()

	// Cookies from the passport host are not always visible to
	// api.bilibili.com (host-only cookies). Re-seed them into a fresh client
	// for the profile check.
	attempts := []struct {
		name    string
		cookies map[string]string
	}{
		{"all", cookies},
		{"session-only", selectCookies(cookies, "SESSDATA", "bili_jct", "DedeUserID", "DedeUserID__ckMd5", "sid")},
		{"sessdata-only", selectCookies(cookies, "SESSDATA")},
	}
	var nav *bili.NavInfo
	var used map[string]string
	var lastErr error
	for _, attempt := range attempts {
		if len(attempt.cookies) == 0 {
			continue
		}
		authClient, err := bili.NewWithCookiesWithProxy(attempt.cookies, q.rate, q.proxy)
		if err != nil {
			lastErr = err
			break
		}
		n, err := authClient.Nav(ctx)
		if err == nil {
			nav = n
			used = authClient.CookieMap()
			break
		}
		lastErr = err
		slog.Warn("qrcode profile check failed",
			"attempt", attempt.name,
			"cookies", cookieNames(attempt.cookies),
			"sessdata_len", len(attempt.cookies["SESSDATA"]),
			"error", err)
	}
	if nav == nil {
		return "", nil, &sites.AuthError{
			Kind:    sites.AuthUnauthorized,
			Message: "login succeeded but profile check failed: " + lastErr.Error(),
			Err:     lastErr,
		}
	}
	raw, err := json.Marshal(bili.Credential{Cookies: used})
	if err != nil {
		return "", nil, err
	}
	return "confirmed", &sites.LoginResult{
		UID:        strconv.FormatInt(nav.Mid, 10),
		Name:       nav.Uname,
		AvatarURL:  nav.Face,
		Credential: raw,
	}, nil
}

func (q *qrSession) Close() error { return nil }

func selectCookies(src map[string]string, names ...string) map[string]string {
	out := make(map[string]string, len(names))
	for _, name := range names {
		if v := src[name]; v != "" {
			out[name] = v
		}
	}
	return out
}

func cookieNames(cookies map[string]string) []string {
	names := make([]string, 0, len(cookies))
	for name := range cookies {
		names = append(names, name)
	}
	return names
}
