package httpapi

import (
	"net/http/httptest"
	"testing"

	"magpiesync/internal/sites"
)

func TestSiteAssetRouteServesRegisteredAsset(t *testing.T) {
	sites.RegisterAsset("synthetic-http", "logo.svg", []byte("<svg></svg>"))
	req := httptest.NewRequest("GET", "/api/site-assets/synthetic-http/logo.svg", nil)
	req.SetPathValue("site", "synthetic-http")
	req.SetPathValue("asset", "logo.svg")
	res := httptest.NewRecorder()
	(&Server{}).handleSiteAsset(res, req)
	if res.Code != 200 || res.Body.String() != "<svg></svg>" {
		t.Fatalf("response = %d %q", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("content type = %q", got)
	}
}
