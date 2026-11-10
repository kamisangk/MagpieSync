package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestOpenClearsLegacyAccountProxies(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.UpsertAccount(ctx, "synthetic-video", "legacy-user", "name", "", []byte("credential"), 100)
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE accounts SET proxy_enc = ? WHERE id = ?`, []byte("legacy-proxy"), id); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		st, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var remaining int
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM accounts WHERE proxy_enc IS NOT NULL`).Scan(&remaining); err != nil {
			st.Close()
			t.Fatal(err)
		}
		account, err := st.GetAccountByID(ctx, id)
		if err != nil || remaining != 0 || string(account.Credential) != "credential" || account.Name != "name" {
			st.Close()
			t.Fatalf("legacy cleanup changed account or retained proxy: count=%d, account=%+v, error=%v", remaining, account, err)
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAccountsIncludeExpired(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	first, err := st.UpsertAccount(ctx, "synthetic-video", "1001", "alice", "", []byte("cred-a"), 100)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.UpsertAccount(ctx, "synthetic-video", "1002", "bob", "", []byte("cred-b"), 101)
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
	if got := seen[first]; got == nil || got.State != "needs_reauth" {
		t.Fatalf("expired account not preserved: %+v", got)
	}
	if got := seen[second]; got == nil || got.State != "active" {
		t.Fatalf("second account unexpected: %+v", got)
	}

	if err := st.UpdateAccountLogin(ctx, first, "alice-renamed", "", []byte("cred-a2"), 104); err != nil {
		t.Fatal(err)
	}
	relogged, err := st.GetAccountByID(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if relogged.State != "active" || relogged.Name != "alice-renamed" ||
		string(relogged.Credential) != "cred-a2" {
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
