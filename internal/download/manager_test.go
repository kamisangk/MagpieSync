package download

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"magpiesync/internal/sites"
	"magpiesync/internal/store"
)

func TestFetchOneResumesPartialFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=3-" {
			t.Errorf("range = %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 3-5/6")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("def"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "video.m4s")
	if err := os.WriteFile(dest, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{http: srv.Client()}
	written, total, err := m.fetchOne(context.Background(), &store.DownloadTask{ID: 1, State: "running"}, srv.URL, dest, 0, m.http, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abcdef" || written != 6 || total != 6 {
		t.Fatalf("data=%q written=%d total=%d", data, written, total)
	}
}

func TestFetchStreamRestartsWhenResumeRangeIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name, contentRange, body string
		status                   int
	}{
		{"unsatisfiable", "bytes */4", "", http.StatusRequestedRangeNotSatisfiable},
		{"even when local size matches", "bytes */6", "", http.StatusRequestedRangeNotSatisfiable},
		{"wrong start", "bytes 2-5/6", "2345", http.StatusPartialContent},
		{"ends before EOF", "bytes 3-4/6", "34", http.StatusPartialContent},
		{"range and body length disagree", "bytes 3-5/6", "34", http.StatusPartialContent},
		{"missing range", "", "345", http.StatusPartialContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const fresh = "new!"
			offset := 3
			if tc.name == "even when local size matches" {
				offset = 6
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Header.Get("Range") {
				case fmt.Sprintf("bytes=%d-", offset):
					w.Header().Set("Content-Range", tc.contentRange)
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				case "":
					_, _ = w.Write([]byte(fresh))
				default:
					t.Errorf("unexpected Range: %q", r.Header.Get("Range"))
					http.Error(w, "unexpected range", http.StatusBadRequest)
				}
			}))
			defer srv.Close()

			dest := filepath.Join(t.TempDir(), "stream")
			if err := os.WriteFile(dest, bytes.Repeat([]byte("x"), offset), 0o644); err != nil {
				t.Fatal(err)
			}
			m := &Manager{}
			total, err := m.fetchStream(context.Background(), &store.DownloadTask{ID: 1}, []string{srv.URL}, dest, 0, Settings{}, srv.Client(), nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(dest)
			if err != nil || string(data) != fresh || total != int64(len(fresh)) {
				t.Fatalf("data=%q total=%d read error=%v", data, total, err)
			}
		})
	}
}

func TestFetchStreamFallsBackOnInvalidParallelResponse(t *testing.T) {
	const payload = "abcdefgh"
	for _, tc := range []struct {
		name, contentRange, body string
	}{
		{"wrong start", "bytes 1-4/8", "bcde"},
		{"wrong total", "bytes 0-3/9", "abcd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fetchedFull atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var contentRange, body string
				switch r.Header.Get("Range") {
				case "bytes=0-0":
					contentRange, body = "bytes 0-0/8", "a"
				case "bytes=0-3":
					contentRange, body = tc.contentRange, tc.body
				case "bytes=4-7":
					contentRange, body = "bytes 4-7/8", "efgh"
				case "":
					fetchedFull.Store(true)
					_, _ = w.Write([]byte(payload))
					return
				default:
					t.Errorf("unexpected Range: %q", r.Header.Get("Range"))
					http.Error(w, "unexpected range", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Range", contentRange)
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			dir := t.TempDir()
			st, err := store.Open(filepath.Join(dir, "parallel.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			dest := filepath.Join(dir, "stream")
			m := &Manager{store: st}
			total, err := m.fetchStream(context.Background(), &store.DownloadTask{ID: 1}, []string{srv.URL}, dest, 0, Settings{ChunkConcurrency: 2, ChunkThreshold: 1}, srv.Client(), nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(dest)
			if err != nil || string(data) != payload || total != int64(len(payload)) || !fetchedFull.Load() {
				t.Fatalf("data=%q total=%d fetched full=%v read error=%v", data, total, fetchedFull.Load(), err)
			}
		})
	}
}

func TestFetchParallelUsesRanges(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789"), 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			http.Error(w, "range required", http.StatusBadRequest)
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer srv.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "parallel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accID, err := st.UpsertAccount(ctx, "synthetic-video", "1", "tester", "", []byte("cred"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateDownloadTasks(ctx, accID, 80, []store.DownloadTaskInput{{RemoteID: "item-1", PartID: 1}}, 1); err != nil {
		t.Fatal(err)
	}
	task, err := st.GetDownloadTask(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "parallel.bin")
	m := &Manager{store: st, http: srv.Client()}
	total, err := m.fetchParallel(ctx, task, srv.URL, dest, 0, 4, 100, m.http, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if total != int64(len(payload)) || !bytes.Equal(got, payload) {
		t.Fatalf("total=%d len=%d equal=%v", total, len(got), bytes.Equal(got, payload))
	}
}

func TestWritePersonNFO(t *testing.T) {
	dir := t.TempDir()
	task := &store.DownloadTask{AuthorName: "薰之君", AuthorID: "395679125"}
	if err := writePersonNFO(dir, task); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "person.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "<name>薰之君</name>") || !strings.Contains(string(body), "<uniqueid>395679125</uniqueid>") {
		t.Fatalf("person.nfo = %s", body)
	}
}

func TestBuildOutputPathUsesPageTitle(t *testing.T) {
	dir := t.TempDir()
	s := DefaultSettings()
	s.MediaDir = dir
	s.Naming = map[string]Naming{"synthetic-video": {Video: "{{author_name}}/{{title}}", Page: "{{page_title}}"}}
	task := &store.DownloadTask{
		Site:       "synthetic-video",
		RemoteID:   "item-xx",
		VideoTitle: "视频标题",
		PageTitle:  "第一集",
		PageNo:     1,
		PageCount:  2,
		AuthorName: "UP主",
	}
	got := buildOutputPath(s, task)
	want := filepath.Join(dir, "synthetic-video", "UP主", "视频标题", "第一集 - P01.mp4")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestBuildOutputPathFallsBackToVideoTitle(t *testing.T) {
	s := DefaultSettings()
	s.MediaDir = "/media"
	s.Naming = map[string]Naming{"synthetic-video": {Video: "{{author_name}}/{{title}}", Page: "{{page_title}}"}}
	task := &store.DownloadTask{Site: "synthetic-video", VideoTitle: "视频标题", PageTitle: "", PageCount: 1, AuthorName: "UP主"}
	got := buildOutputPath(s, task)
	want := filepath.Join("/media", "synthetic-video", "UP主", "视频标题", "视频标题.mp4")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestBuildOutputPathDoesNotSplitTitleSlashes(t *testing.T) {
	s := DefaultSettings()
	s.MediaDir = "/media"
	s.Naming = map[string]Naming{"synthetic-video": {Video: "{{author_name}}/{{title}}", Page: "{{page_title}}"}}
	task := &store.DownloadTask{Site: "synthetic-video", VideoTitle: "a/b", PageTitle: "", PageCount: 1, AuthorName: "UP主"}
	got := buildOutputPath(s, task)
	want := filepath.Join("/media", "synthetic-video", "UP主", "a_b", "a_b.mp4")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestBuildOutputPathIncludesSiteDir(t *testing.T) {
	s := DefaultSettings()
	s.MediaDir = "/media"
	s.Naming = map[string]Naming{"synthetic-direct": {Video: "{{title}}", Page: "{{page_title}}"}}
	task := &store.DownloadTask{
		Site:       "synthetic-direct",
		VideoTitle: "标题",
		PageTitle:  "P1",
		PageCount:  1,
		AuthorName: "UP主",
	}
	got := buildOutputPath(s, task)
	want := filepath.Join("/media", "synthetic-direct", "标题", "P1.mp4")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestSafePath(t *testing.T) {
	tests := map[string]string{
		`a/b`:         "a/b",
		`../evil`:     "evil",
		`a\b`:         "a/b",
		`bad<>:"|?*x`: "bad_______x",
		`  name.  `:   "name",
		"\x00\x01":    "untitled",
		"":            "untitled",
	}
	for in, want := range tests {
		if got := safePath(in); got != want {
			t.Errorf("safePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderTemplateEmptyFallsBack(t *testing.T) {
	if got := renderTemplate("{{page_title}}", map[string]string{"page_title": ""}); got != "" {
		t.Fatalf("empty template = %q", got)
	}
	if got := renderTemplate("{{title}}", map[string]string{"title": "x/y"}); got != "x_y" {
		t.Fatalf("template = %q", got)
	}
}

func TestSanitizeSegmentReservedAndLength(t *testing.T) {
	if got := sanitizeSegment("CON"); got != "_CON" {
		t.Fatalf("reserved name = %q", got)
	}
	long := strings.Repeat("a", 200)
	if got := sanitizeSegment(long); len([]rune(got)) != 120 {
		t.Fatalf("long name length = %d", len([]rune(got)))
	}
}

func TestSettingsRoundTripValues(t *testing.T) {
	s := DefaultSettings()
	s.SiteQualities = map[string]int{"synthetic-video": 120, "synthetic-direct": 1080}
	s.Codecs = []string{"hevc", "avc"}
	s.Naming = map[string]Naming{"synthetic-video": {Video: "{{author_name}}/{{title}}", Page: "{{page_title}}"}}
	s.Archive = map[string]ArchiveOptions{
		"synthetic-video":  {NFO: true, Cover: true, Danmaku: true, Subtitles: true},
		"synthetic-direct": {NFO: false, Cover: true},
	}
	values := s.Values()
	if values["download.quality"] != "" ||
		values["download.quality.synthetic-video"] != "120" ||
		values["download.quality.synthetic-direct"] != "1080" ||
		values["download.codecs"] != "hevc,avc" {
		t.Fatalf("values = %v", values)
	}
	if values["download.nfo.synthetic-video"] != "1" ||
		values["download.nfo.synthetic-direct"] != "0" ||
		values["download.cover.synthetic-direct"] != "1" ||
		values["download.cover.synthetic-video"] != "1" {
		t.Fatalf("archive values = %v", values)
	}
	if !strings.Contains(values["download.video_template.synthetic-video"], "{{title}}") {
		t.Fatalf("synthetic-video template = %q", values["download.video_template.synthetic-video"])
	}
}

func TestNormalizeQualityForSite(t *testing.T) {
	site := sites.Site{ID: "synthetic", DownloadQualities: []sites.DownloadQuality{
		{Value: 240}, {Value: 480}, {Value: 720}, {Value: 1080},
	}}
	cases := []struct {
		name string
		site sites.Site
		in   int
		want int
	}{
		{"exact declared value", site, 720, 720},
		{"automatic", site, 0, 0},
		{"above offers", site, 2160, 1080},
		{"below offers", site, 16, 240},
	}
	for _, tc := range cases {
		if got := NormalizeQualityForSite(tc.site, tc.in); got != tc.want {
			t.Errorf("%s: NormalizeQualityForSite = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestSettingsQualityForDefaultsToAutomatic(t *testing.T) {
	s := DefaultSettings()
	if got := s.QualityFor("synthetic"); got != 0 {
		t.Fatalf("default quality = %d, want automatic", got)
	}
	s.SiteQualities["synthetic"] = 720
	if got := s.QualityFor("synthetic"); got != 720 {
		t.Fatalf("site quality = %d", got)
	}
}
