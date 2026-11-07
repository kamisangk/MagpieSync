package httpapi

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	qrcode "github.com/skip2/go-qrcode"

	"magpiesync/internal/config"
	"magpiesync/internal/download"
	"magpiesync/internal/netproxy"
	"magpiesync/internal/sites"
	"magpiesync/internal/store"
	syncpkg "magpiesync/internal/sync"
	"magpiesync/internal/vault"
	"magpiesync/internal/version"
)

//go:embed web
var webAssets embed.FS

func init() {
	_ = mime.AddExtensionType(".woff2", "font/woff2")
}

type Server struct {
	cfg    config.Config
	store  *store.Store
	vault  *vault.Vault
	engine *syncpkg.Engine
	hub    *Hub

	download        *download.Manager
	settingsMu      sync.Mutex
	scheduleUpdater func(cron.Schedule)

	mu         sync.Mutex
	qrSessions map[string]*qrSession
	auth       *localAuth
}

type qrSession struct {
	session     sites.QRSession
	siteID      string
	url         string
	proxy       string
	accountID   int64
	expectedUID string
	expiresAt   time.Time
}

func New(cfg config.Config, st *store.Store, v *vault.Vault, engine *syncpkg.Engine, hub *Hub) *Server {
	if hub == nil {
		hub = NewHub()
	}
	s := &Server{
		cfg:        cfg,
		store:      st,
		vault:      v,
		engine:     engine,
		hub:        hub,
		qrSessions: make(map[string]*qrSession),
		auth:       newLocalAuth(),
	}
	engine.SetFatalClassifier(sites.IsFatal)
	engine.SetAuthClassifier(sites.IsAuth)
	engine.SetProgress(func(p syncpkg.Progress) {
		event := map[string]any{"type": "progress", "progress": p}
		if p.State != "" && p.State != "running" {
			event["type"] = "run_done"
		}
		hub.Publish(event)
	})
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", s.handleHealth)
	mux.HandleFunc("GET /api/auth/local", s.handleLocalAuthState)
	mux.HandleFunc("POST /api/auth/setup", s.handleLocalSetup)
	mux.HandleFunc("POST /api/auth/login", s.handleLocalLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLocalLogout)
	mux.HandleFunc("POST /api/auth/qrcode", s.handleQRCode)
	mux.HandleFunc("GET /api/auth/qrcode/poll", s.handleQRPoll)
	mux.HandleFunc("POST /api/auth/cookie", s.handleCookieLogin)
	mux.HandleFunc("POST /api/auth/password", s.handlePasswordLogin)
	mux.HandleFunc("GET /api/me", s.handleMe)
	mux.HandleFunc("POST /api/logout", s.handleLocalLogout)
	mux.HandleFunc("GET /api/accounts", s.handleAccounts)
	mux.HandleFunc("POST /api/accounts/{id}/activate", s.handleAccountActivate)
	mux.HandleFunc("PUT /api/accounts/{id}/proxy", s.handleAccountProxy)
	mux.HandleFunc("DELETE /api/accounts/{id}", s.handleAccountDelete)
	mux.HandleFunc("POST /api/proxy/test", s.handleProxyTest)
	mux.HandleFunc("GET /api/overview", s.handleOverview)
	mux.HandleFunc("GET /api/sites", s.handleSites)
	mux.HandleFunc("GET /api/site-assets/{site}/{asset...}", s.handleSiteAsset)
	mux.HandleFunc("GET /api/folders", s.handleFolders)
	mux.HandleFunc("GET /api/sources", s.handleSources)
	mux.HandleFunc("POST /api/sources", s.handleSources)
	mux.HandleFunc("GET /api/collections", s.handleCollections)
	mux.HandleFunc("GET /api/items", s.handleItems)
	mux.HandleFunc("GET /api/items/{id}", s.handleItem)
	mux.HandleFunc("POST /api/sync", s.handleSync)
	mux.HandleFunc("POST /api/sync-all", s.handleSyncAll)
	mux.HandleFunc("GET /api/runs", s.handleRuns)
	mux.HandleFunc("GET /api/runs/{id}", s.handleRun)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/export", s.handleExport)
	mux.HandleFunc("GET /api/settings", s.handleSettings)
	mux.HandleFunc("GET /api/settings/sync-preview", s.handleSyncPreview)
	mux.HandleFunc("PUT /api/settings", s.handleSettings)
	mux.HandleFunc("GET /api/downloads", s.handleDownloads)
	mux.HandleFunc("POST /api/downloads", s.handleEnqueueDownloads)
	mux.HandleFunc("POST /api/downloads/{id}/retry", s.handleRetryDownload)
	mux.HandleFunc("POST /api/downloads/{id}/pause", s.handlePauseDownload)
	mux.HandleFunc("POST /api/downloads/{id}/resume", s.handleResumeDownload)
	mux.HandleFunc("POST /api/downloads/{id}/cancel", s.handleCancelDownload)

	sub, err := fs.Sub(webAssets, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(sub)))
	return s.requireLocalAuth(mux)
}

func (s *Server) requireAccount(w http.ResponseWriter, r *http.Request) (*store.Account, bool) {
	a, err := s.currentAccount(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return nil, false
	}
	return a, true
}

func (s *Server) accountContext(_ context.Context, a *store.Account) (sites.AccountContext, error) {
	plain, err := s.vault.Decrypt(a.Credential, credentialAAD(a.Site, a.RemoteUID))
	if err != nil {
		return sites.AccountContext{}, err
	}
	proxyURL, err := s.proxyForAccount(a)
	if err != nil {
		return sites.AccountContext{}, err
	}
	return sites.AccountContext{
		Account:    a,
		Credential: plain,
		ProxyURL:   proxyURL,
		Rate:       s.cfg.Rate,
		Store:      s.store,
	}, nil
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version.Version})
}

// resolveSiteID fills in a missing site id without hardcoding a default site:
// the active account decides first, and a single-plugin build has only one
// possible answer. Any other case must be explicit.
func (s *Server) resolveSiteID(ctx context.Context, raw string) (string, error) {
	if id := strings.TrimSpace(raw); id != "" {
		return id, nil
	}
	if acct, err := s.currentAccount(ctx); err == nil && acct != nil && acct.Site != "" {
		return acct.Site, nil
	}
	var ready []string
	for _, site := range sites.List() {
		if site.Ready {
			ready = append(ready, site.ID)
		}
	}
	if len(ready) == 1 {
		return ready[0], nil
	}
	return "", errors.New("请指定站点")
}

func (s *Server) handleQRCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Site      string `json:"site"`
		AccountID int64  `json:"account_id"`
		Proxy     string `json:"proxy"`
	}
	if err := readJSON(r, &body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	siteID, err := s.resolveSiteID(r.Context(), body.Site)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, ok := sites.Get(siteID); !ok {
		writeError(w, http.StatusBadRequest, "该站点未编译进当前版本")
		return
	}
	drv, ok := sites.DriverFor(siteID)
	if !ok {
		writeError(w, http.StatusBadRequest, "该站点未编译进当前版本")
		return
	}
	qrDriver, ok := drv.(sites.QRDriver)
	if !ok {
		writeError(w, http.StatusBadRequest, "该站点不支持二维码登录")
		return
	}
	proxyURL, err := netproxy.Normalize(body.Proxy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var account *store.Account
	if body.AccountID > 0 {
		account, err = s.store.GetAccountByID(r.Context(), body.AccountID)
		if err != nil || !accountUsable(account) {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		if account.Site != siteID {
			writeError(w, http.StatusBadRequest, errAccountMismatch.Error())
			return
		}
	}
	start, err := qrDriver.StartQR(r.Context(), proxyURL, s.cfg.Rate)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	png, err := qrcode.Encode(start.URL, qrcode.Medium, 240)
	if err != nil {
		start.Session.Close()
		writeError(w, http.StatusInternalServerError, "failed to render QR code")
		return
	}

	q := &qrSession{
		session:   start.Session,
		siteID:    siteID,
		url:       start.URL,
		proxy:     proxyURL,
		expiresAt: start.ExpiresAt,
	}
	if account != nil {
		q.accountID = account.ID
		q.expectedUID = account.RemoteUID
	}
	s.mu.Lock()
	for k, v := range s.qrSessions {
		if time.Now().After(v.expiresAt) {
			v.session.Close()
			delete(s.qrSessions, k)
		}
	}
	s.qrSessions[start.Key] = q
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"key":        start.Key,
		"url":        start.URL,
		"image":      "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		"expires_at": start.ExpiresAt.Unix(),
	})
}

func (s *Server) handleQRPoll(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	s.mu.Lock()
	sess := s.qrSessions[key]
	if sess != nil && time.Now().After(sess.expiresAt) {
		sess.session.Close()
		delete(s.qrSessions, key)
		sess = nil
	}
	s.mu.Unlock()
	if sess == nil {
		writeJSON(w, http.StatusOK, map[string]any{"state": "expired"})
		return
	}

	state, result, err := sess.session.Poll(r.Context())
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if state != "confirmed" {
		if state == "expired" {
			s.mu.Lock()
			sess.session.Close()
			delete(s.qrSessions, key)
			s.mu.Unlock()
		}
		writeJSON(w, http.StatusOK, map[string]any{"state": state})
		return
	}
	if result == nil {
		s.mu.Lock()
		sess.session.Close()
		delete(s.qrSessions, key)
		s.mu.Unlock()
		writeError(w, http.StatusBadGateway, "登录未返回账户信息")
		return
	}
	if sess.accountID > 0 && result.UID != sess.expectedUID {
		s.mu.Lock()
		sess.session.Close()
		delete(s.qrSessions, key)
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, errAccountMismatch.Error())
		return
	}
	site, _ := sites.Get(sess.siteID)
	a, err := s.saveAccount(r.Context(), site, result, sess.proxy, sess.accountID)
	if err != nil {
		s.mu.Lock()
		sess.session.Close()
		delete(s.qrSessions, key)
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.mu.Lock()
	sess.session.Close()
	delete(s.qrSessions, key)
	s.mu.Unlock()
	if result.AutoSync {
		s.startInitialSync(a.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": "confirmed", "account": a})
}

func (s *Server) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Site      string `json:"site"`
		Email     string `json:"email"`
		Password  string `json:"password"`
		Proxy     string `json:"proxy"`
		AccountID int64  `json:"account_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	siteID := strings.TrimSpace(body.Site)
	if _, ok := sites.Get(siteID); !ok {
		writeError(w, http.StatusBadRequest, "该站点未编译进当前版本")
		return
	}
	drv, ok := sites.DriverFor(siteID)
	if !ok {
		writeError(w, http.StatusBadRequest, "该站点未编译进当前版本")
		return
	}
	pwd, ok := drv.(sites.PasswordDriver)
	if !ok {
		writeError(w, http.StatusBadRequest, "该站点暂不支持账号密码登录")
		return
	}
	email := strings.TrimSpace(body.Email)
	if email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "请输入账号和密码")
		return
	}
	proxyURL, err := netproxy.Normalize(body.Proxy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var account *store.Account
	if body.AccountID > 0 {
		account, err = s.store.GetAccountByID(r.Context(), body.AccountID)
		if err != nil || !accountUsable(account) {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		if account.Site != siteID {
			writeError(w, http.StatusBadRequest, errAccountMismatch.Error())
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := pwd.PasswordLogin(ctx, sites.PasswordInput{Email: email, Password: body.Password, ProxyURL: proxyURL})
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if account != nil && result.UID != account.RemoteUID {
		writeError(w, http.StatusBadRequest, errAccountMismatch.Error())
		return
	}
	accountID := int64(0)
	if account != nil {
		accountID = account.ID
	}
	site, _ := sites.Get(siteID)
	a, err := s.saveAccount(ctx, site, result, proxyURL, accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result.AutoSync {
		s.startInitialSync(a.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": a})
}

func (s *Server) handleCookieLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Site      string `json:"site"`
		Cookie    string `json:"cookie"`
		UserAgent string `json:"user_agent"`
		Proxy     string `json:"proxy"`
		AccountID int64  `json:"account_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	siteID, err := s.resolveSiteID(r.Context(), body.Site)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site, ok := sites.Get(siteID)
	if !ok {
		writeError(w, http.StatusBadRequest, "该站点未编译进当前版本")
		return
	}
	drv, ok := sites.DriverFor(siteID)
	if !ok {
		writeError(w, http.StatusBadRequest, "该站点未编译进当前版本")
		return
	}
	proxyURL, err := netproxy.Normalize(body.Proxy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var account *store.Account
	if body.AccountID > 0 {
		account, err = s.store.GetAccountByID(r.Context(), body.AccountID)
		if err != nil || !accountUsable(account) {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		if account.Site != siteID {
			writeError(w, http.StatusBadRequest, errAccountMismatch.Error())
			return
		}
	}

	result, err := drv.CookieLogin(r.Context(), sites.CookieInput{
		RawCookie: body.Cookie,
		UserAgent: body.UserAgent,
		ProxyURL:  proxyURL,
		Rate:      s.cfg.Rate,
	})
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if account != nil && result.UID != account.RemoteUID {
		writeError(w, http.StatusBadRequest, errAccountMismatch.Error())
		return
	}
	accountID := int64(0)
	if account != nil {
		accountID = account.ID
	}
	a, err := s.saveAccount(r.Context(), site, result, proxyURL, accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result.AutoSync {
		s.startInitialSync(a.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": a})
}

// startInitialSync fills an empty account right after login so the collection
// does not look broken before the user finds the sync button. Existing data is
// never deleted; this is the same read-only "all" sync.
func (s *Server) startInitialSync(accountID int64) {
	if s.engine == nil || accountID <= 0 {
		return
	}
	folders, err := s.store.CountFolders(context.Background(), accountID)
	if err != nil {
		slog.Warn("count folders before initial sync", "account_id", accountID, "error", err)
		return
	}
	if folders > 0 {
		return
	}
	if _, err := s.engine.Start(accountID, "login", "all"); err != nil && !errors.Is(err, syncpkg.ErrAlreadyRunning) {
		slog.Warn("start initial sync", "account_id", accountID, "error", err)
	}
}

func credentialAAD(site, uid string) string {
	return site + ":" + uid
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	a, err := s.currentAccount(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrNoAccount) {
			writeJSON(w, http.StatusOK, map[string]any{"account": nil})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": a})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.store.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	folders, err := s.store.CountFoldersAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, err := s.store.CountItemsAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	last, err := s.store.LastRunAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	localDone, err := s.store.CountDownloadedVideosAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts":   len(accounts),
		"folders":    folders,
		"items":      items,
		"local_done": localDone,
		"last_run":   last,
		"running":    s.engine.AnyRunning(),
	})
}

func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAccount(w, r)
	if !ok {
		return
	}
	folders, err := s.store.ListFolders(r.Context(), a.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if folders == nil {
		folders = []store.Folder{}
	}
	for i := range folders {
		folders[i].Site = a.Site
	}
	if err := s.attachFolderLocal(r.Context(), a.ID, folders); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, err := s.store.CountItems(r.Context(), a.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	localDone, err := s.store.CountDownloadedVideos(r.Context(), a.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"folders": folders,
		"library_summary": map[string]int{
			"items":      items,
			"local_done": localDone,
		},
	})
}

func (s *Server) handleItems(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAccount(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	f := store.ItemFilter{
		State: q.Get("state"),
		Query: strings.TrimSpace(q.Get("q")),
		Sort:  q.Get("sort"),
	}
	f.FolderID, _ = strconv.ParseInt(q.Get("folder_id"), 10, 64)
	f.Page, _ = strconv.Atoi(q.Get("page"))
	f.PageSize, _ = strconv.Atoi(q.Get("page_size"))
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 200 {
		f.PageSize = 60
	}
	items, total, err := s.store.QueryItems(r.Context(), a.ID, f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []store.Item{}
	}
	if err := s.attachItemLocal(r.Context(), a.ID, items); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"total":     total,
		"page":      f.Page,
		"page_size": f.PageSize,
	})
}

func (s *Server) handleItem(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAccount(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	item, err := s.store.GetItem(r.Context(), a.ID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	items := []store.Item{*item}
	if err := s.attachItemLocal(r.Context(), a.ID, items); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	item.Local = items[0].Local
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAccount(w, r)
	if !ok {
		return
	}
	var body struct {
		Scope string `json:"scope"`
	}
	if err := readJSON(r, &body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	scope := strings.TrimSpace(body.Scope)
	if scope == "" {
		scope = "all"
	}
	if scope != "all" && !strings.HasPrefix(scope, "folder:") {
		writeError(w, http.StatusBadRequest, "scope must be all or folder:<remote_id>")
		return
	}
	runID, err := s.engine.Start(a.ID, "manual", scope)
	if err != nil {
		if errors.Is(err, syncpkg.ErrAlreadyRunning) {
			writeError(w, http.StatusConflict, "sync already running")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"run_id": runID})
}

func (s *Server) handleSyncAll(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.store.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(accounts) == 0 {
		writeError(w, http.StatusBadRequest, "请先在视频源添加站点")
		return
	}
	started := 0
	skipped := 0
	var failed []string
	for _, a := range accounts {
		if a.State != "active" {
			skipped++
			continue
		}
		if _, err := s.engine.Start(a.ID, "manual", "all"); err != nil {
			if errors.Is(err, syncpkg.ErrAlreadyRunning) {
				skipped++
				continue
			}
			failed = append(failed, err.Error())
			continue
		}
		started++
	}
	status := http.StatusAccepted
	if started == 0 && len(failed) > 0 {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, map[string]any{"started": started, "skipped": skipped, "errors": failed})
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.store.ListRunsAll(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []store.Run{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid run id")
		return
	}
	run, err := s.store.GetRunAll(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ch, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case b, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.store.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	folders, err := s.store.AllFoldersAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, err := s.store.AllItemsAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	links, err := s.store.AllFolderItemsAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if accounts == nil {
		accounts = []store.Account{}
	}
	if folders == nil {
		folders = []store.Folder{}
	}
	if items == nil {
		items = []store.Item{}
	}
	if links == nil {
		links = []store.ExportFolderItem{}
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	if format == "csv" {
		s.exportCSV(w, folders, items, links, accounts)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="magpiesync-export.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"version":      2,
		"exported_at":  time.Now().Unix(),
		"accounts":     accounts,
		"folders":      folders,
		"items":        items,
		"folder_items": links,
	})
}

func (s *Server) exportCSV(w http.ResponseWriter, folders []store.Folder, items []store.Item, links []store.ExportFolderItem, accounts []store.Account) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="magpiesync-export.csv"`)
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"folder_id", "folder_title", "item_id", "remote_id", "remote_id", "title", "author", "url", "state", "favorited_at", "account_id", "site", "account_name"})

	folderTitles := make(map[int64]string, len(folders))
	folderAccounts := make(map[int64]int64, len(folders))
	for _, f := range folders {
		folderTitles[f.ID] = f.Title
		folderAccounts[f.ID] = f.AccountID
	}
	accountNames := make(map[int64]string, len(accounts))
	accountSites := make(map[int64]string, len(accounts))
	for _, a := range accounts {
		accountNames[a.ID] = a.Name
		accountSites[a.ID] = a.Site
	}
	itemByID := make(map[int64]store.Item, len(items))
	for _, it := range items {
		itemByID[it.ID] = it
	}
	for _, l := range links {
		it, ok := itemByID[l.ItemID]
		if !ok {
			continue
		}
		accountID := folderAccounts[l.FolderID]
		_ = cw.Write([]string{
			strconv.FormatInt(l.FolderID, 10),
			folderTitles[l.FolderID],
			strconv.FormatInt(it.ID, 10),
			it.RemoteID,
			it.RemoteID,
			it.Title,
			it.AuthorName,
			it.URL,
			l.State,
			strconv.FormatInt(l.FavoritedAt, 10),
			strconv.FormatInt(accountID, 10),
			accountSites[accountID],
			accountNames[accountID],
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	return dec.Decode(v)
}
