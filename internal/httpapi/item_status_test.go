package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"magpiesync/internal/store"
)

func TestItemStatusAPIContextFiltersAndCompatibility(t *testing.T) {
	s, account, folders := autoTestServer(t)
	ctx := context.Background()
	var itemID int64
	err := s.store.WithTx(ctx, func(tx *store.Tx) error {
		var err error
		itemID, _, _, err = tx.UpsertItem(ctx, account, store.ItemInput{RemoteID: "shared", Title: "Shared"}, 1)
		if err != nil {
			return err
		}
		for _, folder := range []int64{folders["one"], folders["two"]} {
			if _, err = tx.UpsertFolderItem(ctx, folder, itemID, 1); err != nil {
				return err
			}
		}
		for i := 0; i < 2; i++ {
			if _, err = tx.MarkLinkMissing(ctx, folders["one"], itemID, 2, 2); err != nil {
				return err
			}
		}
		return tx.RecomputeItemStates(ctx, account)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.store.CreateDownloadTasks(ctx, account, 0, []store.DownloadTaskInput{{RemoteID: "shared", PartID: 1}}, 1); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.store.ListDownloadTasks(ctx, account, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.store.FinishDownloadTask(ctx, tasks[0].ID, "missing", "/lost.mp4", "", 0, "", 0, 2); err != nil {
		t.Fatal(err)
	}
	foreign, err := s.store.UpsertAccount(ctx, autoTestSite, "foreign", "Foreign", "", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	foreignFolders, err := s.store.ReconcileFolders(ctx, foreign, []store.FolderInput{{RemoteID: "foreign", Title: "Foreign"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	request := func(query string, detail bool) *httptest.ResponseRecorder {
		path := "/api/items"
		if detail {
			path += fmt.Sprintf("/%d", itemID)
		}
		r := httptest.NewRequest(http.MethodGet, path+query, nil)
		r.SetPathValue("id", fmt.Sprint(itemID))
		w := httptest.NewRecorder()
		if detail {
			s.handleItem(w, r)
		} else {
			s.handleItems(w, r)
		}
		return w
	}
	for _, detail := range []bool{false, true} {
		for _, tc := range []struct {
			query  string
			status int
		}{
			{"?folder_id=bad", 400}, {"?folder_id=-1", 400}, {"?folder_id=999999", 404}, {fmt.Sprintf("?folder_id=%d", foreignFolders["foreign"]), 404},
		} {
			if w := request(tc.query, detail); w.Code != tc.status {
				t.Fatalf("detail=%v %s: %d %s", detail, tc.query, w.Code, w.Body.String())
			}
		}
	}
	for _, query := range []string{"?remote_state=bad", "?local_state=bad", "?state=active&remote_state=removed"} {
		if w := request(query, false); w.Code != 400 {
			t.Fatal(query, w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct{ query, state string }{
		{"", "active"}, {fmt.Sprintf("?folder_id=%d&remote_state=removed&local_state=missing&page_size=1", folders["one"]), "removed"}, {fmt.Sprintf("?folder_id=%d&state=active", folders["one"]), "removed"},
	} {
		w := request(tc.query, false)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var result struct {
			Items []store.Item `json:"items"`
			Total int          `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Total != 1 || len(result.Items) != 1 || result.Items[0].RemoteState != tc.state || result.Items[0].State != "active" || result.Items[0].Local.State != "missing" || result.Items[0].Local.Missing != 1 {
			t.Fatalf("%s: %+v", tc.query, result)
		}
	}
	w := request(fmt.Sprintf("?folder_id=%d", folders["one"]), true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var detail struct {
		Item store.Item `json:"item"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Item.RemoteState != "removed" || detail.Item.State != "active" || len(detail.Item.Folders) != 2 || detail.Item.Local.Missing != 1 {
		t.Fatal(detail)
	}
	emptyFolder, err := s.store.AddSource(ctx, account, "favorite", "", "empty", "Empty", 1)
	if err != nil {
		t.Fatal(err)
	}
	if w := request(fmt.Sprintf("?folder_id=%d", emptyFolder), true); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
}
