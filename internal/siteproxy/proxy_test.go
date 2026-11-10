package siteproxy

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"magpiesync/internal/store"
	"magpiesync/internal/vault"
)

func TestEncryptedProxyPersistenceAndIsolation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { st.Close() }()
	v, err := vault.New("", dir)
	if err != nil {
		t.Fatal(err)
	}
	const raw = "http://proxy-user:proxy-secret@127.0.0.1:8123"
	encoded, err := Encode(v, "alpha", raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, "proxy-secret") || encoded == raw {
		t.Fatal("proxy was not encrypted")
	}
	if err := st.SetSettings(ctx, map[string]string{Key("alpha"): encoded, Key("beta"): encoded}, 100); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(ctx, st, v, "alpha"); err != nil || got != raw {
		t.Fatalf("round trip = %q, %v", got, err)
	}
	if _, err := Load(ctx, st, v, "beta"); err == nil {
		t.Fatal("ciphertext moved between sites")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err = vault.New("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Load(ctx, st, v, "alpha"); err != nil || got != raw {
		t.Fatalf("after restart = %q, %v", got, err)
	}
	empty, err := Encode(nil, "alpha", "  ")
	if err != nil || empty != "" {
		t.Fatalf("clear = %q, %v", empty, err)
	}
	if err := st.SetSettings(ctx, map[string]string{Key("alpha"): empty}, 101); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(ctx, st, nil, "alpha"); err != nil || got != "" {
		t.Fatalf("cleared = %q, %v", got, err)
	}
}

func TestProxyValidationDoesNotEchoSecrets(t *testing.T) {
	for _, raw := range []string{"http://proxy-user:proxy-secret@host:bad", "proxy-secret://host", "http://proxy-user:proxy-secret@host/path"} {
		_, err := Encode(nil, "alpha", raw)
		if err == nil || strings.Contains(err.Error(), "proxy-secret") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
}
