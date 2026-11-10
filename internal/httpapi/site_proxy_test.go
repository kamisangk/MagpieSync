package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"magpiesync/internal/siteproxy"
	"magpiesync/internal/sites"
	"magpiesync/internal/store"
	syncpkg "magpiesync/internal/sync"
	"magpiesync/internal/vault"
)

const proxyTestSite = "synthetic-platform-proxy"
const proxyOtherSite = "synthetic-platform-other"

type proxyTestDriver struct {
	proxy string
	calls int
}

func (*proxyTestDriver) SiteID() string     { return proxyTestSite }
func (*proxyTestDriver) IsAuth(error) bool  { return false }
func (*proxyTestDriver) IsFatal(error) bool { return false }
func (*proxyTestDriver) NewSource(context.Context, sites.AccountContext) (syncpkg.Source, error) {
	return nil, nil
}
func (d *proxyTestDriver) CookieLogin(_ context.Context, in sites.CookieInput) (*sites.LoginResult, error) {
	d.proxy = in.ProxyURL
	d.calls++
	return &sites.LoginResult{UID: "login", Name: "Login", Credential: []byte("secret-credential")}, nil
}
func (d *proxyTestDriver) PasswordLogin(_ context.Context, in sites.PasswordInput) (*sites.LoginResult, error) {
	return d.CookieLogin(context.Background(), sites.CookieInput{ProxyURL: in.ProxyURL})
}
func (d *proxyTestDriver) StartQR(_ context.Context, proxy string, _ float64) (*sites.QRStart, error) {
	d.proxy = proxy
	d.calls++
	return &sites.QRStart{Key: "proxy-qr", URL: "https://example.com/login", ExpiresAt: time.Now().Add(time.Minute), Session: proxyTestQR{}}, nil
}

type proxyTestQR struct{}

func (proxyTestQR) Poll(context.Context) (string, *sites.LoginResult, error) {
	return "confirmed", &sites.LoginResult{UID: "login", Name: "Login", Credential: []byte("secret-credential")}, nil
}
func (proxyTestQR) Close() error { return nil }

var proxyTestRegistration sync.Once
var proxyDriver = &proxyTestDriver{}

func proxyServer(t *testing.T) *Server {
	t.Helper()
	proxyTestRegistration.Do(func() {
		sites.Register(sites.Site{ID: proxyTestSite, Name: "Proxy test", Ready: true})
		sites.Register(sites.Site{ID: proxyOtherSite, Name: "Proxy other", Ready: true})
		sites.RegisterDriver(proxyTestSite, proxyDriver)
	})
	st, err := store.Open(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	v, err := vault.New("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Server{store: st, vault: v, qrSessions: make(map[string]*qrSession)}
}

func proxySettings(t *testing.T, s *Server, method string, value any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleSettings(w, httptest.NewRequest(method, "/api/settings", strings.NewReader(string(b))))
	return w
}

func TestSiteProxySettingsAtomicAndAccountIndependent(t *testing.T) {
	s := proxyServer(t)
	ctx := context.Background()
	const first = "http://test-user:test-secret@127.0.0.1:8123"
	const second = "socks5://127.0.0.1:1080"
	if w := proxySettings(t, s, "PUT", map[string]any{"site_proxies": map[string]string{proxyTestSite: first, proxyOtherSite: second}, "concurrency": 3}); w.Code != 200 {
		t.Fatalf("save without accounts: %d %s", w.Code, w.Body.String())
	}
	stored, found, err := s.store.GetSetting(ctx, siteproxy.Key(proxyTestSite))
	if err != nil || !found || stored == first || strings.Contains(stored, "test-secret") {
		t.Fatalf("not encrypted: %v", err)
	}
	w := proxySettings(t, s, "GET", nil)
	var response struct {
		Settings struct {
			SiteProxies map[string]string `json:"site_proxies"`
			Concurrency int               `json:"concurrency"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || response.Settings.SiteProxies[proxyTestSite] != first || response.Settings.SiteProxies[proxyOtherSite] != second || response.Settings.Concurrency != 3 {
		t.Fatalf("round trip: %s", w.Body.String())
	}
	before, err := s.store.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, proxies := range []map[string]string{
		{proxyTestSite: "http://test-user:test-secret@host:bad"},
		{proxyTestSite: "http://127.0.0.1:9999", "unknown-proxy-site": second},
	} {
		w := proxySettings(t, s, "PUT", map[string]any{"site_proxies": proxies, "concurrency": 7, "sync_schedule": "0 5 * * *"})
		if w.Code != 400 || strings.Contains(w.Body.String(), "test-secret") {
			t.Fatalf("invalid result: %d %s", w.Code, w.Body.String())
		}
		after, err := s.store.Settings(ctx)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("invalid proxy partially saved settings: %v", err)
		}
	}
	if w := proxySettings(t, s, "PUT", map[string]any{"concurrency": 4}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if got, err := s.proxyForSite(ctx, proxyTestSite); err != nil || got != first {
		t.Fatalf("omitted proxy changed: %q %v", got, err)
	}
	if w := proxySettings(t, s, "PUT", map[string]any{"site_proxies": map[string]string{proxyTestSite: ""}}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if got, err := s.proxyForSite(ctx, proxyTestSite); err != nil || got != "" {
		t.Fatalf("clear failed: %q %v", got, err)
	}
	if got, err := s.proxyForSite(ctx, proxyOtherSite); err != nil || got != second {
		t.Fatalf("clear crossed sites: %q %v", got, err)
	}
}

func TestLoginsAndAccountsUseSavedSiteProxy(t *testing.T) {
	s := proxyServer(t)
	const proxy = "http://test-user:test-secret@127.0.0.1:8123"
	if w := proxySettings(t, s, "PUT", map[string]any{"site_proxies": map[string]string{proxyTestSite: proxy}}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, handler := range []struct {
		name   string
		handle http.HandlerFunc
	}{{"cookie", s.handleCookieLogin}, {"password", s.handlePasswordLogin}, {"qrcode", s.handleQRCode}} {
		t.Run(handler.name, func(t *testing.T) {
			for _, override := range []string{"http://override-secret@127.0.0.1:9999", ""} {
				before := proxyDriver.calls
				body, _ := json.Marshal(map[string]any{"site": proxyTestSite, "email": "name", "password": "password", "cookie": "cookie", "proxy": override})
				w := httptest.NewRecorder()
				handler.handle(w, httptest.NewRequest("POST", "/api/auth/"+handler.name, strings.NewReader(string(body))))
				if override != "" {
					if w.Code != 400 || proxyDriver.calls != before || strings.Contains(w.Body.String(), "override-secret") {
						t.Fatalf("override accepted: %d %s", w.Code, w.Body.String())
					}
					continue
				}
				if w.Code != 200 || proxyDriver.proxy != proxy || proxyDriver.calls != before+1 {
					t.Fatalf("saved proxy unused: %d %s; proxy=%q", w.Code, w.Body.String(), proxyDriver.proxy)
				}
				if handler.name == "qrcode" {
					w = httptest.NewRecorder()
					s.handleQRPoll(w, httptest.NewRequest("GET", "/api/auth/qrcode/poll?key=proxy-qr", nil))
					if w.Code != 200 {
						t.Fatal(w.Body.String())
					}
				}
			}
		})
	}
	ctx := context.Background()
	for _, uid := range []string{"login", "second"} {
		a, err := s.saveAccount(ctx, sites.Site{ID: proxyTestSite}, &sites.LoginResult{UID: uid, Credential: []byte("secret-credential")}, 0)
		if err != nil {
			t.Fatal(err)
		}
		in, err := s.accountContext(ctx, a)
		if err != nil || in.ProxyURL != proxy {
			t.Fatalf("account %s proxy: %q %v", uid, in.ProxyURL, err)
		}
	}
	stored, _, _ := s.store.GetSetting(ctx, siteproxy.Key(proxyTestSite))
	for _, endpoint := range []struct {
		path   string
		handle http.HandlerFunc
	}{
		{"/api/accounts", s.handleAccounts}, {"/api/sites", s.handleSites}, {"/api/export", s.handleExport}, {"/api/export?format=csv", s.handleExport},
	} {
		w := httptest.NewRecorder()
		endpoint.handle(w, httptest.NewRequest("GET", endpoint.path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", endpoint.path, w.Code, w.Body.String())
		}
		for _, secret := range []string{"test-secret", "secret-credential", stored, "has_proxy", "proxy_enc"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatalf("%s exposed %s", endpoint.path, fmt.Sprintf("%q", secret))
			}
		}
	}
	if w := proxySettings(t, s, "PUT", map[string]any{"site_proxies": map[string]string{proxyTestSite: ""}}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range accounts {
		in, err := s.accountContext(ctx, &a)
		if err != nil || in.ProxyURL != "" {
			t.Fatalf("new client retained cleared proxy: %q %v", in.ProxyURL, err)
		}
	}
}

func TestSiteProxyDatabaseFailureRollsBackAllSettings(t *testing.T) {
	s := proxyServer(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rollback.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s.store = st
	if w := proxySettings(t, s, "PUT", map[string]any{"site_proxies": map[string]string{proxyTestSite: "http://127.0.0.1:8123"}, "concurrency": 3}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	before, err := st.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER reject_proxy BEFORE UPDATE ON settings WHEN NEW.key LIKE 'network.proxy.%' BEGIN SELECT RAISE(ABORT, 'test write failure'); END`); err != nil {
		t.Fatal(err)
	}
	w := proxySettings(t, s, "PUT", map[string]any{"site_proxies": map[string]string{proxyTestSite: "http://127.0.0.1:9999"}, "concurrency": 6})
	if w.Code != 500 {
		t.Fatalf("write failure = %d %s", w.Code, w.Body.String())
	}
	after, err := st.Settings(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed write partially changed settings: %v", err)
	}
}
