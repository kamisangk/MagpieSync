package httpapi

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	localPasswordSetting = "auth.password"
	sessionCookieName    = "magpiesync_session"
	sessionMaxAge        = 30 * 24 * time.Hour
	passwordIterations   = 310_000
	passwordSaltBytes    = 16
	passwordKeyBytes     = 32
	minPasswordLength    = 8
	maxPasswordLength    = 128
)

var errInvalidPassword = errors.New("invalid password")

type localAuth struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}

func newLocalAuth() *localAuth {
	return &localAuth{sessions: make(map[string]time.Time)}
}

func (a *localAuth) create() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	now := time.Now()
	a.mu.Lock()
	for k, expiry := range a.sessions {
		if now.After(expiry) {
			delete(a.sessions, k)
		}
	}
	a.sessions[token] = now.Add(sessionMaxAge)
	a.mu.Unlock()
	return token, nil
}

func (a *localAuth) valid(token string) bool {
	if token == "" {
		return false
	}
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	expiry, ok := a.sessions[token]
	if !ok {
		return false
	}
	if now.After(expiry) {
		delete(a.sessions, token)
		return false
	}
	return true
}

func (a *localAuth) destroy(token string) {
	a.mu.Lock()
	delete(a.sessions, token)
	a.mu.Unlock()
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyBytes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return errInvalidPassword
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 100_000 || iter > 1_000_000 {
		return errInvalidPassword
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return errInvalidPassword
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return errInvalidPassword
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return errInvalidPassword
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return errInvalidPassword
	}
	return nil
}

func (s *Server) localPasswordConfigured(ctx context.Context) (bool, error) {
	_, ok, err := s.store.GetSetting(ctx, localPasswordSetting)
	return ok, err
}

func (s *Server) localSessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func (s *Server) localAuthenticated(r *http.Request) bool {
	return s.auth.valid(s.localSessionToken(r))
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionMaxAge / time.Second),
		Expires:  time.Now().Add(sessionMaxAge),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

func (s *Server) handleLocalAuthState(w http.ResponseWriter, r *http.Request) {
	configured, err := s.localPasswordConfigured(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	authenticated := configured && s.localAuthenticated(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"configured":    configured,
		"authenticated": authenticated,
	})
}

func (s *Server) handleLocalSetup(w http.ResponseWriter, r *http.Request) {
	configured, err := s.localPasswordConfigured(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if configured {
		writeError(w, http.StatusConflict, "本地密码已经设置")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.Password) < minPasswordLength {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("密码至少需要 %d 个字符", minPasswordLength))
		return
	}
	if len(body.Password) > maxPasswordLength {
		writeError(w, http.StatusBadRequest, "密码过长")
		return
	}
	encoded, err := hashPassword(body.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法保存密码")
		return
	}
	if err := s.store.SetSettings(r.Context(), map[string]string{localPasswordSetting: encoded}, time.Now().Unix()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	token, err := s.auth.create()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建会话")
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLocalLogin(w http.ResponseWriter, r *http.Request) {
	encoded, configured, err := s.store.GetSetting(r.Context(), localPasswordSetting)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !configured {
		writeError(w, http.StatusConflict, "尚未设置本地密码")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if verifyPassword(body.Password, encoded) != nil {
		writeError(w, http.StatusUnauthorized, "密码错误")
		return
	}
	token, err := s.auth.create()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建会话")
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLocalLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.destroy(s.localSessionToken(r))
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) requireLocalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if isPublicAPIPath(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !s.localAuthenticated(r) {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isPublicAPIPath(r *http.Request) bool {
	switch r.URL.Path {
	case "/api/healthz":
		return true
	case "/api/auth/local":
		return r.Method == http.MethodGet
	case "/api/auth/setup", "/api/auth/login", "/api/auth/logout":
		return r.Method == http.MethodPost
	}
	return false
}
