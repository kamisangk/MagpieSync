package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestItemLifecycleAndIdempotency(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	accID, err := st.UpsertAccount(ctx, "synthetic-video", "42", "tester", "", []byte("secret"), 100)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := st.ReconcileFolders(ctx, accID, []FolderInput{{RemoteID: "1", Title: "默认收藏夹", RemoteCount: 2}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	folderID := ids["1"]
	if folderID == 0 {
		t.Fatal("folder id is zero")
	}

	in := ItemInput{
		RemoteID: "item-xx", Kind: 2, Title: "测试标题", URL: "https://media.example.test/video/item-xx",
		CoverURL: "https://example.invalid/cover.jpg", Duration: 120, AuthorID: "7", AuthorName: "UP主",
		Summary: "简介", PublishedAt: 90, Attr: 0,
	}

	var itemID int64
	err = st.WithTx(ctx, func(tx *Tx) error {
		id, isNew, changed, err := tx.UpsertItem(ctx, accID, in, 100)
		if err != nil {
			return err
		}
		if !isNew || changed {
			t.Fatalf("first upsert: isNew=%v changed=%v", isNew, changed)
		}
		itemID = id
		prev, err := tx.UpsertFolderItem(ctx, folderID, id, 50)
		if err != nil {
			return err
		}
		if prev != "" {
			t.Fatalf("first link state = %q", prev)
		}
		return tx.RecomputeItemStates(ctx, accID)
	})
	if err != nil {
		t.Fatal(err)
	}

	err = st.WithTx(ctx, func(tx *Tx) error {
		_, isNew, changed, err := tx.UpsertItem(ctx, accID, in, 200)
		if err != nil {
			return err
		}
		if isNew || changed {
			t.Fatalf("second upsert: isNew=%v changed=%v", isNew, changed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	item, err := st.GetItem(ctx, accID, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if item.UpdatedAt != 100 {
		t.Fatalf("unchanged item updated_at = %d, want 100", item.UpdatedAt)
	}
	if item.LastSeenAt != 200 {
		t.Fatalf("last_seen_at = %d, want 200", item.LastSeenAt)
	}
	if item.State != "active" {
		t.Fatalf("state = %q, want active", item.State)
	}
	if len(item.Folders) != 1 || item.Folders[0].ID != folderID {
		t.Fatalf("folders = %+v", item.Folders)
	}

	step := func(now int64) string {
		var kind string
		err := st.WithTx(ctx, func(tx *Tx) error {
			k, err := tx.MarkLinkMissing(ctx, folderID, itemID, 2, now)
			kind = k
			if err != nil {
				return err
			}
			return tx.RecomputeItemStates(ctx, accID)
		})
		if err != nil {
			t.Fatal(err)
		}
		return kind
	}

	if got := step(300); got != "missing" {
		t.Fatalf("first absence = %q, want missing", got)
	}
	item, _ = st.GetItem(ctx, accID, itemID)
	if item.State != "missing" {
		t.Fatalf("state after first absence = %q", item.State)
	}

	if got := step(400); got != "removed" {
		t.Fatalf("second absence = %q, want removed", got)
	}
	item, _ = st.GetItem(ctx, accID, itemID)
	if item.State != "removed" {
		t.Fatalf("state after second absence = %q", item.State)
	}

	err = st.WithTx(ctx, func(tx *Tx) error {
		prev, err := tx.UpsertFolderItem(ctx, folderID, itemID, 500)
		if err != nil {
			return err
		}
		if prev != "removed" {
			t.Fatalf("prev state on restore = %q, want removed", prev)
		}
		return tx.RecomputeItemStates(ctx, accID)
	})
	if err != nil {
		t.Fatal(err)
	}
	item, _ = st.GetItem(ctx, accID, itemID)
	if item.State != "active" {
		t.Fatalf("state after restore = %q", item.State)
	}

	items, total, err := st.QueryItems(ctx, accID, ItemFilter{Query: "测试", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("search total=%d items=%d", total, len(items))
	}
}

func TestReconcileFoldersArchivesMissing(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accID, _ := st.UpsertAccount(ctx, "synthetic-video", "1", "u", "", []byte("x"), 1)

	if _, err := st.ReconcileFolders(ctx, accID, []FolderInput{{RemoteID: "1", Title: "A"}, {RemoteID: "2", Title: "B"}}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReconcileFolders(ctx, accID, []FolderInput{{RemoteID: "1", Title: "A"}}, 2); err != nil {
		t.Fatal(err)
	}
	folders, err := st.ListFolders(ctx, accID)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, f := range folders {
		states[f.RemoteID] = f.State
	}
	if states["1"] != "active" || states["2"] != "archived" {
		t.Fatalf("states = %+v", states)
	}
}
