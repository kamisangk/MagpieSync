package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func statusStore(t *testing.T) (*Store, int64, map[string]int64) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "status.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	account, err := st.UpsertAccount(context.Background(), "synthetic-status", "one", "One", "", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := st.ReconcileFolders(context.Background(), account, []FolderInput{{RemoteID: "A", Title: "A"}, {RemoteID: "B", Title: "B"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	return st, account, folders
}

func statusItem(t *testing.T, st *Store, account, folder int64, remote, state string) int64 {
	t.Helper()
	var id int64
	err := st.WithTx(context.Background(), func(tx *Tx) error {
		var err error
		id, _, _, err = tx.UpsertItem(context.Background(), account, ItemInput{RemoteID: remote, Title: remote}, 1)
		if err != nil {
			return err
		}
		if folder > 0 {
			if _, err = tx.UpsertFolderItem(context.Background(), folder, id, 1); err != nil {
				return err
			}
		}
		_, err = tx.tx.Exec(`UPDATE folder_items SET state=? WHERE item_id=? AND folder_id=?`, state, id, folder)
		if err != nil {
			return err
		}
		return tx.RecomputeItemStates(context.Background(), account)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func statusTask(t *testing.T, st *Store, account int64, remote string, part int, state, path string, pages int) {
	t.Helper()
	_, err := st.db.Exec(`INSERT INTO download_tasks(account_id,remote_id,part_id,state,file_path,page_count,created_at,updated_at) VALUES(?,?,?,?,?,?,1,1)`, account, remote, part, state, path, pages)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRemovedMembershipRetentionAndSummary(t *testing.T) {
	st, account, folders := statusStore(t)
	ctx := context.Background()
	cases := []struct {
		remote, state, path string
		visible             bool
	}{
		{"never", "", "", false}, {"done", "done", "", true}, {"lost", "missing", "", true},
		{"pending", "pending", "", true}, {"running", "running", "", true}, {"paused", "paused", "", true},
		{"failed", "failed", "", false}, {"canceled", "canceled", "", false}, {"output", "failed", "/recorded.mp4", true},
	}
	for _, tc := range cases {
		id := statusItem(t, st, account, folders["A"], tc.remote, "removed")
		if tc.state != "" {
			statusTask(t, st, account, tc.remote, 1, tc.state, tc.path, 1)
		}
		for _, folder := range []int64{0, folders["A"]} {
			got, total, err := st.QueryItems(ctx, account, ItemFilter{FolderID: folder, Query: tc.remote})
			if err != nil {
				t.Fatal(err)
			}
			if (total > 0) != tc.visible || len(got) != total {
				t.Fatalf("%s folder=%d items=%+v total=%d", tc.remote, folder, got, total)
			}
			if total > 0 && got[0].RemoteState != "removed" {
				t.Fatal(got[0])
			}
		}
		if _, err := st.GetItem(ctx, account, id); err != nil {
			t.Fatal("history deleted", err)
		}
	}
	count, err := st.CountItems(ctx, account)
	if err != nil || count != 6 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	all, err := st.CountItemsAll(ctx)
	if err != nil || all != count {
		t.Fatalf("all=%d err=%v", all, err)
	}
	fs, err := st.ListFolders(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	counts, err := st.FolderLocalCounts(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	if fs[0].ItemCount != 6 || counts[folders["A"]].Total != 6 || counts[folders["A"]].Done != 1 {
		t.Fatalf("folders=%+v counts=%+v", fs, counts)
	}
	raw, err := st.AllItems(ctx, account)
	if err != nil || len(raw) != len(cases) {
		t.Fatalf("raw=%d err=%v", len(raw), err)
	}
}

func TestRemoteContextIsolationRestorationAndOrphans(t *testing.T) {
	st, account, folders := statusStore(t)
	ctx := context.Background()
	id := statusItem(t, st, account, folders["A"], "shared", "removed")
	statusItem(t, st, account, folders["B"], "shared", "active")
	statusTask(t, st, account, "shared", 1, "missing", "/lost.mp4", 1)
	for _, tc := range []struct {
		folder int64
		remote string
	}{{folders["A"], "removed"}, {folders["B"], "active"}, {0, "active"}} {
		rows, total, err := st.QueryItems(ctx, account, ItemFilter{FolderID: tc.folder, RemoteState: tc.remote, LocalState: "missing"})
		if err != nil || total != 1 || rows[0].State != "active" || rows[0].RemoteState != tc.remote {
			t.Fatalf("%+v rows=%+v total=%d err=%v", tc, rows, total, err)
		}
	}
	statusItem(t, st, account, folders["A"], "shared", "active")
	rows, total, err := st.QueryItems(ctx, account, ItemFilter{FolderID: folders["A"], RemoteState: "active"})
	if err != nil || total != 1 || rows[0].ID != id {
		t.Fatal(rows, total, err)
	}
	other, err := st.UpsertAccount(ctx, "synthetic-status", "other", "Other", "", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	statusItem(t, st, account, folders["A"], "foreign-evidence", "removed")
	statusTask(t, st, other, "foreign-evidence", 1, "done", "/other.mp4", 1)
	_, total, err = st.QueryItems(ctx, account, ItemFilter{Query: "foreign-evidence"})
	if err != nil || total != 0 {
		t.Fatal(total, err)
	}
	orphan := statusItem(t, st, account, folders["A"], "orphan", "removed")
	if _, err := st.db.Exec(`DELETE FROM folder_items WHERE item_id=?`, orphan); err != nil {
		t.Fatal(err)
	}
	_, total, err = st.QueryItems(ctx, account, ItemFilter{Query: "orphan"})
	if err != nil || total != 1 {
		t.Fatal(total, err)
	}
}

func TestLocalFiltersShareAggregationBeforePagination(t *testing.T) {
	st, account, folders := statusStore(t)
	ctx := context.Background()
	cases := []struct {
		remote  string
		states  []string
		pages   int
		want    string
		missing int
	}{
		{"none", nil, 1, "none", 0}, {"canceled", []string{"canceled"}, 1, "none", 0},
		{"done", []string{"done"}, 1, "done", 0}, {"expected-parts", []string{"done"}, 2, "partial", 0},
		{"lost", []string{"missing"}, 1, "missing", 1}, {"mixed", []string{"done", "missing"}, 2, "partial", 1},
		{"failed", []string{"failed"}, 1, "failed", 0}, {"failed-lost", []string{"failed", "missing"}, 2, "missing", 1},
		{"running", []string{"done", "running"}, 2, "running", 0}, {"pending", []string{"missing", "pending"}, 2, "pending", 1},
		{"paused", []string{"done", "paused"}, 2, "paused", 0},
	}
	ids := []string{}
	for _, tc := range cases {
		statusItem(t, st, account, folders["A"], tc.remote, "missing")
		ids = append(ids, tc.remote)
		for p, state := range tc.states {
			statusTask(t, st, account, tc.remote, p, state, "", tc.pages)
		}
	}
	statuses, err := st.LocalStatuses(ctx, account, ids)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int{}
	for _, tc := range cases {
		expected[tc.want]++
		status := statuses[tc.remote]
		if tc.states != nil && (status.State != tc.want || status.Missing != tc.missing) {
			t.Fatalf("%s=%+v", tc.remote, status)
		}
	}
	for state, want := range expected {
		seen := map[int64]bool{}
		for page := 1; page <= want; page++ {
			rows, total, err := st.QueryItems(ctx, account, ItemFilter{FolderID: folders["A"], RemoteState: "missing", LocalState: state, PageSize: 1, Page: page})
			if err != nil || total != want || len(rows) != 1 {
				t.Fatalf("%s page %d=%+v total=%d err=%v", state, page, rows, total, err)
			}
			if seen[rows[0].ID] {
				t.Fatal("duplicate page")
			}
			seen[rows[0].ID] = true
		}
	}
	for i := 0; i < 7; i++ {
		remote := fmt.Sprintf("page-%d", i)
		statusItem(t, st, account, folders["A"], remote, "removed")
		statusTask(t, st, account, remote, 1, "missing", "", 1)
	}
	rows, total, err := st.QueryItems(ctx, account, ItemFilter{RemoteState: "removed", LocalState: "missing", Page: 3, PageSize: 3})
	if err != nil || total != 7 || len(rows) != 1 {
		t.Fatal(rows, total, err)
	}
	done, err := st.CountDownloadedVideos(ctx, account)
	if err != nil || done != 1 {
		t.Fatal(done, err)
	}
}

func TestLocalCompletenessCountsUnrepresentedPartsAcrossQualities(t *testing.T) {
	st, account, folders := statusStore(t)
	ctx := context.Background()
	statusItem(t, st, account, folders["A"], "multi-quality", "active")
	statusTask(t, st, account, "multi-quality", 1, "done", "/one.mp4", 2)
	if _, err := st.db.Exec(`INSERT INTO download_tasks(account_id,remote_id,part_id,quality,state,file_path,page_count,created_at,updated_at) VALUES(?, 'multi-quality', 1, 720, 'done', '/one-other-quality.mp4', 2, 1, 1)`, account); err != nil {
		t.Fatal(err)
	}
	check := func(want string, wantDone, wantTotal int) {
		t.Helper()
		statuses, err := st.LocalStatuses(ctx, account, []string{"multi-quality"})
		status := statuses["multi-quality"]
		if err != nil || status.State != want || status.Done != wantDone || status.Total != wantTotal {
			t.Fatalf("status=%+v err=%v", status, err)
		}
		_, count, err := st.QueryItems(ctx, account, ItemFilter{FolderID: folders["A"], LocalState: want})
		if err != nil || count != 1 {
			t.Fatalf("filter=%d err=%v", count, err)
		}
		expectedBackups := 0
		if want == "done" {
			expectedBackups = 1
		}
		counts, err := st.FolderLocalCounts(ctx, account)
		if err != nil || counts[folders["A"]].Done != expectedBackups {
			t.Fatalf("folder counts=%+v err=%v", counts, err)
		}
		accountDone, err := st.CountDownloadedVideos(ctx, account)
		if err != nil || accountDone != expectedBackups {
			t.Fatalf("account count=%d err=%v", accountDone, err)
		}
		globalDone, err := st.CountDownloadedVideosAll(ctx)
		if err != nil || globalDone != expectedBackups {
			t.Fatalf("global count=%d err=%v", globalDone, err)
		}
	}
	check("partial", 2, 3)
	statusTask(t, st, account, "multi-quality", 2, "done", "/two.mp4", 2)
	check("done", 3, 3)
}
