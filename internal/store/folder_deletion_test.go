package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func deletionFixture(t *testing.T) (*Store, int64, int64) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	statements := []string{
		`INSERT INTO accounts(id,site,remote_uid,created_at,updated_at) VALUES(1,'synthetic','one',1,1),(2,'synthetic','two',1,1),(3,'different','three',1,1)`,
		`INSERT INTO folders(id,account_id,remote_id,title,state,created_at,updated_at) VALUES(1,1,'old','Old','archived',1,1),(2,1,'other','Other','archived',1,1)`,
		`INSERT INTO items(id,account_id,remote_id,title,url,first_seen_at,last_seen_at,updated_at) VALUES(1,1,'video','Video','',1,1,1)`,
		`INSERT INTO folder_items(folder_id,item_id) VALUES(1,1)`,
		`INSERT INTO folder_preferences(folder_id,auto_download,updated_at) VALUES(1,1,1)`,
		`INSERT INTO download_tasks(id,account_id,site,remote_id,state,file_path,created_at,updated_at) VALUES(1,1,'','video','done','out.mp4',1,1)`,
	}
	for _, q := range statements {
		if _, err := st.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return st, 1, 1
}

func TestDeleteArchivedFolderRetention(t *testing.T) {
	st, account, folder := deletionFixture(t)
	ctx := context.Background()
	if _, err := st.DeleteArchivedFolder(ctx, 2, folder, nil, 2); !errors.Is(err, ErrFolderNotFound) {
		t.Fatal(err)
	}
	result, err := st.DeleteArchivedFolder(ctx, account, folder, func(snapshot FolderCleanupSnapshot) FolderDeletionResult {
		if len(snapshot.Tasks) != 1 || !snapshot.Tasks[0].Candidate || snapshot.Tasks[0].Protected || snapshot.Tasks[0].Site != "synthetic" {
			t.Fatalf("snapshot=%+v", snapshot)
		}
		return FolderDeletionResult{MissingTaskIDs: []int64{1}}
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.FolderID != folder {
		t.Fatal(result)
	}
	for _, table := range []string{"folder_preferences", "folder_items"} {
		var count int
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	var state string
	st.db.QueryRow(`SELECT state FROM download_tasks WHERE id=1`).Scan(&state)
	if state != "missing" {
		t.Fatal(state)
	}
	var count int
	st.db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
}

func TestDeleteArchivedFolderProtection(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		protected   bool
	}{
		{"historical folder", `INSERT INTO folder_items(folder_id,item_id,state) VALUES(2,1,'removed')`, true},
		{"other account item", `INSERT INTO items(account_id,remote_id,title,url,first_seen_at,last_seen_at,updated_at) VALUES(2,'video','V','',1,1,1)`, true},
		{"other account task", `INSERT INTO download_tasks(account_id,remote_id,state,created_at,updated_at) VALUES(2,'video','failed',1,1)`, true},
		{"different site", `INSERT INTO download_tasks(account_id,remote_id,state,created_at,updated_at) VALUES(3,'video','done',1,1)`, false},
		{"pending part", `INSERT INTO download_tasks(account_id,remote_id,part_id,state,created_at,updated_at) VALUES(1,'video',2,'pending',1,1)`, true},
		{"paused part", `INSERT INTO download_tasks(account_id,remote_id,part_id,state,created_at,updated_at) VALUES(1,'video',2,'paused',1,1)`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, a, f := deletionFixture(t)
			if _, err := st.db.Exec(tc.query); err != nil {
				t.Fatal(err)
			}
			_, err := st.DeleteArchivedFolder(context.Background(), a, f, func(snapshot FolderCleanupSnapshot) FolderDeletionResult {
				if snapshot.Tasks[0].Protected != tc.protected {
					t.Fatalf("protected=%v", snapshot.Tasks[0].Protected)
				}
				return FolderDeletionResult{}
			}, 2)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeleteArchivedFolderConflicts(t *testing.T) {
	for _, query := range []string{`UPDATE folders SET state='active' WHERE id=1`, `INSERT INTO sync_runs(account_id,trigger,state,started_at) VALUES(1,'manual','running',1)`} {
		st, a, f := deletionFixture(t)
		if _, err := st.db.Exec(query); err != nil {
			t.Fatal(err)
		}
		if _, err := st.DeleteArchivedFolder(context.Background(), a, f, nil, 2); !errors.Is(err, ErrFolderDeletionConflict) {
			t.Fatal(err)
		}
	}
}

func TestDeleteArchivedFolderSerializesClaimsAndReferences(t *testing.T) {
	st, a, f := deletionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	deleted := make(chan error, 1)
	go func() {
		_, err := st.DeleteArchivedFolder(ctx, a, f, func(FolderCleanupSnapshot) FolderDeletionResult {
			close(entered)
			<-release
			return FolderDeletionResult{}
		}, 2)
		deleted <- err
	}()
	<-entered
	claimed := make(chan error, 1)
	linked := make(chan error, 1)
	go func() { _, err := st.ClaimAnyDownloadTask(ctx, 3); claimed <- err }()
	go func() {
		linked <- st.WithTx(ctx, func(tx *Tx) error { _, err := tx.UpsertFolderItem(ctx, 2, 1, 1); return err })
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
