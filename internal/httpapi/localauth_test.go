package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	encoded, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(encoded, "pbkdf2-sha256$") {
		t.Fatalf("unexpected format: %s", encoded)
	}
	if err := verifyPassword("correct horse battery staple", encoded); err != nil {
		t.Fatalf("verify valid password: %v", err)
	}
	if err := verifyPassword("wrong password", encoded); err == nil {
		t.Fatal("wrong password accepted")
	}
	if err := verifyPassword("", "malformed"); err == nil {
		t.Fatal("malformed hash accepted")
	}
}

func TestLocalSessions(t *testing.T) {
	auth := newLocalAuth()
	token, err := auth.create()
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if token == "" || !auth.valid(token) {
		t.Fatal("new token is not valid")
	}
	auth.destroy(token)
	if auth.valid(token) {
		t.Fatal("destroyed token is still valid")
	}
	if auth.valid("unknown") {
		t.Fatal("unknown token accepted")
	}
}

func TestPublicAPIPaths(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/api/healthz", true},
		{http.MethodGet, "/api/auth/local", true},
		{http.MethodPost, "/api/auth/local", false},
		{http.MethodPost, "/api/auth/setup", true},
		{http.MethodGet, "/api/auth/setup", false},
		{http.MethodPost, "/api/auth/login", true},
		{http.MethodPost, "/api/auth/logout", true},
		{http.MethodGet, "/api/folders", false},
		{http.MethodPost, "/api/auth/qrcode", false},
		{http.MethodGet, "/app.js", false},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if got := isPublicAPIPath(r); got != tc.want {
			t.Errorf("%s %s = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestRequireLocalAuthBlocksAPI(t *testing.T) {
	s := &Server{auth: newLocalAuth()}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := s.requireLocalAuth(next)

	r := httptest.NewRequest(http.MethodGet, "/api/folders", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("protected API status = %d, want 401", w.Code)
	}

	token, err := s.auth.create()
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/folders", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("authenticated API status = %d, want 204", w.Code)
	}

	r = httptest.NewRequest(http.MethodGet, "/app.js", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("static status = %d, want 204", w.Code)
	}
}
