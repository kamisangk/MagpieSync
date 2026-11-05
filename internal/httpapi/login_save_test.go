package httpapi

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"magpiesync/internal/sites"
	"magpiesync/internal/store"
	"magpiesync/internal/vault"
)

func TestSaveAccountRejectsChangedIdentity(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	v, err := vault.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st, vault: v}
	id, err := st.UpsertAccount(context.Background(), "synthetic", "old-id", "old-name", "", []byte("credential"), 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.saveAccount(context.Background(), sites.Site{ID: "synthetic", Name: "Synthetic"}, &sites.LoginResult{
		UID: "new-id", Name: "new-name", Credential: []byte("new credential"),
	}, "", id)
	if !errors.Is(err, errAccountMismatch) {
		t.Fatalf("saveAccount error = %v, want account mismatch", err)
	}
}
