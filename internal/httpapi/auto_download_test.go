package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"magpiesync/internal/config"
	"magpiesync/internal/download"
	"magpiesync/internal/sites"
	"magpiesync/internal/store"
	syncpkg "magpiesync/internal/sync"
	"magpiesync/internal/vault"
)

const autoTestSite = "synthetic-auto-download"

var (
	autoTestAuthError  = errors.New("authentication expired")
	autoTestFatalError = errors.New("risk control")
)

type autoTestDriver struct{}

func (autoTestDriver) SiteID() string         { return autoTestSite }
func (autoTestDriver) IsAuth(err error) bool  { return errors.Is(err, autoTestAuthError) }
func (autoTestDriver) IsFatal(err error) bool { return errors.Is(err, autoTestFatalError) }
func (autoTestDriver) CookieLogin(context.Context, sites.CookieInput) (*sites.LoginResult, error) {
	return nil, nil
}
func (autoTestDriver) NewSource(context.Context, sites.AccountContext) (syncpkg.Source, error) {
	return nil, nil
}
func (autoTestDriver) BuildTasks(_ context.Context, _ sites.AccountContext, req sites.TaskRequest) ([]store.DownloadTaskInput, error) {
	var tasks []store.DownloadTaskInput
	for _, item := range req.Items {
		if item.RemoteID == "auth-error" {
			return nil, autoTestAuthError
		}
		if item.RemoteID == "fatal-error" {
			return nil, autoTestFatalError
		}
		if item.RemoteID == "unavailable" {
			return nil, errors.New("video unavailable")
		}
		parts := 1
		if item.RemoteID == "multipart" {
			parts = 2
		}
		for p := 1; p <= parts; p++ {
			tasks = append(tasks, store.DownloadTaskInput{Site: autoTestSite, RemoteID: item.RemoteID, PartID: int64(p), PageCount: parts, PageNo: p, VideoTitle: item.Title, SourceTitle: req.SourceTitle})
		}
	}
	return tasks, nil
}

var registerAutoTestSite sync.Once

func autoTestServer(t *testing.T) (*Server, int64, map[string]int64) {
	t.Helper()
	registerAutoTestSite.Do(func() {
		sites.Register(sites.Site{ID: autoTestSite, Name: "Auto test", Download: true})
		sites.RegisterDriver(autoTestSite, autoTestDriver{})
		sites.Register(sites.Site{ID: "synthetic-no-download", Name: "No download"})
	})
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	v, err := vault.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	credential, err := v.Encrypt([]byte("credential"), credentialAAD(autoTestSite, "1"))
	if err != nil {
		t.Fatal(err)
	}
	accountID, err := st.UpsertAccount(context.Background(), autoTestSite, "1", "one", "", credential, 100)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := st.ReconcileFolders(context.Background(), accountID, []store.FolderInput{{RemoteID: "one", Title: "One"}, {RemoteID: "two", Title: "Two"}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	s := New(config.Config{}, st, v, syncpkg.NewEngine(st, nil), NewHub())
	s.SetDownloadManager(download.NewManager(st, nil, nil, nil, nil))
	return s, accountID, folders
}

func TestFolderAutoDownloadAPIPersistsWithoutQueueing(t *testing.T) {
	s, accountID, folders := autoTestServer(t)
	request := func(id int64, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/folders/%d/auto-download", id), strings.NewReader(body))
		r.SetPathValue("id", fmt.Sprint(id))
		w := httptest.NewRecorder()
		s.handleFolderAutoDownload(w, r)
		return w
	}
	if w := request(folders["one"], `{"auto_download":true}`); w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	f, err := s.store.ListFolders(context.Background(), accountID)
	if err != nil {
		t.Fatal(err)
	}
	if !f[0].AutoDownload || f[1].AutoDownload {
		t.Fatalf("preferences=%+v", f)
	}
	if tasks, err := s.store.ListDownloadTasks(context.Background(), accountID, "", 100); err != nil || len(tasks) != 0 {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	if w := request(folders["one"], `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("missing boolean status=%d", w.Code)
	}
	if w := request(999, `{"auto_download":true}`); w.Code != http.StatusNotFound {
		t.Fatalf("foreign folder status=%d", w.Code)
	}
	ctx := context.Background()
	otherID, err := s.store.UpsertAccount(ctx, autoTestSite, "other", "other", "", []byte("credential"), 100)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := s.store.ReconcileFolders(ctx, otherID, []store.FolderInput{{RemoteID: "foreign", Title: "Foreign"}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.setActiveAccount(ctx, accountID); err != nil {
		t.Fatal(err)
	}
	if w := request(foreign["foreign"], `{"auto_download":true}`); w.Code != http.StatusNotFound {
		t.Fatalf("foreign-account folder status=%d", w.Code)
	}
	listed := httptest.NewRecorder()
	s.handleFolders(listed, httptest.NewRequest(http.MethodGet, "/api/folders", nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"auto_download":true`) {
		t.Fatalf("folders=%d %s", listed.Code, listed.Body.String())
	}
	unauth := httptest.NewRecorder()
	s.Handler().ServeHTTP(unauth, httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/folders/%d/auto-download", folders["one"]), strings.NewReader(`{"auto_download":false}`)))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", unauth.Code)
	}
	unsupportedID, err := s.store.UpsertAccount(ctx, "synthetic-no-download", "1", "unsupported", "", []byte("credential"), 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.setActiveAccount(ctx, unsupportedID); err != nil {
		t.Fatal(err)
	}
	if w := request(folders["one"], `{"auto_download":true}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unsupported download status=%d", w.Code)
	}
}

type autoSyncSource struct{}

func (autoSyncSource) ListFolders(context.Context) ([]syncpkg.RemoteFolder, error) {
	return []syncpkg.RemoteFolder{{RemoteID: "one", Title: "One"}, {RemoteID: "two", Title: "Two"}}, nil
}
func (autoSyncSource) ListFolderPage(_ context.Context, remoteID string, _, _ int) (syncpkg.RemotePage, error) {
	ids := []string{"unavailable", "old", "new"}
	if remoteID == "two" {
		ids = []string{"other"}
	}
	var items []syncpkg.RemoteItem
	for _, id := range ids {
		items = append(items, syncpkg.RemoteItem{RemoteID: id, Title: id, URL: "https://example.test/" + id})
	}
	return syncpkg.RemotePage{Items: items}, nil
}

func TestSuccessfulSyncEventBackfillsAndUnavailableItemDoesNotBlockOthers(t *testing.T) {
	base, accountID, folders := autoTestServer(t)
	ctx := context.Background()
	if _, err := base.store.SetFolderAutoDownload(ctx, accountID, folders["one"], true, 100); err != nil {
		t.Fatal(err)
	}
	if err := base.store.WithTx(ctx, func(tx *store.Tx) error {
		id, _, _, err := tx.UpsertItem(ctx, accountID, store.ItemInput{RemoteID: "old", Title: "old", URL: "https://example.test/old"}, 100)
		if err != nil {
			return err
		}
		_, err = tx.UpsertFolderItem(ctx, folders["one"], id, 100)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	engine := syncpkg.NewEngine(base.store, func(context.Context, int64) (syncpkg.Source, error) { return autoSyncSource{}, nil })
	hub := NewHub()
	s := New(config.Config{}, base.store, base.vault, engine, hub)
	s.SetDownloadManager(base.download)
	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	runID, err := engine.Start(accountID, "scheduled", "all")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case raw := <-events:
			var event struct {
				Type    string `json:"type"`
				Created int    `json:"created"`
			}
			if err := json.Unmarshal(raw, &event); err != nil {
				t.Fatal(err)
			}
			if event.Type != "download_enqueued" || event.Created == 0 {
				continue
			}
			tasks, err := s.store.ListDownloadTasks(ctx, accountID, "", 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) != 2 {
				t.Fatalf("tasks=%+v; want historical and new videos", tasks)
			}
			for _, task := range tasks {
				if task.RemoteID != "old" && task.RemoteID != "new" {
					t.Fatalf("unexpected task=%+v", task)
				}
			}
			run, err := s.store.GetRunAll(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			if run.State != "success" {
				t.Fatalf("enqueue error changed sync state=%q", run.State)
			}
			return
		case <-deadline.C:
			t.Fatal("successful sync did not enqueue historical and new videos")
		}
	}
}

func TestAutoDownloadRunsOnlyForSuccessfulScopeAndPreservesStates(t *testing.T) {
	s, accountID, folders := autoTestServer(t)
	ctx := context.Background()
	for _, folderID := range folders {
		if _, err := s.store.SetFolderAutoDownload(ctx, accountID, folderID, true, 100); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range []struct{ remote, folder string }{{"old", "one"}, {"new", "one"}, {"multipart", "one"}, {"canceled", "one"}, {"failed", "one"}, {"other", "two"}} {
		if err := s.store.WithTx(ctx, func(tx *store.Tx) error {
			id, _, _, err := tx.UpsertItem(ctx, accountID, store.ItemInput{RemoteID: entry.remote, Title: entry.remote, URL: "https://example.test/" + entry.remote}, 100)
			if err != nil {
				return err
			}
			_, err = tx.UpsertFolderItem(ctx, folders[entry.folder], id, 100)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.store.CreateDownloadTasks(ctx, accountID, 720, []store.DownloadTaskInput{{RemoteID: "multipart", PartID: 1, PageCount: 2}, {RemoteID: "canceled", PartID: 1}, {RemoteID: "failed", PartID: 1}}, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.store.FinishDownloadTask(ctx, 1, "done", "part.mp4", "", 720, "avc", 1, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.store.CancelDownloadTask(ctx, 2, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.store.FinishDownloadTask(ctx, 3, "failed", "", "error", 0, "", 0, 100); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"partial", "failed", "suspicious", "aborted"} {
		run, err := s.store.CreateRun(ctx, accountID, "manual", "all", 100)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.store.FinishRun(ctx, run, state, 2, 2, 6, 0, 0, 0, 0, "", "", 101); err != nil {
			t.Fatal(err)
		}
		s.autoDownloadAfterSync(ctx, run)
	}
	tasks, err := s.store.ListDownloadTasks(ctx, accountID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("unsuccessful sync created tasks=%+v", tasks)
	}
	run, err := s.store.CreateRun(ctx, accountID, "scheduled", "folder:one", 102)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.FinishRun(ctx, run, "success", 1, 1, 5, 0, 0, 0, 0, "", "", 103); err != nil {
		t.Fatal(err)
	}
	s.autoDownloadAfterSync(ctx, run)
	s.autoDownloadAfterSync(ctx, run)
	tasks, err = s.store.ListDownloadTasks(ctx, accountID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 6 {
		t.Fatalf("tasks=%+v; want 6", tasks)
	}
	states := map[string]string{}
	for _, task := range tasks {
		states[fmt.Sprintf("%s:%d", task.RemoteID, task.PartID)] = task.State
		if task.RemoteID == "other" {
			t.Fatal("queued folder outside run scope")
		}
	}
	for key, want := range map[string]string{"old:1": "pending", "new:1": "pending", "multipart:1": "done", "multipart:2": "pending", "canceled:1": "canceled", "failed:1": "pending"} {
		if states[key] != want {
			t.Errorf("%s state=%q want=%q", key, states[key], want)
		}
	}
}

func TestAutoDownloadWaitsForSyncStartedAfterEnabling(t *testing.T) {
	s, accountID, folders := autoTestServer(t)
	ctx := context.Background()
	folderID := folders["one"]
	if err := s.store.WithTx(ctx, func(tx *store.Tx) error {
		id, _, _, err := tx.UpsertItem(ctx, accountID, store.ItemInput{RemoteID: "old", Title: "old", URL: "https://example.test/old"}, 100)
		if err != nil {
			return err
		}
		_, err = tx.UpsertFolderItem(ctx, folderID, id, 100)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	completedRun, err := s.store.CreateRun(ctx, accountID, "manual", "all", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.FinishRun(ctx, completedRun, "success", 2, 2, 1, 0, 0, 0, 0, "", "", 100); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.SetFolderAutoDownload(ctx, accountID, folderID, true, 100); err != nil {
		t.Fatal(err)
	}
	// A completed run may reach post-processing after waiting for another pass.
	s.autoDownloadAfterSync(ctx, completedRun)
	tasks, err := s.store.ListDownloadTasks(ctx, accountID, "", 100)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("old delayed run queued tasks=%+v, err=%v", tasks, err)
	}
	nextRun, err := s.store.CreateRun(ctx, accountID, "manual", "all", 100)
	if err != nil {
		t.Fatal(err)
	}
	// Repeating an already-enabled save must not postpone eligibility again.
	if _, err := s.store.SetFolderAutoDownload(ctx, accountID, folderID, true, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.store.FinishRun(ctx, nextRun, "success", 2, 2, 1, 0, 0, 0, 0, "", "", 100); err != nil {
		t.Fatal(err)
	}
	s.autoDownloadAfterSync(ctx, nextRun)
	tasks, err = s.store.ListDownloadTasks(ctx, accountID, "", 100)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("next run tasks=%+v, err=%v; want one", tasks, err)
	}
}

func TestAutoDownloadStopsOnAuthAndFatalErrorsWithoutChangingSyncResult(t *testing.T) {
	for _, tc := range []struct{ remoteID, accountState string }{
		{"auth-error", "needs_reauth"}, {"fatal-error", "active"},
	} {
		t.Run(tc.remoteID, func(t *testing.T) {
			s, accountID, folders := autoTestServer(t)
			ctx := context.Background()
			for _, folderID := range []int64{folders["one"], folders["two"]} {
				if _, err := s.store.SetFolderAutoDownload(ctx, accountID, folderID, true, 100); err != nil {
					t.Fatal(err)
				}
			}
			for _, entry := range []struct{ remoteID, folder string }{{tc.remoteID, "one"}, {"later", "one"}, {"other", "two"}} {
				if err := s.store.WithTx(ctx, func(tx *store.Tx) error {
					id, _, _, err := tx.UpsertItem(ctx, accountID, store.ItemInput{RemoteID: entry.remoteID, Title: entry.remoteID}, 100)
					if err != nil {
						return err
					}
					_, err = tx.UpsertFolderItem(ctx, folders[entry.folder], id, 100)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			runID, err := s.store.CreateRun(ctx, accountID, "manual", "all", 100)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.store.FinishRun(ctx, runID, "success", 2, 2, 3, 0, 0, 0, 0, "", "", 100); err != nil {
				t.Fatal(err)
			}
			s.autoDownloadAfterSync(ctx, runID)
			tasks, err := s.store.ListDownloadTasks(ctx, accountID, "", 100)
			if err != nil || len(tasks) != 0 {
				t.Fatalf("tasks=%+v err=%v; want no work after terminal error", tasks, err)
			}
			account, err := s.store.GetAccountByID(ctx, accountID)
			if err != nil || account.State != tc.accountState {
				t.Fatalf("account=%+v err=%v want state %s", account, err, tc.accountState)
			}
			run, err := s.store.GetRunAll(ctx, runID)
			if err != nil || run.State != "success" {
				t.Fatalf("run=%+v err=%v; want success", run, err)
			}
		})
	}
}
