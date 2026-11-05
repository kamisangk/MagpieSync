//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bilibili

import (
	"testing"

	"magpiesync/internal/sites"
)

func TestBilibiliSiteDescriptor(t *testing.T) {
	site, ok := sites.Get("bilibili")
	if !ok {
		t.Fatal("bilibili site is not registered")
	}
	if !site.Ready || !site.Download {
		t.Fatalf("bilibili should be ready: %+v", site)
	}
	if len(site.Login.Methods) == 0 || site.Login.Methods[0].ID != "qrcode" {
		t.Fatalf("bilibili login methods = %+v", site.Login.Methods)
	}
	if asset, ok := sites.Asset("bilibili", "logo.svg"); !ok || len(asset) == 0 {
		t.Fatal("bilibili logo is not registered")
	}
	want := map[string]bool{"favorite": true, "collection": true, "submission": true, "watch_later": true}
	for _, c := range site.Categories {
		delete(want, c.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing categories: %v", want)
	}
	if _, ok := sites.DriverFor("bilibili"); !ok {
		t.Fatal("bilibili driver is not registered")
	}
	if got := sites.CategoryName("bilibili", "season"); got != "合集" {
		t.Fatalf("season category = %q", got)
	}
	if got := sites.CategoryName("bilibili", "series"); got != "合集" {
		t.Fatalf("series category = %q", got)
	}
}
