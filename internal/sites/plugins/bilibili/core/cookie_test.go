//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"net/http"
	"net/url"
	"testing"
)

func TestCookieMapIncludesHostOnlyPassportCookie(t *testing.T) {
	c := NewGuest()
	u := &url.URL{Scheme: "https", Host: "passport.bilibili.com"}
	c.jar.SetCookies(u, []*http.Cookie{{Name: "SESSDATA", Value: "abc", Path: "/"}})

	got := c.CookieMap()
	if got["SESSDATA"] != "abc" {
		t.Fatalf("CookieMap() = %+v, want SESSDATA=abc", got)
	}
}

func TestSeededCookiesAreSentToAPIHost(t *testing.T) {
	c := NewWithCookies(map[string]string{"SESSDATA": "abc", "buvid3": "xyz"}, 1.5)
	u := &url.URL{Scheme: "https", Host: "api.bilibili.com"}
	found := map[string]string{}
	for _, ck := range c.jar.Cookies(u) {
		found[ck.Name] = ck.Value
	}
	if found["SESSDATA"] != "abc" || found["buvid3"] != "xyz" {
		t.Fatalf("api cookies = %+v", found)
	}
}
