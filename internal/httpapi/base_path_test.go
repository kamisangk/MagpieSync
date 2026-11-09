package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"magpiesync/internal/config"
)

func TestHandlerServesGatewayPrefixedRoutes(t *testing.T) {
	s := &Server{cfg: config.Config{BasePath: "/app/magpiesync"}, auth: newLocalAuth()}
	h := s.Handler()

	r := httptest.NewRequest(http.MethodGet, "/app/magpiesync/api/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("gateway health status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unprefixed health status = %d, want %d", w.Code, http.StatusNotFound)
	}

	r = httptest.NewRequest(http.MethodGet, "/app/magpiesyncX/api/healthz", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("lookalike-prefix health status = %d, want %d", w.Code, http.StatusNotFound)
	}

	r = httptest.NewRequest(http.MethodGet, "/app/magpiesync/app.js", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "applicationBasePath") {
		t.Fatalf("gateway app.js response = %d, want JS asset: %s", w.Code, w.Body.String())
	}
}
