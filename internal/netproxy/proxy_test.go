package netproxy

import (
	"strings"
	"testing"
)

func TestNormalizeErrorsDoNotExposeProxyCredentials(t *testing.T) {
	for _, raw := range []string{"http://username:proxy-secret@host:invalid", "proxy-secret://host"} {
		_, err := Normalize(raw)
		if err == nil || strings.Contains(err.Error(), "proxy-secret") {
			t.Fatalf("unsafe proxy error: %v", err)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "empty", raw: "  ", want: ""},
		{name: "host defaults to http", raw: "127.0.0.1:7890", want: "http://127.0.0.1:7890"},
		{name: "http", raw: "http://127.0.0.1:7890", want: "http://127.0.0.1:7890"},
		{name: "https credentials", raw: "https://user:pass@proxy.example:8443", want: "https://user:pass@proxy.example:8443"},
		{name: "socks default port", raw: "socks5://127.0.0.1", want: "socks5://127.0.0.1:1080"},
		{name: "socksh credentials", raw: "SOCKS5H://user:pass@127.0.0.1:1080", want: "socks5h://user:pass@127.0.0.1:1080"},
		{name: "bad scheme", raw: "ftp://127.0.0.1:21", wantErr: true},
		{name: "path", raw: "http://127.0.0.1:7890/proxy", wantErr: true},
		{name: "missing host", raw: "socks5://", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Normalize(%q) = %q, want error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Normalize(%q): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestTransport(t *testing.T) {
	tr, err := Transport("socks5://127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Proxy != nil {
		t.Fatal("SOCKS5 transport must not use an HTTP proxy function")
	}
	if tr.DialContext == nil {
		t.Fatal("SOCKS5 transport has no dialer")
	}
	direct, err := Transport("")
	if err != nil {
		t.Fatal(err)
	}
	if direct != nil {
		t.Fatal("direct transport should be nil")
	}
}
