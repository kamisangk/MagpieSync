package sync

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"magpiesync/internal/store"
)

type fakeSource struct {
	folders []RemoteFolder
	items   map[string][]RemoteItem
}

func (f *fakeSource) ListFolders(context.Context) ([]RemoteFolder, error) {
	return f.folders, nil
}

func (f *fakeSource) ListFolderPage(_ context.Context, remoteID string, page, pageSize int) (RemotePage, error) {
	all := f.items[remoteID]
	start := (page - 1) * pageSize
	if start >= len(all) {
		return RemotePage{}, nil
	}
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	return RemotePage{Items: all[start:end], HasMore: end < len(all)}, nil
}

func item(id, title string) RemoteItem {
	return RemoteItem{
		RemoteID: id, Kind: 2, Title: title,
		URL: "https://media.example.test/video/" + id, AuthorID: "1", AuthorName: "up",
		PublishedAt: 100, FavoritedAt: 200,
	}
}

func waitForRun(t *testing.T, st *store.Store, engine *Engine, accountID int64) *store.Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for engine.IsRunning(accountID) {
		if time.Now().After(deadline) {
			t.Fatal("sync did not finish in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, err := st.LastRun(context.Background(), accountID)
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("no run recorded")
	}
	return run
}

func TestEnginePersistsSourceKind(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "source-kind.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accID, err := st.UpsertAccount(ctx, "synthetic-video", "42", "tester", "", []byte("cred"), 1)
	if err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{
		folders: []RemoteFolder{{RemoteID: "watch_later", Title: "稍后再看", Kind: "watch_later", Count: 1}},
		items:   map[string][]RemoteItem{"watch_later": {item("item-1", "first")}},
	}
	engine := NewEngine(st, func(context.Context, int64) (Source, error) { return src, nil })
	run := mustRun(t, st, engine, accID)
	if run.State != "success" || run.ItemsAdded != 1 {
		t.Fatalf("run = %+v", run)
	}
	folders, err := st.ListFolders(ctx, accID)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].Kind != "watch_later" {
		t.Fatalf("folders = %+v", folders)
	}
}

func TestEngineMissingRemovedAndRestore(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	accID, err := st.UpsertAccount(ctx, "synthetic-video", "42", "tester", "", []byte("cred"), 1)
	if err != nil {
		t.Fatal(err)
	}

	src := &fakeSource{
		folders: []RemoteFolder{{RemoteID: "1", Title: "默认", Count: 2}},
		items: map[string][]RemoteItem{
			"1": {item("item-1", "first"), item("item-2", "second")},
		},
	}
	engine := NewEngine(st, func(context.Context, int64) (Source, error) { return src, nil })
	engine.SetGrace(2)

	run1 := mustRun(t, st, engine, accID)
	if run1.State != "success" || run1.ItemsAdded != 2 || run1.ItemsSeen != 2 {
		t.Fatalf("run1 = %+v", run1)
	}

	src.items["1"] = []RemoteItem{item("item-1", "first")}
	run2 := mustRun(t, st, engine, accID)
	if run2.ItemsMissing != 1 || run2.ItemsRemoved != 0 {
		t.Fatalf("run2 = %+v", run2)
	}
	folders, _ := st.ListFolders(ctx, accID)
	items, _, err := st.QueryItems(ctx, accID, store.ItemFilter{FolderID: folders[0].ID, State: "missing", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].RemoteID != "item-2" {
		t.Fatalf("missing items = %+v", items)
	}

	run3 := mustRun(t, st, engine, accID)
	if run3.ItemsRemoved != 1 {
		t.Fatalf("run3 = %+v", run3)
	}
	items, _, _ = st.QueryItems(ctx, accID, store.ItemFilter{FolderID: folders[0].ID, State: "removed", Page: 1, PageSize: 10})
	if len(items) != 0 {
		t.Fatalf("removed items = %+v", items)
	}
	history, err := st.AllItems(ctx, accID)
	if err != nil || len(history) != 2 || history[1].State != "removed" {
		t.Fatalf("history=%+v err=%v", history, err)
	}

	src.items["1"] = []RemoteItem{item("item-1", "first"), item("item-2", "second")}
	run4 := mustRun(t, st, engine, accID)
	if run4.State != "success" || run4.ItemsAdded != 0 {
		t.Fatalf("run4 = %+v", run4)
	}
	items, _, _ = st.QueryItems(ctx, accID, store.ItemFilter{FolderID: folders[0].ID, State: "active", Page: 1, PageSize: 10})
	if len(items) != 2 {
		t.Fatalf("active items after restore = %+v", items)
	}
}

func mustRun(t *testing.T, st *store.Store, engine *Engine, accountID int64) *store.Run {
	t.Helper()
	if _, err := engine.Start(accountID, "test", "all"); err != nil {
		t.Fatal(err)
	}
	return waitForRun(t, st, engine, accountID)
}

func TestEngineGuardSkipsRemovalOnSharpDrop(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accID, _ := st.UpsertAccount(ctx, "synthetic-video", "42", "tester", "", []byte("cred"), 1)

	first := make([]RemoteItem, 0, 24)
	for i := 0; i < 24; i++ {
		first = append(first, item("BV"+string(rune('A'+i)), "item"))
	}
	src := &fakeSource{
		folders: []RemoteFolder{{RemoteID: "1", Title: "big", Count: 24}},
		items:   map[string][]RemoteItem{"1": first},
	}
	engine := NewEngine(st, func(context.Context, int64) (Source, error) { return src, nil })
	engine.SetGrace(2)

	if run := mustRun(t, st, engine, accID); run.ItemsAdded != 24 {
		t.Fatalf("first run = %+v", run)
	}

	src.items["1"] = first[:5]
	run := mustRun(t, st, engine, accID)
	if run.State != "suspicious" || run.ItemsMissing != 0 || run.ItemsRemoved != 0 {
		t.Fatalf("guard run = %+v", run)
	}
	_, total, err := st.QueryItems(ctx, accID, store.ItemFilter{RemoteState: "active"})
	if err != nil || total != 24 {
		t.Fatalf("guard visibility=%d err=%v", total, err)
	}
}

type interruptedPages struct {
	*fakeSource
	fail bool
}

func (s *interruptedPages) ListFolderPage(ctx context.Context, remote string, page, size int) (RemotePage, error) {
	if s.fail && remote == "A" {
		if page == 1 {
			return RemotePage{Items: []RemoteItem{item("stay", "Stay")}, HasMore: true}, nil
		}
		return RemotePage{}, errors.New("fixture pagination unavailable")
	}
	return s.fakeSource.ListFolderPage(ctx, remote, page, size)
}

func TestRemoteRemovalRetainsOutputsAndWorkAcrossPartialSync(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "retention.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	account, err := st.UpsertAccount(ctx, "synthetic-status", "1", "One", "", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	initial := []RemoteItem{item("stay", "Stay"), item("never", "Never"), item("downloaded", "Downloaded"), item("pending", "Pending"), item("shared", "Shared")}
	src := &interruptedPages{fakeSource: &fakeSource{folders: []RemoteFolder{{RemoteID: "A", Title: "A"}, {RemoteID: "B", Title: "B"}}, items: map[string][]RemoteItem{"A": initial, "B": {item("shared", "Shared")}}}}
	engine := NewEngine(st, func(context.Context, int64) (Source, error) { return src, nil })
	engine.SetGrace(2)
	if run := mustRun(t, st, engine, account); run.State != "success" {
		t.Fatal(run)
	}
	if _, _, err := st.CreateDownloadTasks(ctx, account, 0, []store.DownloadTaskInput{{RemoteID: "downloaded", PartID: 1}, {RemoteID: "pending", PartID: 1}, {RemoteID: "shared", PartID: 1}}, 1); err != nil {
		t.Fatal(err)
	}
	tasks, err := st.ListDownloadTasks(ctx, account, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.RemoteID != "pending" {
			if err := st.FinishDownloadTask(ctx, task.ID, "done", "/fixture/"+task.RemoteID+".mp4", "", 0, "", 1, 2); err != nil {
				t.Fatal(err)
			}
		}
	}
	folders, err := st.ListFolders(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	a, b := folders[0].ID, folders[1].ID
	src.items["A"] = initial[:1]
	if run := mustRun(t, st, engine, account); run.ItemsMissing != 4 || run.ItemsRemoved != 0 {
		t.Fatal(run)
	}
	_, total, err := st.QueryItems(ctx, account, store.ItemFilter{FolderID: a, RemoteState: "missing"})
	if err != nil || total != 4 {
		t.Fatal(total, err)
	}
	src.fail = true
	for i := 0; i < 2; i++ {
		if run := mustRun(t, st, engine, account); run.State != "partial" || run.ItemsRemoved != 0 {
			t.Fatal(run)
		}
	}
	_, total, err = st.QueryItems(ctx, account, store.ItemFilter{FolderID: a, RemoteState: "missing"})
	if err != nil || total != 4 {
		t.Fatal(total, err)
	}
	src.fail = false
	if run := mustRun(t, st, engine, account); run.ItemsRemoved != 4 {
		t.Fatal(run)
	}
	rows, total, err := st.QueryItems(ctx, account, store.ItemFilter{FolderID: a, RemoteState: "removed"})
	if err != nil || total != 3 {
		t.Fatal(rows, total, err)
	}
	for _, it := range rows {
		if it.RemoteID == "never" {
			t.Fatal("never downloaded retained")
		}
		if it.RemoteID == "shared" && it.State != "active" {
			t.Fatal(it)
		}
	}
	rows, total, err = st.QueryItems(ctx, account, store.ItemFilter{FolderID: b, RemoteState: "active"})
	if err != nil || total != 1 || rows[0].RemoteID != "shared" {
		t.Fatal(rows, total, err)
	}
	after, err := st.ListDownloadTasks(ctx, account, "", 20)
	if err != nil || len(after) != len(tasks) {
		t.Fatal(after, err)
	}
	for _, task := range after {
		want := "done"
		if task.RemoteID == "pending" {
			want = "pending"
		}
		if task.State != want {
			t.Fatal(task)
		}
	}
	src.items["A"] = initial
	if run := mustRun(t, st, engine, account); run.ItemsAdded != 0 {
		t.Fatal(run)
	}
	_, total, err = st.QueryItems(ctx, account, store.ItemFilter{FolderID: a, RemoteState: "active"})
	if err != nil || total != 5 {
		t.Fatal(total, err)
	}
}

type failingSource struct{ err error }

func (f *failingSource) ListFolders(context.Context) ([]RemoteFolder, error) { return nil, f.err }
func (f *failingSource) ListFolderPage(context.Context, string, int, int) (RemotePage, error) {
	return RemotePage{}, f.err
}

func TestEngineMarksAccountNeedsReauthOnAuthError(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accID, err := st.UpsertAccount(ctx, "synthetic-direct", "42", "tester", "", []byte("cred"), 1)
	if err != nil {
		t.Fatal(err)
	}
	authErr := errors.New("login expired")
	engine := NewEngine(st, func(context.Context, int64) (Source, error) {
		return &failingSource{err: authErr}, nil
	})
	engine.SetAuthClassifier(func(err error) bool { return errors.Is(err, authErr) })
	run := mustRun(t, st, engine, accID)
	if run.State != "failed" {
		t.Fatalf("run state = %s, want failed", run.State)
	}
	account, err := st.GetAccountByID(ctx, accID)
	if err != nil {
		t.Fatal(err)
	}
	if account.State != "needs_reauth" {
		t.Fatalf("account state = %s, want needs_reauth", account.State)
	}
}
