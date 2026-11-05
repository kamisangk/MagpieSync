package download

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"magpiesync/internal/store"
)

// processDirect downloads one task from a site's official MP4 links. It does
// not merge audio/video or write sidecars; the source is already a complete
// file. All requests are GETs to the exact links exposed by the site.
func (m *Manager) processDirect(ctx context.Context, task *store.DownloadTask) {
	settings, err := m.settings(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		_ = m.store.FinishDownloadTask(ctx, task.ID, "failed", task.FilePath, err.Error(), 0, "", 0, time.Now().Unix())
		m.emit("download_done", m.taskEvent(task.ID, "failed", 0, err.Error()))
		return
	}
	tmpDir := ""
	cleanup := func() {
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	}
	fail := func(err error) {
		if ctx.Err() != nil {
			if current, getErr := m.store.GetDownloadTask(context.Background(), task.ID); getErr == nil && current.State == "canceled" {
				cleanup()
			}
			return
		}
		if m.isAuth(err) {
			// A dead/expired credential invalidates all downloads for this
			// account regardless of retry eligibility, so surface it now.
			m.markNeedsReauth(task.AccountID)
		}
		if task.Attempts < 3 && retryable(err) {
			_ = m.store.RequeueDownloadTask(ctx, task.ID, err.Error(), time.Now().Unix())
			m.emit("download_progress", map[string]any{
				"id": task.ID, "state": "pending", "progress": 0, "error": err.Error(),
			})
			return
		}
		cleanup()
		_ = m.store.FinishDownloadTask(ctx, task.ID, "failed", task.FilePath, err.Error(), task.ActualQuality, task.Codec, 0, time.Now().Unix())
		m.emit("download_done", m.taskEvent(task.ID, "failed", 0, err.Error()))
	}
	if m.direct == nil {
		fail(errors.New("该站点没有配置下载解析器"))
		return
	}
	if task.PageURL == "" {
		fail(errors.New("下载任务缺少视频页面地址"))
		return
	}
	resolver, err := m.direct(ctx, task.AccountID, task.Site)
	if err != nil {
		fail(err)
		return
	}
	defer func() { _ = resolver.Close() }()

	options, err := resolver.Options(ctx, task.PageURL)
	if err != nil {
		fail(err)
		return
	}
	opt, ok := pickDirectOption(options, task.Quality)
	if !ok {
		fail(errors.New("该视频没有可用的官方下载地址"))
		return
	}
	mediaHTTP := m.http
	if m.httpFor != nil {
		mediaHTTP, err = m.httpFor(ctx, task.AccountID)
		if err != nil {
			fail(err)
			return
		}
	}

	tmpDir = filepath.Join(settings.MediaDir, ".magpiesync-tmp", strconv.FormatInt(task.ID, 10))
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		fail(err)
		return
	}
	mediaPath := filepath.Join(tmpDir, "media.mp4")
	total, err := m.fetchStream(ctx, task, []string{opt.URL}, mediaPath, 0, settings, mediaHTTP, directHeaders(opt.Headers))
	if err != nil {
		fail(err)
		return
	}
	if total <= 0 {
		fail(errors.New("下载文件为空"))
		return
	}

	outPath := buildOutputPath(settings, task)
	if task.FilePath != outPath {
		if _, statErr := os.Stat(outPath); statErr == nil {
			outPath = trimExt(outPath) + " [" + task.RemoteID + "]" + filepath.Ext(outPath)
		}
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fail(err)
		return
	}
	_ = os.Remove(outPath)
	if err := os.Rename(mediaPath, outPath); err != nil {
		if err := copyFile(mediaPath, outPath); err != nil {
			fail(err)
			return
		}
	}
	if ctx.Err() != nil {
		if current, getErr := m.store.GetDownloadTask(context.Background(), task.ID); getErr == nil && current.State == "canceled" {
			cleanup()
		}
		return
	}
	size := fileSize(outPath)
	sidecarErr := m.writeDirectSidecars(ctx, settings, task, outPath, mediaHTTP)
	if ctx.Err() != nil {
		if current, getErr := m.store.GetDownloadTask(context.Background(), task.ID); getErr == nil && current.State == "canceled" {
			cleanup()
		}
		return
	}
	cleanup()
	_ = m.store.UpdateDownloadProgress(ctx, task.ID, 1, size, size, time.Now().Unix())
	_ = m.store.FinishDownloadTask(ctx, task.ID, "done", outPath, sidecarErr, opt.Quality, "mp4", size, time.Now().Unix())
	m.emit("download_done", m.taskEvent(task.ID, "done", opt.Quality, ""))
	m.emit("download_progress", map[string]any{
		"id":             task.ID,
		"state":          "done",
		"progress":       1,
		"file_path":      outPath,
		"actual_quality": opt.Quality,
		"codec":          "mp4",
		"total_bytes":    size,
	})
}

func directHeaders(values map[string]string) http.Header {
	if len(values) == 0 {
		return nil
	}
	h := make(http.Header, len(values))
	for key, value := range values {
		if value != "" {
			h.Set(key, value)
		}
	}
	return h
}

// pickDirectOption selects the highest offered quality not above the request.
func pickDirectOption(options []DirectOption, requested int) (DirectOption, bool) {
	if len(options) == 0 {
		return DirectOption{}, false
	}
	sorted := append([]DirectOption(nil), options...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Quality > sorted[j].Quality })
	if requested <= 0 {
		return sorted[0], true
	}
	for _, opt := range sorted {
		if opt.Quality <= requested {
			return opt, true
		}
	}
	return sorted[len(sorted)-1], true
}

func trimExt(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path
	}
	return path[:len(path)-len(ext)]
}
