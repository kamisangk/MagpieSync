package netproxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// Normalize accepts an optional HTTP(S) or SOCKS5 proxy URL and returns a
// canonical form. An empty string means a direct connection.
func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("代理地址无效")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	switch u.Scheme {
	case "http", "https":
	case "socks5", "socks5h":
		if u.Port() == "" {
			u.Host = net.JoinHostPort(u.Hostname(), "1080")
		}
	default:
		return "", fmt.Errorf("不支持的代理协议，请使用 http:// 或 socks5://")
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("代理地址缺少主机名")
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("代理地址不能包含路径")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("代理地址不能包含查询参数或片段")
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// Transport builds an HTTP transport for the configured proxy. A nil transport
// means the caller should use Go's default transport.
func Transport(raw string) (*http.Transport, error) {
	canonical, err := Normalize(raw)
	if err != nil {
		return nil, err
	}
	if canonical == "" {
		return nil, nil
	}
	u, _ := url.Parse(canonical)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}

	switch u.Scheme {
	case "http", "https":
		tr.Proxy = http.ProxyURL(u)
	case "socks5", "socks5h":
		var auth *xproxy.Auth
		if u.User != nil {
			password, _ := u.User.Password()
			auth = &xproxy.Auth{User: u.User.Username(), Password: password}
		}
		dialer, err := xproxy.SOCKS5("tcp", u.Host, auth, xproxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("创建 SOCKS5 代理失败: %w", err)
		}
		ctxDialer, ok := dialer.(xproxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("SOCKS5 代理不支持上下文取消")
		}
		tr.Proxy = nil
		tr.DialContext = ctxDialer.DialContext
	}
	return tr, nil
}

// NewClient builds an HTTP client that routes requests through the proxy.
func NewClient(raw string, timeout time.Duration) (*http.Client, error) {
	tr, err := Transport(raw)
	if err != nil {
		return nil, err
	}
	if tr == nil {
		tr = http.DefaultTransport.(*http.Transport).Clone()
	}
	tr.MaxIdleConns = 64
	tr.MaxIdleConnsPerHost = 16
	tr.IdleConnTimeout = 90 * time.Second
	tr.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: tr, Timeout: timeout}, nil
}

// Test verifies that the proxy can reach a public HTTPS endpoint.
func Test(ctx context.Context, raw string) error {
	client, err := NewClient(raw, 15*time.Second)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MagpieSync proxy test")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusProxyAuthRequired {
		return fmt.Errorf("代理认证失败 (HTTP 407)")
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("代理已连接，但目标站点返回 HTTP %d", resp.StatusCode)
	}
	return nil
}
