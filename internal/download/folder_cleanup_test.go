package download

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"magpiesync/internal/store"
)

func TestFolderCleanupFilesAndCompanions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "video.mp4")
	for _, name := range []string{"video.mp4", "video.nfo", "video.en.srt", "video.zh-CN.default.ass", "poster.jpg", "unrelated.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result := cleanupFolderFiles(context.Background(), store.FolderCleanupSnapshot{MediaDir: root, Tasks: []store.FolderCleanupTask{{ID: 1, Site: "synthetic", RemoteID: "v", State: "done", Candidate: true, FilePath: path}}})
	if result.DeletedFiles != 4 || result.SkippedFiles != 1 || len(result.MissingTaskIDs) != 1 {
		t.Fatalf("%+v", result)
	}
	for _, name := range []string{"poster.jpg", "unrelated.txt"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestFolderCleanupSharedAndBusy(t *testing.T) {
	for _, kind := range []string{"reference", "path", "alias", "stem", "running", "outside", "directory"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "video.mp4")
			os.WriteFile(path, []byte("fixture"), 0600)
			tasks := []store.FolderCleanupTask{{ID: 1, Site: "synthetic", RemoteID: "v", State: "done", Candidate: true, FilePath: path}}
			snap := store.FolderCleanupSnapshot{MediaDir: root}
			switch kind {
			case "reference":
				tasks[0].Protected = true
			case "path":
				tasks = append(tasks, store.FolderCleanupTask{ID: 2, State: "failed", FilePath: filepath.Join(root, ".", "video.mp4")})
			case "alias":
				alias := filepath.Join(root, "alias.mp4")
				if err := os.Link(path, alias); err != nil {
					t.Skip(err)
				}
				tasks = append(tasks, store.FolderCleanupTask{ID: 2, State: "done", FilePath: alias})
			case "stem":
				os.WriteFile(filepath.Join(root, "video.nfo"), []byte("fixture"), 0600)
				tasks = append(tasks, store.FolderCleanupTask{ID: 2, State: "missing", FilePath: filepath.Join(root, "video.mkv")})
			case "running":
				snap.RunningDownload = true
			case "outside":
				snap.MediaDir = t.TempDir()
			case "directory":
				tasks[0].FilePath = root
			}
			snap.Tasks = tasks
			result := cleanupFolderFiles(context.Background(), snap)
			if kind == "stem" {
				if _, err := os.Stat(filepath.Join(root, "video.nfo")); err != nil {
					t.Fatal(err)
				}
				if result.DeletedFiles != 1 {
					t.Fatal(result)
				}
				return
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
			if result.DeletedFiles != 0 || len(result.MissingTaskIDs) != 0 {
				t.Fatal(result)
			}
		})
	}
}

func TestFolderCleanupMissingAndSymlink(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing.mp4")
	result := cleanupFolderFiles(context.Background(), store.FolderCleanupSnapshot{MediaDir: root, Tasks: []store.FolderCleanupTask{{ID: 1, State: "done", Candidate: true, FilePath: missing}}})
	if len(result.MissingTaskIDs) != 1 || result.DeletedFiles != 0 {
		t.Fatal(result)
	}
	outside := t.TempDir()
	target := filepath.Join(outside, "target.mp4")
	os.WriteFile(target, []byte("fixture"), 0600)
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	if _, err := safeCleanupPath(root, filepath.Join(link, "target.mp4")); err == nil {
		t.Fatal("accepted escape")
	}
	link = filepath.Join(root, "file.mp4")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if _, err := safeCleanupPath(root, link); err == nil {
		t.Fatal("accepted file symlink")
	}
}

func TestFolderCleanupFailurePreservesDownload(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "video.mp4")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := store.FolderCleanupSnapshot{MediaDir: root, Tasks: []store.FolderCleanupTask{{ID: 1, State: "done", Candidate: true, FilePath: path}}}
	result := cleanupFolderFilesUsing(context.Background(), snapshot, func(string, string) error { return os.ErrPermission })
	if result.FailedFiles != 1 || result.DeletedFiles != 0 || len(result.MissingTaskIDs) != 0 {
		t.Fatal(result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestFolderCleanupAbsentDirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "absent", "video.mp4")
	result := cleanupFolderFiles(context.Background(), store.FolderCleanupSnapshot{MediaDir: root, Tasks: []store.FolderCleanupTask{{ID: 1, State: "done", Candidate: true, FilePath: path}}})
	if len(result.MissingTaskIDs) != 1 || result.FailedFiles != 0 {
		t.Fatal(result)
	}
}

func TestFolderCleanupCanceledWorkerStillProtectsFiles(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	a, err := st.UpsertAccount(ctx, "synthetic-cleanup", "one", "One", "", []byte("credential"), 1)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := st.ReconcileFolders(ctx, a, []store.FolderInput{{RemoteID: "one", Title: "One"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.WithTx(ctx, func(tx *store.Tx) error {
		item, _, _, err := tx.UpsertItem(ctx, a, store.ItemInput{RemoteID: "v", Title: "Video"}, 1)
		if err != nil {
			return err
		}
		_, err = tx.UpsertFolderItem(ctx, folders["one"], item, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "video.mp4")
	os.WriteFile(path, []byte("fixture"), 0600)
	if err = st.SetSettings(ctx, map[string]string{"download.media_dir": root}, 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err = st.CreateDownloadTasks(ctx, a, 0, []store.DownloadTaskInput{{Site: "synthetic-cleanup", RemoteID: "v"}}, 1); err != nil {
		t.Fatal(err)
	}
	task, err := st.ClaimAnyDownloadTask(ctx, 1)
	if err != nil || task == nil {
		t.Fatal(err)
	}
	if err = st.FinishDownloadTask(ctx, task.ID, "done", path, "", 0, "", 7, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = st.ReconcileFolders(ctx, a, nil, 2); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(st, nil, nil, nil, nil)
	// Claim a second task but deliberately delay launching its worker. Canceling
	// in this boundary must find the synchronously registered cancellation, and
	// cleanup must still see that worker after its DB state becomes canceled.
	if _, _, err = st.CreateDownloadTasks(ctx, a, 0, []store.DownloadTaskInput{{Site: "synthetic-cleanup", RemoteID: "delayed-worker"}}, 2); err != nil {
		t.Fatal(err)
	}
	claimed, workerCtx, cancel, err := manager.claimTask(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	defer cancel()
	if err = st.CancelDownloadTask(ctx, claimed.ID, 2); err != nil {
		t.Fatal(err)
	}
	manager.CancelTask(claimed.ID)
	select {
	case <-workerCtx.Done():
	default:
		t.Fatal("cancellation missed claimed worker before processing")
	}
	result, err := manager.DeleteArchivedFolder(ctx, a, folders["one"], true, 3)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedFiles != 0 || result.SkippedFiles != 1 || !result.PartialCleanup {
		t.Fatal(result)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestFolderCleanupReservedMetadataNames(t *testing.T) {
	for _, name := range []string{"person", "tvshow"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, name+".mp4")
			meta := filepath.Join(root, name+".nfo")
			for _, file := range []string{path, meta} {
				if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result := cleanupFolderFiles(context.Background(), store.FolderCleanupSnapshot{MediaDir: root, Tasks: []store.FolderCleanupTask{{ID: 1, State: "done", Candidate: true, FilePath: path}}})
			if result.DeletedFiles != 1 || result.SkippedFiles != 1 {
				t.Fatal(result)
			}
			if _, err := os.Stat(meta); err != nil {
				t.Fatal(err)
			}
			result = cleanupFolderFiles(context.Background(), store.FolderCleanupSnapshot{MediaDir: root, Tasks: []store.FolderCleanupTask{{ID: 1, State: "done", Candidate: true, FilePath: meta}}})
			if result.DeletedFiles != 0 || result.SkippedFiles != 1 {
				t.Fatal(result)
			}
		})
	}
}

func TestClaimTaskWaitsForPreviousWorkerAfterResume(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	account, err := st.UpsertAccount(ctx, "synthetic-claim", "one", "One", "", []byte("credential"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = st.CreateDownloadTasks(ctx, account, 0, []store.DownloadTaskInput{{Site: "synthetic-claim", RemoteID: "first"}, {Site: "synthetic-claim", RemoteID: "second"}}, 1); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(st, nil, nil, nil, nil)
	first, firstCtx, cancelFirst, err := manager.claimTask(ctx)
	if err != nil || first == nil {
		t.Fatalf("task=%v err=%v", first, err)
	}
	defer cancelFirst()
	if err = st.PauseDownloadTask(ctx, first.ID, 2); err != nil {
		t.Fatal(err)
	}
	manager.CancelTask(first.ID)
	if err = st.ResumeDownloadTask(ctx, first.ID, 3); err != nil {
		t.Fatal(err)
	}
	second, _, cancelSecond, err := manager.claimTask(ctx)
	if err != nil || second == nil {
		t.Fatalf("task=%v err=%v", second, err)
	}
	defer cancelSecond()
	if second.ID == first.ID || second.RemoteID != "second" {
		t.Fatalf("claimed unfinished worker again: %+v", second)
	}
	if firstCtx.Err() == nil {
		t.Fatal("previous worker did not receive pause cancellation")
	}
	if task, _, cancel, err := manager.claimTask(ctx); err != nil || task != nil {
		if cancel != nil {
			cancel()
		}
		t.Fatalf("registered resumed task should remain excluded: %+v %v", task, err)
	}
	// Simulate precisely the old worker's deferred registry release.
	manager.mu.Lock()
	delete(manager.cancels, first.ID)
	manager.mu.Unlock()
	resumed, resumedCtx, cancelResumed, err := manager.claimTask(ctx)
	if err != nil || resumed == nil {
		t.Fatalf("task=%v err=%v", resumed, err)
	}
	defer cancelResumed()
	if resumed.ID != first.ID {
		t.Fatalf("resumed wrong task: %+v", resumed)
	}
	manager.CancelTask(first.ID)
	if resumedCtx.Err() == nil {
		t.Fatal("resumed worker cancellation was not registered")
	}
}
