package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"magpiesync/internal/store"
	"magpiesync/internal/vault"
)

func TestDeleteAccountHandlerRemovesAccountAndLocalData(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	v, err := vault.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st, vault: v}
	ctx := context.Background()
	id, err := st.UpsertAccount(ctx, "synthetic-video", "1001", "alice", "", []byte("cred"), 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReconcileFolders(ctx, id, []store.FolderInput{{RemoteID: "fav", Title: "收藏", Kind: "favorite"}}, 101); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/accounts/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	s.handleAccountDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetAccountByID(ctx, id); err == nil {
		t.Fatal("account still exists")
	}
	if folders, err := st.CountFolders(ctx, id); err != nil || folders != 0 {
		t.Fatalf("folders = %d, err = %v", folders, err)
	}
}
