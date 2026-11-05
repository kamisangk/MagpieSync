package download

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "magpiesync/internal/sites/all"
	"magpiesync/internal/store"
)

type fakeDirectResolver struct {
	options []DirectOption
	err     error
	closed  bool
}

func (f *fakeDirectResolver) Options(context.Context, string) ([]DirectOption, error) {
	return f.options, f.err
}

func (f *fakeDirectResolver) Close() error {
	f.closed = true
	return nil
}

func TestProcessDirectDownloadsOfficialLink(t *testing.T) {
	payload := []byte("direct-video-bytes")
	coverPayload := []byte("direct-cover-bytes")
	var gotReferer, gotCookie, gotCoverReferer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/480.mp4", "/720.mp4":
			gotReferer = r.Header.Get("Referer")
			gotCookie = r.Header.Get("Cookie")
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(payload)
		case "/cover.jpg":
			gotCoverReferer = r.Header.Get("Referer")
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(coverPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "direct.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accID, err := st.UpsertAccount(ctx, "synthetic-direct", "1", "tester", "", []byte("cred"), 1)
	if err != nil {
		t.Fatal(err)
	}
	mediaDir := t.TempDir()
	if _, _, err := st.CreateDownloadTasks(ctx, accID, 0, []store.DownloadTaskInput{{
		Site:       "synthetic-direct",
		PageURL:    "https://media.example.test/video/1/example/",
		RemoteID:   "1",
		VideoTitle: "Example",
		CoverURL:   srv.URL + "/cover.jpg",
		PageNo:     1,
		PageCount:  1,
		AuthorName: "Uploader",
		Summary:    "Example plot",
		Tags:       []string{"tag-a"},
	}}, 1); err != nil {
		t.Fatal(err)
	}
	task, err := st.ClaimDownloadTask(ctx, accID, 2)
	if err != nil || task == nil {
		t.Fatalf("claim = %v, %v", task, err)
	}
	resolver := &fakeDirectResolver{options: []DirectOption{
		{Quality: 480, Label: "480P", URL: srv.URL + "/480.mp4"},
		{Quality: 720, Label: "720P", URL: srv.URL + "/720.mp4", Headers: map[string]string{
			"Referer": "https://media.example.test/video/1/example/",
			"Cookie":  "X-Session=test",
		}},
	}}
	settings := DefaultSettings()
	settings.MediaDir = mediaDir
	settings.ChunkConcurrency = 1
	settings.Archive = map[string]ArchiveOptions{"synthetic-direct": {NFO: true, Cover: true}}
	m := &Manager{
		store:    st,
		http:     srv.Client(),
		settings: func(context.Context) (Settings, error) { return settings, nil },
		direct: func(context.Context, int64, string) (DirectResolver, error) {
			return resolver, nil
		},
	}
	m.processDirect(ctx, task)

	got, err := st.GetDownloadTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "done" {
		t.Fatalf("state = %s, error = %s", got.State, got.Error)
	}
	if got.ActualQuality != 720 || got.Codec != "mp4" {
		t.Fatalf("quality/codec = %d/%s", got.ActualQuality, got.Codec)
	}
	data, err := os.ReadFile(got.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) {
		t.Fatalf("file = %q", data)
	}
	if gotReferer != "https://media.example.test/video/1/example/" || gotCookie != "X-Session=test" {
		t.Fatalf("headers referer=%q cookie=%q", gotReferer, gotCookie)
	}
	if gotCoverReferer != "https://media.example.test/video/1/example/" {
		t.Fatalf("cover referer = %q", gotCoverReferer)
	}
	dir := filepath.Dir(got.FilePath)
	for _, name := range []string{"poster.jpg", "fanart.jpg"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(data) != string(coverPayload) {
			t.Fatalf("%s = %q", name, data)
		}
	}
	nfo, err := os.ReadFile(strings.TrimSuffix(got.FilePath, filepath.Ext(got.FilePath)) + ".nfo")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<title>Example</title>",
		"<plot>Example plot</plot>",
		"<tag>tag-a</tag>",
		"<role>作者</role>",
		"<thumb>poster.jpg</thumb>",
	} {
		if !strings.Contains(string(nfo), want) {
			t.Fatalf("NFO missing %q: %s", want, nfo)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "person.nfo")); err != nil {
		t.Fatalf("person.nfo: %v", err)
	}
	if !resolver.closed {
		t.Fatal("resolver was not closed")
	}
}

func TestProcessDirectAuthErrorMarksNeedsReauth(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "direct.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	accID, err := st.UpsertAccount(ctx, "synthetic-direct", "1", "tester", "", []byte("cred"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateDownloadTasks(ctx, accID, 0, []store.DownloadTaskInput{{
		Site:       "synthetic-direct",
		PageURL:    "https://media.example.test/video/v1",
		RemoteID:   "1",
		VideoTitle: "Example",
		PageNo:     1,
		PageCount:  1,
		AuthorName: "Uploader",
	}}, 1); err != nil {
		t.Fatal(err)
	}
	task, err := st.ClaimDownloadTask(ctx, accID, 2)
	if err != nil || task == nil {
		t.Fatalf("claim = %v, %v", task, err)
	}
	authErr := errors.New("synthetic-direct: login required")
	resolver := &fakeDirectResolver{err: authErr}
	settings := DefaultSettings()
	settings.MediaDir = t.TempDir()
	settings.ChunkConcurrency = 1
	m := &Manager{
		store:    st,
		http:     &http.Client{},
		settings: func(context.Context) (Settings, error) { return settings, nil },
		direct: func(context.Context, int64, string) (DirectResolver, error) {
			return resolver, nil
		},
	}
	m.SetAuthClassifier(func(err error) bool {
		return err == authErr
	})
	m.processDirect(ctx, task)

	acct, err := st.GetAccountByID(ctx, accID)
	if err != nil {
		t.Fatal(err)
	}
	if acct.State != "needs_reauth" {
		t.Fatalf("state = %q, want needs_reauth", acct.State)
	}
}

func TestPickDirectOptionChoosesHighestQualityAtOrBelowRequest(t *testing.T) {
	opts := []DirectOption{{Quality: 720}, {Quality: 480}, {Quality: 360}}
	tests := map[int]int{0: 720, 720: 720, 640: 480, 480: 480, 240: 360}
	for requested, want := range tests {
		got, ok := pickDirectOption(opts, requested)
		if !ok || got.Quality != want {
			t.Errorf("pick(%d) = %+v, %v; want %d", requested, got, ok, want)
		}
	}
}
