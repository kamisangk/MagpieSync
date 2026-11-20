package download

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishDownloadPreservesConcurrentOutputs(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir("videos", 0o755); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"Title.mp4", "Title [same_id].mp4"} {
		if err := root.WriteFile(filepath.Join("videos", filename), []byte(filename), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	type result struct {
		published publishedDownload
		want      string
		err       error
	}
	const workers = 4
	results := make(chan result, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		source := fmt.Sprintf("source-%d.mp4", i)
		if err := root.WriteFile(source, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		go func() {
			<-start
			published, err := publishDownload(t.Context(), root, filepath.Join(dir, source), filepath.Join(dir, "videos", "Title.mp4"), "same/id")
			results <- result{published, source, err}
		}()
	}
	close(start)
	paths := make(map[string]bool)
	for i := 0; i < workers; i++ {
		res := <-results
		if res.err != nil {
			t.Error(res.err)
			continue
		}
		path := res.published.path
		if paths[path] || filepath.Dir(path) != filepath.Join(dir, "videos") {
			t.Errorf("unexpected output path %q", path)
		}
		paths[path] = true
		data, err := os.ReadFile(path)
		if err != nil || string(data) != res.want || res.published.size != int64(len(res.want)) {
			t.Errorf("output %q: got %q, size %d, error %v", path, data, res.published.size, err)
		}
		if data, err := root.ReadFile(res.want); err != nil || string(data) != res.want {
			t.Errorf("preserved source %q: got %q, error %v", res.want, data, err)
		}
	}
	for _, filename := range []string{"Title.mp4", "Title [same_id].mp4"} {
		if data, err := root.ReadFile(filepath.Join("videos", filename)); err != nil || string(data) != filename {
			t.Errorf("existing output %q: got %q, error %v", filename, data, err)
		}
	}
}
