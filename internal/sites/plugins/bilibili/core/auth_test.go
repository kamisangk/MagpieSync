//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"net/url"
	"testing"
)

func TestMixinKeyPermutation(t *testing.T) {
	orig := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-_"
	if len(orig) != 64 {
		t.Fatalf("test fixture length = %d", len(orig))
	}
	key := getMixinKey(orig)
	if len(key) != 32 {
		t.Fatalf("mixin key length = %d", len(key))
	}
	for i := 0; i < 32; i++ {
		want := orig[mixinKeyEncTab[i]]
		if key[i] != want {
			t.Fatalf("key[%d] = %q, want %q", i, key[i], want)
		}
	}
}

func TestSignParamsDeterministic(t *testing.T) {
	const mixin = "0123456789abcdef0123456789abcdef"
	p := url.Values{"media_id": {"123"}, "pn": {"1"}, "ps": {"20"}}
	signParams(p, mixin, 1700000000)
	if p.Get("wts") != "1700000000" {
		t.Fatalf("wts = %q", p.Get("wts"))
	}
	if len(p.Get("w_rid")) != 32 {
		t.Fatalf("w_rid = %q", p.Get("w_rid"))
	}
	first := p.Get("w_rid")

	q := url.Values{"media_id": {"123"}, "pn": {"1"}, "ps": {"20"}}
	signParams(q, mixin, 1700000000)
	if q.Get("w_rid") != first {
		t.Fatalf("signature not deterministic: %q vs %q", first, q.Get("w_rid"))
	}

	r := url.Values{"media_id": {"124"}, "pn": {"1"}, "ps": {"20"}}
	signParams(r, mixin, 1700000000)
	if r.Get("w_rid") == first {
		t.Fatal("signature did not change with params")
	}
}

func TestParseCredentialRequiresSESSDATA(t *testing.T) {
	if _, err := ParseCredential([]byte(`{"cookies":{"a":"b"}}`)); err == nil {
		t.Fatal("expected error without SESSDATA")
	}
	c, err := ParseCredential([]byte(`{"cookies":{"SESSDATA":"x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Cookies["SESSDATA"] != "x" {
		t.Fatalf("cookie = %+v", c.Cookies)
	}
}

func TestParseCookieString(t *testing.T) {
	cookies := ParseCookieString("SESSDATA=abc; bili_jct=def; DedeUserID=42")
	if cookies["SESSDATA"] != "abc" || cookies["bili_jct"] != "def" || cookies["DedeUserID"] != "42" {
		t.Fatalf("cookies = %+v", cookies)
	}
}
