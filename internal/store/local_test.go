package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLocalStatusesAndFolderCounts(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "local.db"))
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
	for _, remoteID := range []string{"item_done", "item_part"} {
		if err := st.WithTx(ctx, func(tx *Tx) error {
			id, _, _, err := tx.UpsertItem(ctx, accID, ItemInput{
				RemoteID: remoteID, Kind: 2, Title: remoteID, URL: "https://example.com/" + remoteID,
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
	inputs := []DownloadTaskInput{
		{RemoteID: "item_done", PartID: 1},
		{RemoteID: "item_part", PartID: 2},
		{RemoteID: "item_part", PartID: 3},
	}
	created, skipped, err := st.CreateDownloadTasks(ctx, accID, 80, inputs, 100)
	if err != nil || created != 3 || skipped != 0 {
		t.Fatalf("create = %d/%d err=%v", created, skipped, err)
	}
	tasks, err := st.ListDownloadTasks(ctx, accID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	for i := range tasks {
		if tasks[i].RemoteID == "item_done" {
			if err := st.FinishDownloadTask(ctx, tasks[i].ID, "done", "/media/done.mp4", "", 80, "avc", 100, 110); err != nil {
				t.Fatal(err)
			}
		}
	}
	statuses, err := st.LocalStatuses(ctx, accID, []string{"item_done", "item_part", "item_none"})
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses["item_done"]; got.State != "done" || got.Done != 1 || got.Total != 1 || got.FilePath != "/media/done.mp4" {
		t.Fatalf("done status = %+v", got)
	}
	if got := statuses["item_part"]; got.State != "pending" || got.Done != 0 || got.Total != 2 {
		t.Fatalf("pending status = %+v", got)
	}
	if _, ok := statuses["item_none"]; ok {
		t.Fatalf("unexpected status for unknown remote_id")
	}
	counts, err := st.FolderLocalCounts(ctx, accID)
	if err != nil {
		t.Fatal(err)
	}
	if got := counts[ids["1"]]; got.Total != 2 || got.Done != 1 {
		t.Fatalf("folder counts = %+v", got)
	}
	done, err := st.DownloadedRemoteIDs(ctx, accID, []string{"item_done", "item_part"})
	if err != nil {
		t.Fatal(err)
	}
	if !done["item_done"] || done["item_part"] {
		t.Fatalf("downloaded remoteIDs = %v", done)
	}
}

func TestDownloadTasksShareOneCopyAcrossQuality(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "dedupe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accID, err := st.UpsertAccount(ctx, "synthetic-video", "42", "tester", "", []byte("secret"), 100)
	if err != nil {
		t.Fatal(err)
	}
	in := []DownloadTaskInput{{RemoteID: "item-1", PartID: 11}}
	if created, skipped, err := st.CreateDownloadTasks(ctx, accID, 80, in, 100); err != nil || created != 1 || skipped != 0 {
		t.Fatalf("first = %d/%d err=%v", created, skipped, err)
	}
	if created, skipped, err := st.CreateDownloadTasks(ctx, accID, 120, in, 101); err != nil || created != 0 || skipped != 1 {
		t.Fatalf("second quality = %d/%d err=%v", created, skipped, err)
	}
	tasks, err := st.ListDownloadTasks(ctx, accID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Quality != 80 {
		t.Fatalf("tasks = %+v", tasks)
	}
}
