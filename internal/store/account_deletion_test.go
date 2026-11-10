package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAccountDeletionSnapshotProtection(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		protected   bool
	}{
		{"own other folder", `INSERT INTO folder_items(folder_id,item_id,state) VALUES(2,1,'removed')`, false},
		{"other account item", `INSERT INTO items(account_id,remote_id,title,url,state,first_seen_at,last_seen_at,updated_at) VALUES(2,'video','V','','removed',1,1,1)`, true},
		{"other account task", `INSERT INTO download_tasks(account_id,remote_id,state,created_at,updated_at) VALUES(2,'video','failed',1,1)`, true},
		{"different site", `INSERT INTO download_tasks(account_id,remote_id,state,created_at,updated_at) VALUES(3,'video','done',1,1)`, false},
		{"ambiguous target", `UPDATE download_tasks SET site='different' WHERE id=1`, true},
		{"unknown external site", `UPDATE accounts SET site='' WHERE id=2; INSERT INTO download_tasks(account_id,remote_id,state,created_at,updated_at) VALUES(2,'video','done',1,1)`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, a, _ := deletionFixture(t)
			if _, err := st.db.Exec(tc.query); err != nil {
				t.Fatal(err)
			}
			called := false
			_, err := st.DeleteAccountWithCleanup(context.Background(), a, func(snapshot FolderCleanupSnapshot) FolderDeletionResult {
				called = true
				if snapshot.DeletingAccountID != a || len(snapshot.Tasks) == 0 || !snapshot.Tasks[0].Candidate || snapshot.Tasks[0].Protected != tc.protected {
					t.Fatalf("%+v", snapshot)
				}
				return FolderDeletionResult{}
			})
			if err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("cleanup not called")
			}
			if _, err = st.GetAccountByID(context.Background(), a); !errors.Is(err, ErrNoAccount) {
				t.Fatal(err)
			}
		})
	}
}

func TestAccountDeletionRejectsBusyAndMissing(t *testing.T) {
	for _, query := range []string{`INSERT INTO sync_runs(account_id,trigger,state,started_at) VALUES(1,'manual','running',1)`, `UPDATE download_tasks SET state='running' WHERE id=1`} {
		st, a, _ := deletionFixture(t)
		if _, err := st.db.Exec(query); err != nil {
			t.Fatal(err)
		}
		called := false
		if _, err := st.DeleteAccountWithCleanup(context.Background(), a, func(FolderCleanupSnapshot) FolderDeletionResult { called = true; return FolderDeletionResult{} }); !errors.Is(err, ErrAccountDeletionConflict) || called {
			t.Fatalf("err=%v called=%v", err, called)
		}
		if _, err := st.GetAccountByID(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	st, _, _ := deletionFixture(t)
	if _, err := st.DeleteAccountWithCleanup(context.Background(), 999, nil); !errors.Is(err, ErrNoAccount) {
		t.Fatal(err)
	}
}

func TestAccountDeletionSerializesClaimsAndReferences(t *testing.T) {
	st, a, _ := deletionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	deleted := make(chan error, 1)
	go func() {
		_, err := st.DeleteAccountWithCleanup(ctx, a, func(FolderCleanupSnapshot) FolderDeletionResult {
			close(entered)
			<-release
			return FolderDeletionResult{}
		})
		deleted <- err
	}()
	<-entered
	claimed := make(chan error, 1)
	linked := make(chan error, 1)
	go func() { _, err := st.ClaimAnyDownloadTask(ctx, 3); claimed <- err }()
	go func() {
		linked <- st.WithTx(ctx, func(tx *Tx) error {
			_, _, _, err := tx.UpsertItem(ctx, 2, ItemInput{RemoteID: "video", Title: "Shared"}, 3)
			return err
		})
	}()
	select {
	case err := <-claimed:
		t.Fatalf("claim escaped transaction: %v", err)
	case err := <-linked:
		t.Fatalf("reference escaped transaction: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	for _, ch := range []chan error{deleted, claimed, linked} {
		if err := <-ch; err != nil {
			t.Fatal(err)
		}
	}
}
