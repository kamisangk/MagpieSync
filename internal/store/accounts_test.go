package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestAccountsIncludeExpiredAndProxy(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	first, err := st.UpsertAccountWithProxy(ctx, "synthetic-video", "1001", "alice", "", []byte("cred-a"), []byte("proxy-a"), 100)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.UpsertAccountWithProxy(ctx, "synthetic-video", "1002", "bob", "", []byte("cred-b"), nil, 101)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAccountState(ctx, first, "needs_reauth", 102); err != nil {
		t.Fatal(err)
	}

	accounts, err := st.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("ListAccounts returned %d accounts, want 2", len(accounts))
	}
	seen := map[int64]*Account{}
	for i := range accounts {
		seen[accounts[i].ID] = &accounts[i]
	}
	if got := seen[first]; got == nil || got.State != "needs_reauth" || string(got.ProxyEnc) != "proxy-a" {
		t.Fatalf("expired account not preserved: %+v", got)
	}
	if got := seen[second]; got == nil || got.State != "active" || len(got.ProxyEnc) != 0 {
		t.Fatalf("second account unexpected: %+v", got)
	}

	if err := st.UpdateAccountProxy(ctx, second, []byte("proxy-b"), 103); err != nil {
		t.Fatal(err)
	}
	updated, err := st.GetAccountByID(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated.ProxyEnc) != "proxy-b" {
		t.Fatalf("proxy = %q, want proxy-b", updated.ProxyEnc)
	}

	if err := st.UpdateAccountLogin(ctx, first, "alice-renamed", "", []byte("cred-a2"), []byte("proxy-a2"), 104); err != nil {
		t.Fatal(err)
	}
	relogged, err := st.GetAccountByID(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if relogged.State != "active" || relogged.Name != "alice-renamed" ||
		string(relogged.Credential) != "cred-a2" || string(relogged.ProxyEnc) != "proxy-a2" {
		t.Fatalf("relogin did not update account: %+v", relogged)
	}
}

func TestDeleteAccountCascadesLocalData(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	id, err := st.UpsertAccount(ctx, "synthetic-video", "1001", "alice", "", []byte("cred"), 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReconcileFolders(ctx, id, []FolderInput{{RemoteID: "f1", Title: "收藏", Kind: "favorite"}}, 101); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRun(ctx, id, "manual", "all", 102); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAccount(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetAccountByID(ctx, id); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("deleted account still exists: %v", err)
	}
	if folders, err := st.CountFolders(ctx, id); err != nil || folders != 0 {
		t.Fatalf("folders = %d, err = %v", folders, err)
	}
	if runs, err := st.ListRuns(ctx, id, 10); err != nil || len(runs) != 0 {
		t.Fatalf("runs = %+v, err = %v", runs, err)
	}
}
