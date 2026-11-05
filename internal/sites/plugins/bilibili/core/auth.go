//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"magpiesync/internal/cookieutil"
)

type NavInfo struct {
	Mid     int64  `json:"mid"`
	Uname   string `json:"uname"`
	Face    string `json:"face"`
	IsLogin bool   `json:"is_login"`
}

type navData struct {
	IsLogin bool   `json:"isLogin"`
	Mid     int64  `json:"mid"`
	Uname   string `json:"uname"`
	Face    string `json:"face"`
	WBIImg  struct {
		ImgURL string `json:"img_url"`
		SubURL string `json:"sub_url"`
	} `json:"wbi_img"`
}

func (c *Client) Nav(ctx context.Context) (*NavInfo, error) {
	var d navData
	if err := c.apiGet(ctx, "/x/web-interface/nav", nil, false, &d); err != nil {
		return nil, err
	}
	if key := parseWBIKeys(d.WBIImg.ImgURL, d.WBIImg.SubURL); key != nil {
		c.mu.Lock()
		c.wbi = key
		c.mu.Unlock()
	}
	info := &NavInfo{Mid: d.Mid, Uname: d.Uname, Face: d.Face, IsLogin: d.IsLogin}
	if !d.IsLogin {
		return info, ErrAuth
	}
	return info, nil
}

type QRSession struct {
	Key       string `json:"key"`
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at"`
}

func (c *Client) QRGenerate(ctx context.Context) (*QRSession, error) {
	var d struct {
		URL       string `json:"url"`
		QrcodeKey string `json:"qrcode_key"`
	}
	if err := c.getAbs(ctx, passportBase+"/x/passport-login/web/qrcode/generate", nil, &d); err != nil {
		return nil, err
	}
	if d.URL == "" || d.QrcodeKey == "" {
		return nil, errors.New("bilibili: empty qrcode response")
	}
	return &QRSession{Key: d.QrcodeKey, URL: d.URL, ExpiresAt: time.Now().Add(180 * time.Second).Unix()}, nil
}

// QRPoll returns one of: pending, scanned, confirmed, expired.
func (c *Client) QRPoll(ctx context.Context, key string) (string, error) {
	var d struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	params := url.Values{"qrcode_key": {key}}
	if err := c.getAbs(ctx, passportBase+"/x/passport-login/web/qrcode/poll", params, &d); err != nil {
		return "", err
	}
	switch d.Code {
	case 0:
		return "confirmed", nil
	case 86101:
		return "pending", nil
	case 86090:
		return "scanned", nil
	case 86038:
		return "expired", nil
	default:
		return "pending", nil
	}
}

type Credential struct {
	Cookies map[string]string `json:"cookies"`
}

func ParseCredential(raw []byte) (*Credential, error) {
	var c Credential
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse credential: %w", err)
	}
	if c.Cookies == nil || c.Cookies["SESSDATA"] == "" {
		return nil, errors.New("credential is missing SESSDATA")
	}
	return &c, nil
}

func ParseCookieString(s string) map[string]string {
	return cookieutil.Parse(s)
}
