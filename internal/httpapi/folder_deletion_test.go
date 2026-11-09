package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"magpiesync/internal/store"
)

func TestFolderDeleteAPIValidationAndRetention(t *testing.T) {
	s, a, folders := autoTestServer(t)
	ctx := context.Background()
	id := folders["one"]
	request := func(id string, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodDelete, "/api/folders/"+id, strings.NewReader(body))
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		s.handleFolderDelete(w, r)
		return w
	}
	for _, tc := range []struct {
		id, body string
		status   int
	}{
		{"invalid", `{"delete_local_files":false}`, 400},
		{fmt.Sprint(id), `{}`, 400},
		{fmt.Sprint(id), `{"delete_local_files":null}`, 400},
		{fmt.Sprint(id), `{"delete_local_files":"yes"}`, 400},
		{"9999", `{"delete_local_files":false}`, 404},
		{fmt.Sprint(id), `{"delete_local_files":false}`, 409},
	} {
		if w := request(tc.id, tc.body); w.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
	}
	if _, err := s.store.ReconcileFolders(ctx, a, []store.FolderInput{{RemoteID: "two", Title: "Two"}}, 101); err != nil {
		t.Fatal(err)
	}
	if w := request(fmt.Sprint(id), `{"delete_local_files":false}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w := request(fmt.Sprint(id), `{"delete_local_files":false}`); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestFolderDeleteAPICleanup(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprint(remove), func(t *testing.T) {
			s, a, folders := autoTestServer(t)
			ctx := context.Background()
			id := folders["one"]
			root := t.TempDir()
			path := filepath.Join(root, "video.mp4")
			os.WriteFile(path, []byte("fixture"), 0600)
			if err := s.store.SetSettings(ctx, map[string]string{"download.media_dir": root}, 100); err != nil {
				t.Fatal(err)
			}
			if err := s.store.WithTx(ctx, func(tx *store.Tx) error {
				item, _, _, err := tx.UpsertItem(ctx, a, store.ItemInput{RemoteID: "video", Title: "Video"}, 100)
				if err != nil {
					return err
				}
				_, err = tx.UpsertFolderItem(ctx, id, item, 100)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.store.CreateDownloadTasks(ctx, a, 0, []store.DownloadTaskInput{{Site: autoTestSite, RemoteID: "video"}}, 100); err != nil {
				t.Fatal(err)
			}
			task, err := s.store.ClaimAnyDownloadTask(ctx, 100)
			if err != nil || task == nil {
				t.Fatal(err)
			}
			if err := s.store.FinishDownloadTask(ctx, task.ID, "done", path, "", 0, "", 7, 100); err != nil {
				t.Fatal(err)
			}
			if _, err := s.store.ReconcileFolders(ctx, a, nil, 101); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodDelete, "/api/folders/"+fmt.Sprint(id), strings.NewReader(fmt.Sprintf(`{"delete_local_files":%t}`, remove)))
			r.SetPathValue("id", fmt.Sprint(id))
			w := httptest.NewRecorder()
			s.handleFolderDelete(w, r)
			if w.Code != 200 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			var result store.FolderDeletionResult
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			task, err = s.store.GetDownloadTask(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(path)
			if remove {
				if result.DeletedFiles != 1 || task.State != "missing" || !os.IsNotExist(statErr) {
					t.Fatalf("result=%+v state=%s stat=%v", result, task.State, statErr)
				}
			} else if statErr != nil || task.State != "done" {
				t.Fatalf("state=%s stat=%v", task.State, statErr)
			}
			items, _, err := s.store.QueryItems(ctx, a, store.ItemFilter{})
			if err != nil || len(items) != 1 {
				t.Fatalf("items=%d err=%v", len(items), err)
			}
		})
	}
}

func TestFolderDeleteRouteRequiresAuthenticationAndOwnership(t *testing.T) {
	s, account, folders := autoTestServer(t)
	ctx := context.Background()
	foreign, err := s.store.UpsertAccount(ctx, autoTestSite, "foreign", "Foreign", "", []byte("fixture credential"), 100)
	if err != nil {
		t.Fatal(err)
	}
	foreignFolders, err := s.store.ReconcileFolders(ctx, foreign, []store.FolderInput{{RemoteID: "foreign-source", Title: "Foreign"}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.ReconcileFolders(ctx, foreign, nil, 101); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.ReconcileFolders(ctx, account, nil, 101); err != nil {
		t.Fatal(err)
	}
	if err = s.setActiveAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	token, err := s.auth.create()
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	for _, tc := range []struct {
		name          string
		folder        int64
		authenticated bool
		want          int
	}{
		{"missing session", folders["one"], false, http.StatusUnauthorized},
		{"foreign folder", foreignFolders["foreign-source"], true, http.StatusNotFound},
		{"owned folder", folders["one"], true, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/folders/%d", tc.folder), strings.NewReader(`{"delete_local_files":false}`))
			if tc.authenticated {
				request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
			}
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, request)
			if result.Code != tc.want {
				t.Fatalf("status=%d body=%s", result.Code, result.Body.String())
			}
		})
	}
	retained, err := s.store.ListFolders(ctx, foreign)
	if err != nil || len(retained) != 1 {
		t.Fatalf("foreign folders=%+v err=%v", retained, err)
	}
}

func TestFolderDeleteAPIProtectsSharedMediaAndUpdatesBackupCounts(t *testing.T) {
	for _, tc := range []struct {
		name          string
		sameSite      bool
		otherFolder   bool
		wantProtected bool
	}{
		{"other account same site", true, false, true},
		{"other account different site", false, false, false},
		{"other archived folder", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, account, folders := autoTestServer(t)
			ctx := context.Background()
			root := t.TempDir()
			path := filepath.Join(root, "video.mp4")
			if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := s.store.SetSettings(ctx, map[string]string{"download.media_dir": root}, 100); err != nil {
				t.Fatal(err)
			}
			if err := s.setActiveAccount(ctx, account); err != nil {
				t.Fatal(err)
			}
			otherAccount := account
			if !tc.otherFolder {
				site := autoTestSite
				if !tc.sameSite {
					site = "synthetic-other-site"
				}
				var err error
				otherAccount, err = s.store.UpsertAccount(ctx, site, "other", "Other", "", []byte("fixture credential"), 100)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := s.store.WithTx(ctx, func(tx *store.Tx) error {
				item, _, _, err := tx.UpsertItem(ctx, account, store.ItemInput{RemoteID: "video", Title: "Video"}, 100)
				if err != nil {
					return err
				}
				if _, err = tx.UpsertFolderItem(ctx, folders["one"], item, 100); err != nil {
					return err
				}
				if tc.otherFolder {
					_, err = tx.UpsertFolderItem(ctx, folders["two"], item, 100)
					return err
				}
				_, _, _, err = tx.UpsertItem(ctx, otherAccount, store.ItemInput{RemoteID: "video", Title: "Other video"}, 100)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.store.CreateDownloadTasks(ctx, account, 0, []store.DownloadTaskInput{{Site: autoTestSite, RemoteID: "video"}}, 100); err != nil {
				t.Fatal(err)
			}
			task, err := s.store.ClaimAnyDownloadTask(ctx, 100)
			if err != nil || task == nil {
				t.Fatalf("task=%+v err=%v", task, err)
			}
			if err = s.store.FinishDownloadTask(ctx, task.ID, "done", path, "", 0, "", 7, 100); err != nil {
				t.Fatal(err)
			}
			if _, err = s.store.ReconcileFolders(ctx, account, nil, 101); err != nil {
				t.Fatal(err)
			}
			token, err := s.auth.create()
			if err != nil {
				t.Fatal(err)
			}
			handler := s.Handler()
			request := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/folders/%d", folders["one"]), strings.NewReader(`{"delete_local_files":true}`))
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var result store.FolderDeletionResult
			if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(path)
			count, err := s.store.CountDownloadedVideos(ctx, account)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantProtected {
				if result.ProtectedVideos != 1 || result.DeletedFiles != 0 || statErr != nil || count != 1 {
					t.Fatalf("result=%+v stat=%v count=%d", result, statErr, count)
				}
			} else if result.ProtectedVideos != 0 || result.DeletedFiles != 1 || !os.IsNotExist(statErr) || count != 0 {
				t.Fatalf("result=%+v stat=%v count=%d", result, statErr, count)
			}
		})
	}
}

func TestFolderDeleteAPIAccountIsolation(t *testing.T) {
	s, a, folders := autoTestServer(t)
	ctx := context.Background()
	id := folders["one"]
	if _, err := s.store.UpsertAccount(ctx, autoTestSite, "second", "Second", "", []byte("credential"), 101); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodDelete, "/api/folders/"+fmt.Sprint(id), strings.NewReader(`{"delete_local_files":true}`))
	r.SetPathValue("id", fmt.Sprint(id))
	w := httptest.NewRecorder()
	s.handleFolderDelete(w, r)
	if w.Code != 404 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range accounts {
		if err := s.store.DeleteAccount(ctx, account.ID); err != nil {
			t.Fatal(err)
		}
	}
	r = httptest.NewRequest(http.MethodDelete, "/api/folders/1", strings.NewReader(`{"delete_local_files":false}`))
	r.SetPathValue("id", "1")
	w = httptest.NewRecorder()
	s.handleFolderDelete(w, r)
	if w.Code != 401 {
		t.Fatalf("%d %s (original account %d)", w.Code, w.Body.String(), a)
	}
}
