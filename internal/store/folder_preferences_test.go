package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFolderAutoDownloadPreferenceDefaultsScopesAndCascades(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()

	accountID, err := st.UpsertAccount(ctx, "test-site", "1", "one", "", []byte("credential"), 100)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := st.ReconcileFolders(ctx, accountID, []FolderInput{{RemoteID: "folder-1", Title: "Folder"}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	folderID := folders["folder-1"]
	listed, err := st.ListFolders(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].AutoDownload {
		t.Fatalf("default folder preferences = %+v, want auto-download disabled", listed)
	}

	if ok, err := st.SetFolderAutoDownload(ctx, accountID+1, folderID, true, 200); err != nil || ok {
		t.Fatalf("cross-account preference update = %v, %v; want false, nil", ok, err)
	}
	if ok, err := st.SetFolderAutoDownload(ctx, accountID, folderID, true, 200); err != nil || !ok {
		t.Fatalf("preference update = %v, %v; want true, nil", ok, err)
	}
	listed, err = st.ListFolders(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !listed[0].AutoDownload {
		t.Fatalf("updated folder preferences = %+v, want auto-download enabled", listed)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	listed, err = st.ListFolders(ctx, accountID)
	if err != nil || len(listed) != 1 || !listed[0].AutoDownload {
		t.Fatalf("preferences after reopening = %+v, err = %v", listed, err)
	}

	if _, err := st.db.ExecContext(ctx, `DELETE FROM folders WHERE id = ?`, folderID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM folder_preferences WHERE folder_id = ?`, folderID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("preferences after folder deletion = %d, want 0", count)
	}
}

func TestAutoDownloadEnqueueRechecksPreferenceAndPreservesEveryPartState(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accountID, err := st.UpsertAccount(ctx, "test-site", "1", "one", "", []byte("credential"), 100)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := st.ReconcileFolders(ctx, accountID, []FolderInput{{RemoteID: "folder", Title: "Folder"}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	folderID := folders["folder"]
	inputs := []DownloadTaskInput{}
	states := []string{"done", "pending", "running", "paused", "canceled", "failed", "missing"}
	for i := range states {
		inputs = append(inputs, DownloadTaskInput{RemoteID: "multipart", PartID: int64(i + 1), PageCount: 8})
	}
	if _, _, err := st.CreateDownloadTasks(ctx, accountID, 720, inputs, 100); err != nil {
		t.Fatal(err)
	}
	for i, state := range states {
		if _, err := st.db.ExecContext(ctx, `UPDATE download_tasks SET state = ? WHERE id = ?`, state, i+1); err != nil {
			t.Fatal(err)
		}
	}
	inputs = append(inputs, DownloadTaskInput{RemoteID: "multipart", PartID: 8, PageCount: 8})
	// This represents a switch being disabled while remote task construction was in progress.
	if _, err := st.SetFolderAutoDownload(ctx, accountID, folderID, false, 110); err != nil {
		t.Fatal(err)
	}
	if created, _, err := st.CreateAutoDownloadTasks(ctx, accountID, folderID, 1, 720, inputs, 120); err != nil || created != 0 {
		t.Fatalf("disabled created=%d err=%v", created, err)
	}
	if _, err := st.SetFolderAutoDownload(ctx, accountID, folderID, true, 130); err != nil {
		t.Fatal(err)
	}
	if created, skipped, err := st.CreateAutoDownloadTasks(ctx, accountID, folderID, 1, 720, inputs, 140); err != nil || created != 3 || skipped != 5 {
		t.Fatalf("created=%d skipped=%d err=%v", created, skipped, err)
	}
	for i, want := range states {
		if want == "failed" || want == "missing" {
			want = "pending"
		}
		task, err := st.GetDownloadTask(ctx, int64(i+1))
		if err != nil {
			t.Fatal(err)
		}
		if task.State != want {
			t.Errorf("part %d state=%q want=%q", i+1, task.State, want)
		}
	}
}

func TestCreateAutoDownloadTasksPreservesCanceledAndRetriesFailed(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accountID, err := st.UpsertAccount(ctx, "test-site", "1", "one", "", []byte("credential"), 100)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := st.ReconcileFolders(ctx, accountID, []FolderInput{{RemoteID: "folder", Title: "Folder"}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	folderID := folders["folder"]
	if _, err := st.SetFolderAutoDownload(ctx, accountID, folderID, true, 100); err != nil {
		t.Fatal(err)
	}
	inputs := []DownloadTaskInput{{RemoteID: "canceled"}, {RemoteID: "failed"}}
	if _, _, err := st.CreateDownloadTasks(ctx, accountID, 720, inputs, 100); err != nil {
		t.Fatal(err)
	}
	if err := st.CancelDownloadTask(ctx, 1, 110); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishDownloadTask(ctx, 2, "failed", "", "failed", 0, "", 0, 110); err != nil {
		t.Fatal(err)
	}

	created, skipped, err := st.CreateAutoDownloadTasks(ctx, accountID, folderID, 1, 720, inputs, 120)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || skipped != 1 {
		t.Fatalf("auto enqueue = created %d, skipped %d; want 1, 1", created, skipped)
	}
	for id, want := range map[int64]string{1: "canceled", 2: "pending"} {
		task, err := st.GetDownloadTask(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if task.State != want {
			t.Errorf("task %d state = %q, want %q", id, task.State, want)
		}
	}
}

func TestAutoDownloadEnqueueRejectsRunsBeforePreferenceWasEnabled(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accountID, err := st.UpsertAccount(ctx, "test-site", "1", "one", "", []byte("credential"), 100)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := st.ReconcileFolders(ctx, accountID, []FolderInput{{RemoteID: "folder", Title: "Folder"}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	folderID := folders["folder"]
	oldRun, err := st.CreateRun(ctx, accountID, "manual", "all", 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetFolderAutoDownload(ctx, accountID, folderID, true, 100); err != nil {
		t.Fatal(err)
	}
	inputs := []DownloadTaskInput{{RemoteID: "one", PartID: 1}}
	if created, skipped, err := st.CreateAutoDownloadTasks(ctx, accountID, folderID, oldRun, 720, inputs, 100); err != nil || created != 0 || skipped != 1 {
		t.Fatalf("old-run transaction created=%d skipped=%d err=%v", created, skipped, err)
	}
	nextRun, err := st.CreateRun(ctx, accountID, "manual", "all", 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetFolderAutoDownload(ctx, accountID, folderID, true, 100); err != nil {
		t.Fatal(err)
	}
	if created, _, err := st.CreateAutoDownloadTasks(ctx, accountID, folderID, nextRun, 720, inputs, 100); err != nil || created != 1 {
		t.Fatalf("next-run transaction created=%d err=%v", created, err)
	}
	if _, err := st.SetFolderAutoDownload(ctx, accountID, folderID, false, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetFolderAutoDownload(ctx, accountID, folderID, true, 100); err != nil {
		t.Fatal(err)
	}
	inputs = []DownloadTaskInput{{RemoteID: "two", PartID: 1}}
	if created, _, err := st.CreateAutoDownloadTasks(ctx, accountID, folderID, nextRun, 720, inputs, 100); err != nil || created != 0 {
		t.Fatalf("re-enabled transaction created=%d err=%v", created, err)
	}
}
