package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestGlobalOverviewAndExportAcrossAccounts(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	acc1, err := st.UpsertAccount(ctx, "synthetic-video", "1001", "alice", "", []byte("cred-a"), 100)
	if err != nil {
		t.Fatal(err)
	}
	acc2, err := st.UpsertAccount(ctx, "synthetic-direct", "1002", "bob", "", []byte("cred-b"), 101)
	if err != nil {
		t.Fatal(err)
	}

	f1, err := st.ReconcileFolders(ctx, acc1, []FolderInput{{RemoteID: "fav-1", Title: "默认收藏夹", RemoteCount: 1}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := st.ReconcileFolders(ctx, acc2, []FolderInput{{RemoteID: "fav-2", Title: "Favourites", RemoteCount: 1}}, 101)
	if err != nil {
		t.Fatal(err)
	}

	for i, acc := range []int64{acc1, acc2} {
		remote := []string{"item-a", "item-b"}[i]
		in := ItemInput{RemoteID: remote, Kind: 2, Title: remote, URL: "https://example.invalid/" + remote}
		folderID := []int64{f1["fav-1"], f2["fav-2"]}[i]
		if err := st.WithTx(ctx, func(tx *Tx) error {
			itemID, _, _, err := tx.UpsertItem(ctx, acc, in, 110)
			if err != nil {
				return err
			}
			if _, err := tx.UpsertFolderItem(ctx, folderID, itemID, 50); err != nil {
				return err
			}
			return tx.RecomputeItemStates(ctx, acc)
		}); err != nil {
			t.Fatal(err)
		}
	}

	run1, err := st.CreateRun(ctx, acc1, "manual", "all", 200)
	if err != nil {
		t.Fatal(err)
	}
	run2, err := st.CreateRun(ctx, acc2, "scheduled", "folder:fav-2", 201)
	if err != nil {
		t.Fatal(err)
	}

	folders, err := st.CountFoldersAll(ctx)
	if err != nil || folders != 2 {
		t.Fatalf("CountFoldersAll = %d, %v; want 2", folders, err)
	}
	items, err := st.CountItemsAll(ctx)
	if err != nil || items != 2 {
		t.Fatalf("CountItemsAll = %d, %v; want 2", items, err)
	}
	if n, err := st.CountDownloadedVideosAll(ctx); err != nil || n != 0 {
		t.Fatalf("CountDownloadedVideosAll = %d, %v; want 0", n, err)
	}

	runs, err := st.ListRunsAll(ctx, 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("ListRunsAll = %+v, %v; want 2 runs", runs, err)
	}
	if runs[0].ID != run2 || runs[1].ID != run1 {
		t.Fatalf("run order = %d,%d; want %d,%d", runs[0].ID, runs[1].ID, run2, run1)
	}
	last, err := st.LastRunAll(ctx)
	if err != nil || last == nil || last.ID != run2 {
		t.Fatalf("LastRunAll = %+v, %v; want run %d", last, err, run2)
	}
	run, err := st.GetRunAll(ctx, run1)
	if err != nil || run.AccountID != acc1 {
		t.Fatalf("GetRunAll = %+v, %v", run, err)
	}

	allFolders, err := st.AllFoldersAll(ctx)
	if err != nil || len(allFolders) != 2 {
		t.Fatalf("AllFoldersAll = %d, %v; want 2", len(allFolders), err)
	}
	allItems, err := st.AllItemsAll(ctx)
	if err != nil || len(allItems) != 2 {
		t.Fatalf("AllItemsAll = %d, %v; want 2", len(allItems), err)
	}
	links, err := st.AllFolderItemsAll(ctx)
	if err != nil || len(links) != 2 {
		t.Fatalf("AllFolderItemsAll = %d, %v; want 2", len(links), err)
	}
}
