package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/robfig/cron/v3"
	"sync"

	"magpiesync/internal/scheduler"
	"magpiesync/internal/store"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSettingsScheduleRoundTrip(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: st}
	calls := 0
	var active cron.Schedule
	s.SetScheduleUpdater(func(plan cron.Schedule) {
		calls++
		active = plan
		value, _, err := st.GetSetting(context.Background(), scheduler.Key)
		if err != nil || (value == "") != (plan == nil) {
			t.Errorf("update before persistence: %q %v", value, err)
		}
	})
	request := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		s.handleSettings(rec, httptest.NewRequest(method, "/api/settings", strings.NewReader(body)))
		return rec
	}
	read := func() string {
		t.Helper()
		rec := request("GET", "")
		if rec.Code != 200 {
			t.Fatal(rec.Body.String())
		}
		var body struct {
			Settings struct {
				Schedule string `json:"sync_schedule"`
			} `json:"settings"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Settings.Schedule
	}
	if read() != "" {
		t.Fatal("default enabled")
	}
	if rec := request("PUT", `{"sync_schedule":" 0 4 * * * "}`); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if read() != "0 4 * * *" || calls != 1 || active == nil {
		t.Fatal("not enabled")
	}
	next := active.Next(time.Now())
	if rec := request("PUT", `{"sync_schedule":"99 4 * * *","concurrency":4}`); rec.Code != 400 {
		t.Fatal(rec.Body.String())
	}
	if read() != "0 4 * * *" || calls != 1 || !active.Next(time.Now()).Equal(next) {
		t.Fatal("invalid changed schedule")
	}
	if rec := request("PUT", `{"concurrency":3}`); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if calls != 1 || read() != "0 4 * * *" {
		t.Fatal("partial update changed schedule")
	}
	if rec := request("PUT", `{"sync_schedule":""}`); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if read() != "" || calls != 2 || active != nil {
		t.Fatal("not disabled")
	}
	st.Close()
	if rec := request("PUT", `{"sync_schedule":"0 5 * * *"}`); rec.Code != 500 {
		t.Fatal("expected storage failure")
	}
	if calls != 2 {
		t.Fatal("applied failed write")
	}
}

func TestSchedulePreview(t *testing.T) {
	s := &Server{}
	for _, tc := range []struct {
		query string
		code  int
	}{
		{"0+4+*+*+*", 200}, {"", 200}, {"99+4+*+*+*", 400},
	} {
		rec := httptest.NewRecorder()
		s.handleSyncPreview(rec, httptest.NewRequest("GET", "/api/settings/sync-preview?expression="+tc.query, nil))
		if rec.Code != tc.code {
			t.Fatalf("%q: %d %s", tc.query, rec.Code, rec.Body.String())
		}
		if tc.code == 200 {
			var body struct {
				Next *time.Time `json:"next_run"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if (body.Next == nil) != (tc.query == "") {
				t.Fatal("unexpected next run")
			}
		}
	}
}

func TestScheduleWriteFailurePreservesRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetSettings(context.Background(), map[string]string{scheduler.Key: "0 4 * * *"}, 100); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER reject_schedule BEFORE UPDATE ON settings WHEN NEW.key = 'sync.schedule' BEGIN SELECT RAISE(ABORT, 'test write failure'); END`); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}
	s.SetScheduleUpdater(func(cron.Schedule) { t.Error("applied failed write") })
	rec := httptest.NewRecorder()
	s.handleSettings(rec, httptest.NewRequest("PUT", "/api/settings", strings.NewReader(`{"sync_schedule":"0 6 * * *"}`)))
	if rec.Code != 500 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	value, _, err := st.GetSetting(context.Background(), scheduler.Key)
	if err != nil || value != "0 4 * * *" {
		t.Fatalf("changed stored plan %q: %v", value, err)
	}
}

func TestConcurrentScheduleSavesKeepRuntimeAndStorageAligned(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: st}
	var active cron.Schedule
	s.SetScheduleUpdater(func(plan cron.Schedule) { active = plan })
	var wg sync.WaitGroup
	for hour := 0; hour < 12; hour++ {
		wg.Add(1)
		go func(hour int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			s.handleSettings(rec, httptest.NewRequest("PUT", "/api/settings", strings.NewReader(fmt.Sprintf(`{"sync_schedule":"0 %d * * *"}`, hour))))
			if rec.Code != 200 {
				t.Errorf("save failed: %s", rec.Body.String())
			}
		}(hour)
	}
	wg.Wait()
	value, _, err := st.GetSetting(context.Background(), scheduler.Key)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := scheduler.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if active == nil || !active.Next(now).Equal(persisted.Next(now)) {
		t.Fatal("runtime diverged from storage")
	}
}
