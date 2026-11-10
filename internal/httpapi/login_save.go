package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"magpiesync/internal/sites"
	"magpiesync/internal/store"
)

// writeAuthError maps a driver authentication error to an HTTP response.
func writeAuthError(w http.ResponseWriter, err error) {
	var authErr *sites.AuthError
	if errors.As(err, &authErr) {
		switch authErr.Kind {
		case sites.AuthBadRequest:
			writeError(w, http.StatusBadRequest, authErr.Error())
		case sites.AuthUnauthorized:
			writeError(w, http.StatusUnauthorized, authErr.Error())
		case sites.AuthUnavailable:
			writeError(w, http.StatusServiceUnavailable, authErr.Error())
		case sites.AuthTimeout:
			writeError(w, http.StatusGatewayTimeout, authErr.Error())
		default:
			writeError(w, http.StatusBadGateway, authErr.Error())
		}
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "登录超时，请重试")
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

// saveAccount persists a site-neutral login result and owns credential
// encryption, identity migration and the active account.
func (s *Server) saveAccount(ctx context.Context, site sites.Site, result *sites.LoginResult, accountID int64) (*store.Account, error) {
	if result == nil {
		return nil, errors.New("登录未返回账户信息")
	}
	uid := strings.TrimSpace(result.UID)
	name := strings.TrimSpace(result.Name)
	if uid == "" {
		return nil, fmt.Errorf("%s 未返回数字账户 ID", site.Name)
	}
	if name == "" {
		name = uid
	}
	enc, err := s.vault.Encrypt(result.Credential, credentialAAD(site.ID, uid))
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if accountID > 0 {
		existing, err := s.store.GetAccountByID(ctx, accountID)
		if err != nil {
			return nil, err
		}
		if existing.Site != site.ID {
			return nil, fmt.Errorf("该账户不是 %s 账户", site.Name)
		}
		if existing.RemoteUID != uid {
			return nil, errAccountMismatch
		}
		if err := s.store.UpdateAccountLogin(ctx, accountID, name, result.AvatarURL, enc, now); err != nil {
			return nil, err
		}
		if err := s.setActiveAccount(ctx, accountID); err != nil {
			return nil, err
		}
		return s.store.GetAccountByID(ctx, accountID)
	}

	id, err := s.store.UpsertAccount(ctx, site.ID, uid, name, result.AvatarURL, enc, now)
	if err != nil {
		return nil, err
	}
	if err := s.setActiveAccount(ctx, id); err != nil {
		return nil, err
	}
	return s.store.GetAccountByID(ctx, id)
}
