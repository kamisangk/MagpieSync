package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"magpiesync/internal/store"
)

func accountCleanupFixture(t *testing.T) (*store.Store, int64, int64, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	a, err := st.UpsertAccount(ctx, "synthetic-cleanup", "one", "One", "", []byte("credential"), 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.UpsertAccount(ctx, "synthetic-cleanup", "two", "Two", "", []byte("credential"), 1)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = st.SetSettings(ctx, map[string]string{"download.media_dir": root}, 1); err != nil {
		t.Fatal(err)
	}
	return st, a, b, root
}

func completedAccountOutput(t *testing.T, st *store.Store, account int64, remote, path string, quality int) int64 {
	t.Helper()
	ctx := context.Background()
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateDownloadTasks(ctx, account, quality, []store.DownloadTaskInput{{Site: "synthetic-cleanup", RemoteID: remote, PartID: int64(quality)}}, 1); err != nil {
		t.Fatal(err)
	}
	task, err := st.ClaimAnyDownloadTask(ctx, 1)
	if err != nil || task == nil {
		t.Fatalf("task=%v err=%v", task, err)
	}
	if err = st.FinishDownloadTask(ctx, task.ID, "done", path, "", 0, "", 7, 1); err != nil {
		t.Fatal(err)
	}
	return task.ID
}

func TestAccountCleanupSharedVideoDoesNotStopUniqueCleanup(t *testing.T) {
	st, a, b, root := accountCleanupFixture(t)
	ctx := context.Background()
	shared := filepath.Join(root, "shared.mp4")
	unique := filepath.Join(root, "unique.mp4")
	completedAccountOutput(t, st, a, "shared", shared, 0)
	completedAccountOutput(t, st, a, "unique", unique, 0)
	if err := st.WithTx(ctx, func(tx *store.Tx) error {
		_, _, _, err := tx.UpsertItem(ctx, b, store.ItemInput{RemoteID: "shared", Title: "Shared"}, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err := DeleteAccount(ctx, st, a, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedFiles != 1 || result.ProtectedVideos != 1 || result.FailedFiles != 0 {
		t.Fatalf("%+v", result)
	}
	if _, err = os.Stat(shared); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(unique); !os.IsNotExist(err) {
		t.Fatalf("unique stat=%v", err)
	}
	if _, err = st.GetAccountByID(ctx, a); !errors.Is(err, store.ErrNoAccount) {
		t.Fatal(err)
	}
	if _, err = st.GetAccountByID(ctx, b); err != nil {
		t.Fatal(err)
	}
}

func TestAccountCleanupOwnDuplicateOutputAndDefaultRetention(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "retain", true: "delete"}[remove], func(t *testing.T) {
			st, a, _, root := accountCleanupFixture(t)
			path := filepath.Join(root, "video.mp4")
			completedAccountOutput(t, st, a, "video", path, 0)
			completedAccountOutput(t, st, a, "video", path, 1)
			result, err := DeleteAccount(context.Background(), st, a, remove)
			if err != nil {
				t.Fatal(err)
			}
			_, err = os.Stat(path)
			if remove {
				if result.DeletedFiles != 1 || !os.IsNotExist(err) {
					t.Fatalf("result=%+v stat=%v", result, err)
				}
			} else if err != nil || result.DeletedFiles != 0 {
				t.Fatalf("result=%+v stat=%v", result, err)
			}
		})
	}
}

func TestAccountCleanupBusyWorkerAndPersistedTask(t *testing.T) {
	st, a, _, root := accountCleanupFixture(t)
	ctx := context.Background()
	path := filepath.Join(root, "video.mp4")
	completedAccountOutput(t, st, a, "video", path, 0)
	if _, _, err := st.CreateDownloadTasks(ctx, a, 0, []store.DownloadTaskInput{{RemoteID: "worker"}}, 1); err != nil {
		t.Fatal(err)
	}
	m := NewManager(st, nil, nil, nil, nil)
	task, _, cancel, err := m.claimTask(ctx)
	if err != nil || task == nil {
		t.Fatal(err)
	}
	defer cancel()
	if _, err = m.DeleteAccount(ctx, a, false); !errors.Is(err, store.ErrAccountDeletionConflict) {
		t.Fatalf("running=%v", err)
	}
	if err = st.CancelDownloadTask(ctx, task.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = m.DeleteAccount(ctx, a, true); !errors.Is(err, store.ErrAccountDeletionConflict) {
		t.Fatalf("registered canceled=%v", err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestAccountCleanupExternalPathsAndUnsafeOutputs(t *testing.T) {
	for _, kind := range []string{"same path", "hardlink", "outside root", "other worker"} {
		t.Run(kind, func(t *testing.T) {
			st, a, b, root := accountCleanupFixture(t)
			ctx := context.Background()
			path := filepath.Join(root, "video.mp4")
			completedAccountOutput(t, st, a, "video", path, 0)
			switch kind {
			case "same path":
				completedAccountOutput(t, st, b, "different-video", path, 0)
			case "hardlink":
				alias := filepath.Join(root, "alias.mp4")
				completedAccountOutput(t, st, b, "different-video", alias, 0)
				if err := os.Remove(alias); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(path, alias); err != nil {
					t.Skip(err)
				}
			case "outside root":
				if err := st.SetSettings(ctx, map[string]string{"download.media_dir": t.TempDir()}, 1); err != nil {
					t.Fatal(err)
				}
			case "other worker":
				if _, _, err := st.CreateDownloadTasks(ctx, b, 0, []store.DownloadTaskInput{{RemoteID: "worker"}}, 1); err != nil {
					t.Fatal(err)
				}
				if task, err := st.ClaimAnyDownloadTask(ctx, 1); err != nil || task == nil {
					t.Fatal(err)
				}
			}
			result, err := DeleteAccount(ctx, st, a, true)
			if err != nil {
				t.Fatal(err)
			}
			if result.DeletedFiles != 0 || result.ProtectedVideos+result.SkippedFiles == 0 {
				t.Fatalf("%+v", result)
			}
			if _, err = os.Stat(path); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAccountCleanupDifferentSiteIDDoesNotProtectFile(t *testing.T) {
	st, a, _, root := accountCleanupFixture(t)
	ctx := context.Background()
	path := filepath.Join(root, "video.mp4")
	completedAccountOutput(t, st, a, "video", path, 0)
	other, err := st.UpsertAccount(ctx, "different-site", "other", "Other", "", []byte("credential"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.WithTx(ctx, func(tx *store.Tx) error {
		_, _, _, err := tx.UpsertItem(ctx, other, store.ItemInput{RemoteID: "video", Title: "Other Video"}, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err := DeleteAccount(ctx, st, a, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedFiles != 1 || result.ProtectedVideos != 0 {
		t.Fatalf("%+v", result)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestAccountCleanupRemovalFailureContinues(t *testing.T) {
	root := t.TempDir()
	failed := filepath.Join(root, "failed.mp4")
	unique := filepath.Join(root, "unique.mp4")
	for _, path := range []string{failed, unique} {
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := store.FolderCleanupSnapshot{MediaDir: root, DeletingAccountID: 1, Tasks: []store.FolderCleanupTask{
		{ID: 1, AccountID: 1, Site: "synthetic-cleanup", RemoteID: "failed", State: "done", Candidate: true, FilePath: failed},
		{ID: 2, AccountID: 1, Site: "synthetic-cleanup", RemoteID: "unique", State: "done", Candidate: true, FilePath: unique},
	}}
	result := cleanupFolderFilesUsing(context.Background(), snapshot, func(root, path string) error {
		if path == failed {
			return os.ErrPermission
		}
		return removeCleanupFile(root, path)
	})
	if result.FailedFiles != 1 || result.DeletedFiles != 1 || len(result.MissingTaskIDs) != 1 || result.MissingTaskIDs[0] != 2 {
		t.Fatalf("%+v", result)
	}
	if strings.Contains(strings.Join(result.Reasons, ";"), "原下载记录已保留") {
		t.Fatalf("misleading reason=%v", result.Reasons)
	}
	if _, err := os.Stat(failed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unique); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
