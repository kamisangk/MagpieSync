package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"magpiesync/internal/store"
)

func TestDownloadsQueryPaginationAndValidation(t *testing.T) {
	s, accountID, _ := autoTestServer(t)
	ctx := context.Background()
	if err := s.setActiveAccount(ctx, accountID); err != nil {
		t.Fatal(err)
	}
	token, err := s.auth.create()
	if err != nil {
		t.Fatal(err)
	}
	inputs := []store.DownloadTaskInput{{RemoteID: "one", PartID: 1}, {RemoteID: "two", PartID: 1}, {RemoteID: "three", PartID: 1}}
	if _, _, err := s.store.CreateDownloadTasks(ctx, accountID, 720, inputs, 100); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.store.ListDownloadTasks(ctx, accountID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.PauseDownloadTask(ctx, tasks[0].ID, 101); err != nil {
		t.Fatal(err)
	}
	if err := s.store.CancelDownloadTask(ctx, tasks[1].ID, 102); err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	t.Run("legacy limit remains compatible", func(t *testing.T) {
		w := request("/api/downloads?limit=1")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body["tasks"].([]any)) != 1 {
			t.Fatalf("tasks=%v", body["tasks"])
		}
		if _, ok := body["total"]; ok {
			t.Fatal("legacy response unexpectedly has pagination fields")
		}
	})
	t.Run("active filter counts and pages", func(t *testing.T) {
		w := request("/api/downloads?filter=active&page=1&page_size=1")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var body struct {
			Tasks    []map[string]any `json:"tasks"`
			Total    int              `json:"total"`
			Page     int              `json:"page"`
			PageSize int              `json:"page_size"`
			Counts   map[string]int   `json:"counts"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Total != 1 || len(body.Tasks) != 1 || body.Page != 1 || body.PageSize != 1 {
			t.Fatalf("response=%+v", body)
		}
		if body.Counts["pending"] != 1 || body.Counts["paused"] != 1 || body.Counts["canceled"] != 1 {
			t.Fatalf("counts=%v", body.Counts)
		}
	})
	for _, path := range []string{
		"/api/downloads?filter=unknown",
		"/api/downloads?filter=all&page=0",
		"/api/downloads?filter=all&page_size=101",
		"/api/downloads?filter=all&page=",
		"/api/downloads?filter=all&page_size=",
		"/api/downloads?filter=all&state=paused",
		"/api/downloads?page=1&limit=5",
	} {
		t.Run(path, func(t *testing.T) {
			w := request(path)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
			}
		})
	}
	t.Run("page beyond end clamps", func(t *testing.T) {
		w := request("/api/downloads?filter=all&page=99&page_size=2")
		var body struct {
			Page  int `json:"page"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || body.Page != 2 || body.Total != 3 {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}
