package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestQueryDownloadTasksFiltersAndPagesWithinAccount(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "query-downloads.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	account, err := st.UpsertAccount(ctx, "synthetic-video", "query", "tester", "", []byte("cred"), 1)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := st.UpsertAccount(ctx, "synthetic-video", "foreign", "foreign", "", []byte("cred"), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, _, err := st.CreateDownloadTasks(ctx, account, 80, []DownloadTaskInput{{RemoteID: id, PartID: 1}}, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := st.CreateDownloadTasks(ctx, foreign, 80, []DownloadTaskInput{{RemoteID: "foreign", PartID: 1}}, 1); err != nil {
		t.Fatal(err)
	}
	all, total, err := st.QueryDownloadTasks(ctx, account, DownloadFilter{Page: 1, PageSize: 2})
	if err != nil || total != 3 || len(all) != 2 || all[0].RemoteID != "c" || all[1].RemoteID != "b" {
		t.Fatalf("first page=%+v total=%d err=%v", all, total, err)
	}
	second, total, err := st.QueryDownloadTasks(ctx, account, DownloadFilter{Page: 2, PageSize: 2})
	if err != nil || total != 3 || len(second) != 1 || second[0].RemoteID != "a" {
		t.Fatalf("second page=%+v total=%d err=%v", second, total, err)
	}
	filtered, total, err := st.QueryDownloadTasks(ctx, account, DownloadFilter{States: []string{"pending", "running"}, Page: 1, PageSize: 10})
	if err != nil || total != 3 || len(filtered) != 3 {
		t.Fatalf("filtered=%+v total=%d err=%v", filtered, total, err)
	}
	if _, total, err := st.QueryDownloadTasks(ctx, foreign, DownloadFilter{Page: 1, PageSize: 10}); err != nil || total != 1 {
		t.Fatalf("foreign total=%d err=%v", total, err)
	}
}

func TestPauseResumeCancelDownloadTask(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accID, err := st.UpsertAccount(ctx, "synthetic-video", "42", "tester", "", []byte("cred"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateDownloadTasks(ctx, accID, 80, []DownloadTaskInput{{RemoteID: "item-1", PartID: 1}}, 1); err != nil {
		t.Fatal(err)
	}
	task, err := st.ClaimDownloadTask(ctx, accID, 10)
	if err != nil || task == nil {
		t.Fatalf("claim: %v %+v", err, task)
	}
	if err := st.PauseDownloadTask(ctx, task.ID, 20); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetDownloadTask(ctx, task.ID)
	if got.State != "paused" {
		t.Fatalf("state = %s", got.State)
	}
	if err := st.ResumeDownloadTask(ctx, task.ID, 30); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetDownloadTask(ctx, task.ID)
	if got.State != "pending" || got.Attempts != 0 {
		t.Fatalf("resumed = %+v", got)
	}
	if err := st.CancelDownloadTask(ctx, task.ID, 40); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetDownloadTask(ctx, task.ID)
	if got.State != "canceled" {
		t.Fatalf("state = %s", got.State)
	}
}
func TestDownloadTaskLifecycle(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "downloads.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	accID, err := st.UpsertAccount(ctx, "synthetic-video", "42", "tester", "", []byte("secret"), 100)
	if err != nil {
		t.Fatal(err)
	}

	if err := st.SetSettings(ctx, map[string]string{
		"download.media_dir": "./media",
		"download.quality":   "80",
	}, 100); err != nil {
		t.Fatal(err)
	}
	values, err := st.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if values["download.quality"] != "80" {
		t.Fatalf("quality setting = %q", values["download.quality"])
	}

	inputs := []DownloadTaskInput{
		{RemoteID: "item-1", PartID: 11, VideoTitle: "视频", PageTitle: "P1", PageNo: 1, PageCount: 2},
		{RemoteID: "item-1", PartID: 12, VideoTitle: "视频", PageTitle: "P2", PageNo: 2, PageCount: 2},
	}
	created, skipped, err := st.CreateDownloadTasks(ctx, accID, 80, inputs, 100)
	if err != nil {
		t.Fatal(err)
	}
	if created != 2 || skipped != 0 {
		t.Fatalf("first enqueue created=%d skipped=%d", created, skipped)
	}
	created, skipped, err = st.CreateDownloadTasks(ctx, accID, 80, inputs, 101)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || skipped != 2 {
		t.Fatalf("duplicate enqueue created=%d skipped=%d", created, skipped)
	}

	task, err := st.ClaimDownloadTask(ctx, accID, 110)
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || task.State != "running" || task.Attempts != 1 {
		t.Fatalf("claimed task = %+v", task)
	}
	if err := st.UpdateDownloadProgress(ctx, task.ID, 0.5, 50, 100, 111); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishDownloadTask(ctx, task.ID, "done", "/media/video.mp4", "", 80, "avc", 100, 112); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetDownloadTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "done" || got.Progress != 1 || got.FilePath != "/media/video.mp4" {
		t.Fatalf("finished task = %+v", got)
	}

	next, err := st.ClaimDownloadTask(ctx, accID, 120)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.ID == task.ID {
		t.Fatalf("second claim = %+v", next)
	}
	counts, err := st.DownloadCounts(ctx, accID)
	if err != nil {
		t.Fatal(err)
	}
	if counts["done"] != 1 || counts["running"] != 1 {
		t.Fatalf("counts = %+v", counts)
	}

	if err := st.ResetDownloadTask(ctx, task.ID, 130); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetDownloadTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "pending" || got.Progress != 0 {
		t.Fatalf("reset task = %+v", got)
	}
}

func TestListFolderRemoteIDs(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "folder-remoteIDs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accID, err := st.UpsertAccount(ctx, "synthetic-video", "42", "tester", "", []byte("secret"), 100)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := st.ReconcileFolders(ctx, accID, []FolderInput{{RemoteID: "1", Title: "收藏夹"}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, remote_id := range []string{"item-1", "item-2"} {
		if err := st.WithTx(ctx, func(tx *Tx) error {
			id, _, _, err := tx.UpsertItem(ctx, accID, ItemInput{
				RemoteID: remote_id, Kind: 2, Title: remote_id, URL: "https://media.example.test/video/" + remote_id,
			}, 100)
			if err != nil {
				return err
			}
			_, err = tx.UpsertFolderItem(ctx, ids["1"], id, 100)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	remoteIDs, err := st.ListFolderRemoteIDs(ctx, accID, ids["1"])
	if err != nil {
		t.Fatal(err)
	}
	if len(remoteIDs) != 2 || remoteIDs[0] != "item-1" || remoteIDs[1] != "item-2" {
		t.Fatalf("folder remoteIDs = %v", remoteIDs)
	}
}

func TestCreateDirectDownloadTaskStoresSiteAndPageURL(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "direct.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accID, err := st.UpsertAccount(ctx, "synthetic-direct", "7", "tester", "", []byte("secret"), 100)
	if err != nil {
		t.Fatal(err)
	}
	input := DownloadTaskInput{
		Site:       "synthetic-direct",
		PageURL:    "https://media.example.test/video/1/example/",
		RemoteID:   "1",
		VideoTitle: "Example",
		PageNo:     1,
		PageCount:  1,
	}
	if created, skipped, err := st.CreateDownloadTasks(ctx, accID, 720, []DownloadTaskInput{input}, 100); err != nil || created != 1 || skipped != 0 {
		t.Fatalf("create = %d/%d, %v", created, skipped, err)
	}
	task, err := st.ClaimDownloadTask(ctx, accID, 110)
	if err != nil || task == nil {
		t.Fatal(err)
	}
	if task.Site != input.Site || task.PageURL != input.PageURL || task.PartID != 0 || task.Quality != 720 {
		t.Fatalf("direct task = %+v", task)
	}
}
