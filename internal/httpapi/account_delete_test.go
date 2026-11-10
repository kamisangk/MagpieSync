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
	"magpiesync/internal/vault"
)

func TestDeleteAccountHandlerRemovesAccountAndLocalData(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	v, err := vault.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st, vault: v}
	ctx := context.Background()
	id, err := st.UpsertAccount(ctx, "synthetic-video", "1001", "alice", "", []byte("cred"), 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReconcileFolders(ctx, id, []store.FolderInput{{RemoteID: "fav", Title: "收藏", Kind: "favorite"}}, 101); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/accounts/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	s.handleAccountDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetAccountByID(ctx, id); err == nil {
		t.Fatal("account still exists")
	}
	if folders, err := st.CountFolders(ctx, id); err != nil || folders != 0 {
		t.Fatalf("folders = %d, err = %v", folders, err)
	}
}

func TestDeleteAccountOptionValidation(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"delete_local_files":null}`, `{"delete_local_files":"yes"}`, `{"delete_local_files":false} {}`, `{`} {
		t.Run(body, func(t *testing.T) {
			s, a, _ := autoTestServer(t)
			req := httptest.NewRequest(http.MethodDelete, "/api/accounts/"+fmt.Sprint(a), strings.NewReader(body))
			req.SetPathValue("id", fmt.Sprint(a))
			w := httptest.NewRecorder()
			s.handleAccountDelete(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if _, err := s.store.GetAccountByID(context.Background(), a); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeleteAccountOptionalFilesSharedProtectionAndFallback(t *testing.T) {
	for _, mode := range []string{"legacy", "retain", "cleanup"} {
		t.Run(mode, func(t *testing.T) {
			s, a, _ := autoTestServer(t)
			ctx := context.Background()
			root := t.TempDir()
			b, err := s.store.UpsertAccount(ctx, autoTestSite, "other", "Other", "", []byte("credential"), 1)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.setActiveAccount(ctx, a); err != nil {
				t.Fatal(err)
			}
			if err = s.store.SetSettings(ctx, map[string]string{"download.media_dir": root}, 1); err != nil {
				t.Fatal(err)
			}
			for _, remote := range []string{"shared", "unique"} {
				path := filepath.Join(root, remote+".mp4")
				if err = os.WriteFile(path, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, _, err = s.store.CreateDownloadTasks(ctx, a, 0, []store.DownloadTaskInput{{Site: autoTestSite, RemoteID: remote}}, 1); err != nil {
					t.Fatal(err)
				}
				task, err := s.store.ClaimAnyDownloadTask(ctx, 1)
				if err != nil || task == nil {
					t.Fatal(err)
				}
				if err = s.store.FinishDownloadTask(ctx, task.ID, "done", path, "", 0, "", 7, 1); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.store.WithTx(ctx, func(tx *store.Tx) error {
				_, _, _, err := tx.UpsertItem(ctx, b, store.ItemInput{RemoteID: "shared", Title: "Shared"}, 1)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			body := ""
			if mode != "legacy" {
				body = fmt.Sprintf(`{"delete_local_files":%t}`, mode == "cleanup")
			}
			req := httptest.NewRequest(http.MethodDelete, "/api/accounts/"+fmt.Sprint(a), strings.NewReader(body))
			req.SetPathValue("id", fmt.Sprint(a))
			w := httptest.NewRecorder()
			s.handleAccountDelete(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var result struct {
				OK      bool  `json:"ok"`
				Deleted int64 `json:"deleted"`
				store.AccountDeletionResult
			}
			if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !result.OK || result.Deleted != a {
				t.Fatalf("%+v", result)
			}
			if _, err = os.Stat(filepath.Join(root, "shared.mp4")); err != nil {
				t.Fatal(err)
			}
			_, err = os.Stat(filepath.Join(root, "unique.mp4"))
			if mode == "cleanup" {
				if !os.IsNotExist(err) || result.DeletedFiles != 1 || result.ProtectedVideos != 1 {
					t.Fatalf("result=%+v stat=%v", result, err)
				}
			} else if err != nil || result.DeletedFiles != 0 {
				t.Fatalf("result=%+v stat=%v", result, err)
			}
			active, err := s.currentAccount(ctx)
			if err != nil || active.ID != b {
				t.Fatalf("active=%+v err=%v", active, err)
			}
		})
	}
}

func TestDeleteAccountBusyDownloadRejected(t *testing.T) {
	s, a, _ := autoTestServer(t)
	ctx := context.Background()
	if _, _, err := s.store.CreateDownloadTasks(ctx, a, 0, []store.DownloadTaskInput{{RemoteID: "busy"}}, 1); err != nil {
		t.Fatal(err)
	}
	if task, err := s.store.ClaimAnyDownloadTask(ctx, 1); err != nil || task == nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/accounts/"+fmt.Sprint(a), nil)
	req.SetPathValue("id", fmt.Sprint(a))
	w := httptest.NewRecorder()
	s.handleAccountDelete(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := s.store.GetAccountByID(ctx, a); err != nil {
		t.Fatal(err)
	}
}
