package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"magpiesync/internal/netproxy"
	"magpiesync/internal/store"
)

const activeAccountSetting = "active.account_id"

func accountUsable(a *store.Account) bool {
	if a == nil {
		return false
	}
	if a.State != "active" && a.State != "needs_reauth" {
		return false
	}
	return len(a.Credential) > 0
}

func (s *Server) currentAccount(ctx context.Context) (*store.Account, error) {
	if raw, ok, err := s.store.GetSetting(ctx, activeAccountSetting); err == nil && ok {
		if id, convErr := strconv.ParseInt(raw, 10, 64); convErr == nil && id > 0 {
			if a, getErr := s.store.GetAccountByID(ctx, id); getErr == nil && accountUsable(a) {
				return a, nil
			}
		}
	}
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, store.ErrNoAccount
	}
	return &accounts[0], nil
}

func (s *Server) setActiveAccount(ctx context.Context, id int64) error {
	return s.store.SetSettings(ctx, map[string]string{activeAccountSetting: strconv.FormatInt(id, 10)}, time.Now().Unix())
}

func proxyAAD(site, uid string) string {
	return "proxy:" + site + ":" + uid
}

func (s *Server) proxyForAccount(a *store.Account) (string, error) {
	if a == nil || len(a.ProxyEnc) == 0 {
		return "", nil
	}
	plain, err := s.vault.Decrypt(a.ProxyEnc, proxyAAD(a.Site, a.RemoteUID))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *Server) encryptProxy(site, uid, raw string) ([]byte, error) {
	canonical, err := netproxy.Normalize(raw)
	if err != nil {
		return nil, err
	}
	if canonical == "" {
		return nil, nil
	}
	return s.vault.Encrypt([]byte(canonical), proxyAAD(site, uid))
}

type accountDTO struct {
	ID       int64  `json:"id"`
	Site     string `json:"site"`
	UID      string `json:"remote_uid"`
	Name     string `json:"name"`
	Avatar   string `json:"avatar_url,omitempty"`
	State    string `json:"state"`
	Current  bool   `json:"current"`
	Proxy    string `json:"proxy,omitempty"`
	HasProxy bool   `json:"has_proxy"`
}

func (s *Server) accountDTO(ctx context.Context, a *store.Account, currentID int64) accountDTO {
	proxy, err := s.proxyForAccount(a)
	if err != nil {
		proxy = ""
	}
	return accountDTO{
		ID:       a.ID,
		Site:     a.Site,
		UID:      a.RemoteUID,
		Name:     a.Name,
		Avatar:   a.AvatarURL,
		State:    a.State,
		Current:  a.ID == currentID,
		Proxy:    proxy,
		HasProxy: proxy != "",
	}
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.store.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	current, _ := s.currentAccount(r.Context())
	var currentID int64
	if current != nil {
		currentID = current.ID
	}
	out := make([]accountDTO, 0, len(accounts))
	for i := range accounts {
		out = append(out, s.accountDTO(r.Context(), &accounts[i], currentID))
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

func (s *Server) handleAccountActivate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return
	}
	a, err := s.store.GetAccountByID(r.Context(), id)
	if err != nil || !accountUsable(a) {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err := s.setActiveAccount(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": s.accountDTO(r.Context(), a, id)})
}

func (s *Server) handleAccountDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return
	}
	account, err := s.store.GetAccountByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if s.engine != nil && s.engine.IsRunning(id) {
		writeError(w, http.StatusConflict, "该账户正在同步，请等待同步结束后再删除")
		return
	}
	wasCurrent := false
	if current, currentErr := s.currentAccount(r.Context()); currentErr == nil && current != nil && current.ID == id {
		wasCurrent = true
	}
	if err := s.store.DeleteAccount(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNoAccount) {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if wasCurrent {
		accounts, listErr := s.store.ListAccounts(r.Context())
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, listErr.Error())
			return
		}
		if len(accounts) > 0 {
			if err := s.setActiveAccount(r.Context(), accounts[0].ID); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		} else if err := s.store.SetSettings(r.Context(), map[string]string{activeAccountSetting: ""}, time.Now().Unix()); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": account.ID})
}

func (s *Server) handleAccountProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return
	}
	a, err := s.store.GetAccountByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	var body struct {
		Proxy string `json:"proxy"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	enc, err := s.encryptProxy(a.Site, a.RemoteUID, body.Proxy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateAccountProxy(r.Context(), a.ID, enc, time.Now().Unix()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.ProxyEnc = enc
	current, _ := s.currentAccount(r.Context())
	var currentID int64
	if current != nil {
		currentID = current.ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": s.accountDTO(r.Context(), a, currentID)})
}

func (s *Server) handleProxyTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Proxy string `json:"proxy"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	canonical, err := netproxy.Normalize(body.Proxy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if canonical == "" {
		writeError(w, http.StatusBadRequest, "请先填写代理地址")
		return
	}
	if err := netproxy.Test(r.Context(), canonical); err != nil {
		writeError(w, http.StatusBadGateway, "代理不可用："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "proxy": canonical})
}

var errAccountMismatch = errors.New("登录的账号与当前账户不一致")
