package download

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"magpiesync/internal/media"
	"magpiesync/internal/sites"
	"magpiesync/internal/store"
)

type ClientProvider func(ctx context.Context, accountID int64) (VideoClient, error)
type HTTPClientProvider func(ctx context.Context, accountID int64) (*http.Client, error)

// DirectOption is one media link exposed by a site's page.
type DirectOption struct {
	Quality int
	Label   string
	URL     string
	Headers map[string]string
}

// DirectResolver resolves a video page to its official download links. It only
// performs GET requests and is closed after each task so browser fallbacks do
// not leak.
type DirectResolver interface {
	Options(ctx context.Context, pageURL string) ([]DirectOption, error)
	Close() error
}

type DirectProvider func(ctx context.Context, accountID int64, site string) (DirectResolver, error)

type VideoClient interface {
	VideoDetail(ctx context.Context, remoteID string) (*media.VideoDetail, error)
	VideoTags(ctx context.Context, remoteID string) ([]string, error)
	PlayURL(ctx context.Context, remoteID string, partID int64, quality int) (*media.PlayInfo, error)
	Danmaku(ctx context.Context, partID int64, duration int) ([]media.Danmaku, error)
	Subtitles(ctx context.Context, remoteID string, partID int64) ([]media.Subtitle, error)
	SubtitleBody(ctx context.Context, rawURL string) ([]media.SubtitleItem, error)
}

type EventFunc func(kind string, data any)

type Manager struct {
	store    *store.Store
	provider ClientProvider
	direct   DirectProvider
	httpFor  HTTPClientProvider
	settings func(context.Context) (Settings, error)
	onEvent  EventFunc
	authFn   func(error) bool

	http *http.Client

	mu      sync.Mutex
	cancel  context.CancelFunc
	cancels map[int64]context.CancelFunc
	running bool
	wg      sync.WaitGroup
}

func NewManager(s *store.Store, provider ClientProvider, httpFor HTTPClientProvider, settings func(context.Context) (Settings, error), onEvent EventFunc) *Manager {
	return &Manager{
		store:    s,
		provider: provider,
		httpFor:  httpFor,
		settings: settings,
		onEvent:  onEvent,
		cancels:  make(map[int64]context.CancelFunc),
		http: &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          32,
				MaxIdleConnsPerHost:   16,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
			},
		},
	}
}

// SetDirectProvider wires the generic direct-download capability.
func (m *Manager) SetDirectProvider(p DirectProvider) { m.direct = p }

// SetAuthClassifier installs the predicate used to detect authentication
// failures on the download path. When such a failure is observed the account
// is marked needs_reauth so the UI can prompt the user to log in again.
func (m *Manager) SetAuthClassifier(fn func(error) bool) { m.authFn = fn }

func (m *Manager) isAuth(err error) bool {
	return m.authFn != nil && m.authFn(err)
}

func (m *Manager) markNeedsReauth(accountID int64) {
	if err := m.store.SetAccountState(context.Background(), accountID, "needs_reauth", time.Now().Unix()); err != nil {
		slog.Error("mark account needs_reauth from download", "account_id", accountID, "error", err)
	}
}

func (m *Manager) Start(parent context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	m.cancel = cancel
	m.running = true
	m.mu.Unlock()
	if n, err := m.store.ResetRunningDownloadTasks(ctx, time.Now().Unix()); err == nil && n > 0 {
		m.emit("download_enqueued", map[string]any{"recovered": n})
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.loop(ctx)
	}()
}

func (m *Manager) Stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.running = false
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Enqueue stores tasks after the caller has resolved them. Site-specific
// expansion (multi-page videos, per-page metadata) happens in the site driver.
func (m *Manager) Enqueue(ctx context.Context, accountID int64, quality int, inputs []store.DownloadTaskInput) (int, int, error) {
	now := time.Now().Unix()
	return m.store.CreateDownloadTasks(ctx, accountID, quality, inputs, now)
}

func (m *Manager) loop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		settings, err := m.settings(ctx)
		if err != nil {
			if !sleepCtx(ctx, 3*time.Second) {
				return
			}
			continue
		}
		concurrency := settings.Concurrency
		if concurrency < 1 {
			concurrency = 1
		}
		sem := make(chan struct{}, concurrency)
		for {
			if cur, err := m.settings(ctx); err != nil || cur.Concurrency != concurrency {
				break
			}
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			task, err := m.store.ClaimAnyDownloadTask(ctx, time.Now().Unix())
			if err != nil || task == nil {
				<-sem
				if !sleepCtx(ctx, 2*time.Second) {
					return
				}
				continue
			}
			m.wg.Add(1)
			go func(task *store.DownloadTask) {
				defer m.wg.Done()
				defer func() { <-sem }()
				taskCtx, cancel := context.WithCancel(ctx)
				m.mu.Lock()
				m.cancels[task.ID] = cancel
				m.mu.Unlock()
				defer func() {
					cancel()
					m.mu.Lock()
					delete(m.cancels, task.ID)
					m.mu.Unlock()
				}()
				m.process(taskCtx, task)
			}(task)
		}
	}
}

func (m *Manager) CancelTask(id int64) {
	m.mu.Lock()
	cancel := m.cancels[id]
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) process(ctx context.Context, task *store.DownloadTask) {
	// Rows written before the multi-site schema carry an empty site; take it
	// from the owning account so every downstream lookup is site-explicit.
	if task.Site == "" && task.AccountID != 0 {
		if acct, err := m.store.GetAccountByID(ctx, task.AccountID); err == nil && acct != nil {
			task.Site = acct.Site
		}
	}
	if drv, ok := sites.DriverFor(task.Site); ok {
		if _, direct := drv.(sites.DirectDriver); direct {
			m.processDirect(ctx, task)
			return
		}
	}
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
			// Paused/canceled or shutting down: leave the task state to the
			// HTTP handler (paused/canceled) or to startup recovery (running).
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
	client, err := m.provider(ctx, task.AccountID)
	if err != nil {
		fail(err)
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
	play, err := client.PlayURL(ctx, task.RemoteID, task.PartID, task.Quality)
	if err != nil {
		fail(err)
		return
	}
	video, audio, quality := play.SelectStreams(task.Quality, settings.Codecs)
	if video == nil {
		fail(errors.New("没有可用的视频流"))
		return
	}

	tmpDir = filepath.Join(settings.MediaDir, ".magpiesync-tmp", strconv.FormatInt(task.ID, 10))
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		fail(err)
		return
	}

	videoPath := filepath.Join(tmpDir, "video.m4s")
	videoTotal, err := m.fetchStream(ctx, task, append([]string{video.URL}, video.BackupURLs...), videoPath, 0, settings, mediaHTTP, nil)
	if err != nil {
		fail(err)
		return
	}
	audioPath := ""
	audioTotal := int64(0)
	if audio != nil {
		audioPath = filepath.Join(tmpDir, "audio.m4s")
		audioTotal, err = m.fetchStream(ctx, task, append([]string{audio.URL}, audio.BackupURLs...), audioPath, videoTotal, settings, mediaHTTP, nil)
		if err != nil {
			fail(err)
			return
		}
	}

	outPath := buildOutputPath(settings, task)
	if task.FilePath != outPath {
		if _, statErr := os.Stat(outPath); statErr == nil {
			outPath = strings.TrimSuffix(outPath, filepath.Ext(outPath)) + " [" + task.RemoteID + "]" + filepath.Ext(outPath)
		}
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fail(err)
		return
	}
	finalTmp := filepath.Join(tmpDir, "final.mp4")
	if audioPath != "" {
		if err := m.merge(ctx, settings.FFmpegPath, videoPath, audioPath, finalTmp); err != nil {
			fail(err)
			return
		}
	} else {
		finalTmp = videoPath
	}
	_ = os.Remove(outPath)
	if err := os.Rename(finalTmp, outPath); err != nil {
		if err := copyFile(finalTmp, outPath); err != nil {
			fail(err)
			return
		}
	}
	size := fileSize(outPath)
	sidecarErr := m.writeSidecars(ctx, client, settings, task, outPath, mediaHTTP)
	if ctx.Err() != nil {
		if current, getErr := m.store.GetDownloadTask(context.Background(), task.ID); getErr == nil && current.State == "canceled" {
			cleanup()
		}
		return
	}
	cleanup()
	_ = m.store.UpdateDownloadProgress(ctx, task.ID, 1, size, size, time.Now().Unix())
	_ = m.store.FinishDownloadTask(ctx, task.ID, "done", outPath, sidecarErr, quality, codecName(video.CodecID), size, time.Now().Unix())
	m.emit("download_done", m.taskEvent(task.ID, "done", quality, ""))
	m.emit("download_progress", map[string]any{
		"id":             task.ID,
		"state":          "done",
		"progress":       1,
		"file_path":      outPath,
		"actual_quality": quality,
		"codec":          codecName(video.CodecID),
		"total_bytes":    videoTotal + audioTotal,
	})
}

func (m *Manager) fetchStream(ctx context.Context, task *store.DownloadTask, urls []string, dest string, base int64, settings Settings, hc *http.Client, headers http.Header) (int64, error) {
	marker := dest + ".complete"
	if fileSize(dest) > 0 && fileSize(marker) > 0 {
		return base + fileSize(dest), nil
	}
	var lastErr error
	for _, rawURL := range urls {
		if rawURL == "" {
			continue
		}
		if settings.ChunkConcurrency > 1 && fileSize(dest) == 0 {
			if total, err := m.fetchParallel(ctx, task, rawURL, dest, base, settings.ChunkConcurrency, settings.ChunkThreshold, hc, headers); err == nil {
				_ = os.WriteFile(marker, []byte("1"), 0o644)
				return total, nil
			}
			// A failed parallel probe/attempt must not leave a partial file
			// that would make the serial fallback think it can resume.
			_ = os.Remove(dest)
		}
		_, totalSize, err := m.fetchOne(ctx, task, rawURL, dest, base, 0, hc, headers)
		if err == nil {
			_ = os.WriteFile(marker, []byte("1"), 0o644)
			return totalSize, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的下载地址")
	}
	return 0, lastErr
}

func (m *Manager) fetchParallel(ctx context.Context, task *store.DownloadTask, rawURL, dest string, base int64, concurrency int, threshold int64, hc *http.Client, headers http.Header) (int64, error) {
	if concurrency < 2 {
		return 0, errors.New("parallel download disabled")
	}
	probe, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	m.setMediaHeaders(probe, headers, task.Site)
	probe.Header.Set("Range", "bytes=0-0")
	resp, err := hc.Do(probe)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return 0, errors.New("server does not support ranges")
	}
	size := parseContentRangeTotal(resp.Header.Get("Content-Range"))
	if size <= 0 || size < threshold {
		return 0, errors.New("file below chunk threshold")
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		return 0, err
	}

	if size < int64(concurrency) {
		return 0, errors.New("file too small for chunked download")
	}
	chunkSize := size / int64(concurrency)
	if chunkSize < 1 {
		chunkSize = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		written  int64
		last     time.Time
		firstErr error
	)
	report := func() {
		now := time.Now()
		if now.Sub(last) < 400*time.Millisecond {
			return
		}
		last = now
		done := base + written
		progress := 0.0
		if size > 0 {
			progress = float64(written) / float64(size)
		}
		_ = m.store.UpdateDownloadProgress(ctx, task.ID, progress, done, base+size, time.Now().Unix())
		m.emit("download_progress", map[string]any{
			"id": task.ID, "state": "running", "progress": progress,
			"downloaded": done, "total_bytes": base + size, "title": task.VideoTitle,
		})
	}
	for i := 0; i < concurrency; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize
		if i == concurrency-1 || end > size {
			end = size
		}
		wg.Add(1)
		go func(start, end int64) {
			defer wg.Done()
			err := m.fetchRange(ctx, rawURL, f, start, end, func(n int) {
				mu.Lock()
				written += int64(n)
				report()
				mu.Unlock()
			}, hc, headers, task.Site)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				cancel()
			}
		}(start, end)
	}
	wg.Wait()
	if firstErr != nil {
		_ = os.Remove(dest)
		return 0, firstErr
	}
	if written != size {
		_ = os.Remove(dest)
		return 0, fmt.Errorf("分片下载大小不匹配：%d/%d", written, size)
	}
	return base + size, nil
}

func (m *Manager) fetchRange(ctx context.Context, rawURL string, f *os.File, start, end int64, add func(int), hc *http.Client, headers http.Header, siteID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	m.setMediaHeaders(req, headers, siteID)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end-1))
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("分片请求失败：HTTP %d", resp.StatusCode)
	}
	expected := end - start
	w := &rangeWriter{f: f, off: start, expected: expected, add: add}
	n, err := io.Copy(w, resp.Body)
	if err != nil {
		return err
	}
	if n != expected {
		return fmt.Errorf("分片长度不匹配：%d/%d", n, expected)
	}
	return nil
}

type rangeWriter struct {
	f        *os.File
	off      int64
	expected int64
	written  int64
	add      func(int)
}

func (w *rangeWriter) Write(p []byte) (int, error) {
	if w.written+int64(len(p)) > w.expected {
		p = p[:w.expected-w.written]
	}
	n, err := w.f.WriteAt(p, w.off)
	w.off += int64(n)
	w.written += int64(n)
	if n > 0 && w.add != nil {
		w.add(n)
	}
	return n, err
}

func (m *Manager) setMediaHeaders(req *http.Request, headers http.Header, siteID string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	if len(headers) == 0 {
		if site, ok := sites.Get(siteID); ok {
			if site.DownloadInfo.Referer != "" {
				req.Header.Set("Referer", site.DownloadInfo.Referer)
			}
			if site.DownloadInfo.Origin != "" {
				req.Header.Set("Origin", site.DownloadInfo.Origin)
			}
		}
		return
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Set(key, value)
		}
	}
}

func parseContentRangeTotal(v string) int64 {
	slash := strings.LastIndexByte(v, '/')
	if slash < 0 || slash+1 >= len(v) {
		return 0
	}
	n, _ := strconv.ParseInt(v[slash+1:], 10, 64)
	return n
}

func (m *Manager) fetchOne(ctx context.Context, task *store.DownloadTask, rawURL, dest string, base, total int64, hc *http.Client, headers http.Header) (int64, int64, error) {
	offset := fileSize(dest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, 0, err
	}
	m.setMediaHeaders(req, headers, task.Site)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0 {
		return offset, base + offset, nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, 0, fmt.Errorf("下载失败：HTTP %d", resp.StatusCode)
	}
	appendMode := offset > 0 && resp.StatusCode == http.StatusPartialContent
	if !appendMode {
		offset = 0
	}
	if cl := resp.ContentLength; cl > 0 {
		total = base + offset + cl
	} else {
		total = base + offset
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	if appendMode {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return 0, 0, err
		}
	} else if err := f.Truncate(0); err != nil {
		return 0, 0, err
	}

	written := offset
	last := time.Now()
	buf := make([]byte, 256*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return written, total, err
			}
			written += int64(n)
			if time.Since(last) > 500*time.Millisecond {
				last = time.Now()
				done := base + written
				var progress float64
				if total > 0 {
					progress = float64(done) / float64(total)
				}
				_ = m.store.UpdateDownloadProgress(ctx, task.ID, progress, done, total, time.Now().Unix())
				m.emit("download_progress", map[string]any{
					"id":          task.ID,
					"state":       "running",
					"progress":    progress,
					"downloaded":  done,
					"total_bytes": total,
					"title":       task.VideoTitle,
				})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return written, total, readErr
		}
	}
	if total <= 0 {
		total = base + written
	}
	return written, total, nil
}

func (m *Manager) merge(ctx context.Context, ffmpegPath, video, audio, out string) error {
	path := strings.TrimSpace(ffmpegPath)
	if path == "" {
		path = "ffmpeg"
	}
	bin, err := exec.LookPath(path)
	if err != nil {
		return fmt.Errorf("未找到 FFmpeg（%s），请安装 FFmpeg 或在设置中填写可执行文件路径", path)
	}
	cmd := exec.CommandContext(ctx, bin,
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", video, "-i", audio,
		"-c", "copy", "-movflags", "+faststart", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("FFmpeg 合并失败：%s", msg)
	}
	return nil
}

func (m *Manager) fetchImage(ctx context.Context, rawURL, dest string, hc *http.Client, siteID string) error {
	referer := ""
	if site, ok := sites.Get(siteID); ok {
		referer = site.DownloadInfo.ImageReferer
		if referer == "" {
			referer = site.DownloadInfo.Referer
		}
	}
	return m.fetchImageWithReferer(ctx, rawURL, dest, hc, referer)
}

func (m *Manager) fetchImageWithReferer(ctx context.Context, rawURL, dest string, hc *http.Client, referer string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("封面下载失败：HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, io.LimitReader(resp.Body, 20<<20))
	return err
}

func (m *Manager) emit(kind string, data any) {
	if m.onEvent != nil {
		m.onEvent(kind, data)
	}
}

func (m *Manager) writeSidecars(ctx context.Context, client VideoClient, settings Settings, task *store.DownloadTask, mediaPath string, hc *http.Client) string {
	base := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	dir := filepath.Dir(mediaPath)
	archive := settings.ArchiveFor(task.Site)
	var warnings []string
	fail := func(kind string, err error) {
		if err != nil {
			warnings = append(warnings, kind+"："+err.Error())
		}
	}
	if archive.Cover && hasHTTPURL(task.CoverURL) {
		poster := filepath.Join(dir, "poster.jpg")
		if err := m.fetchImage(ctx, task.CoverURL, poster, hc, task.Site); err != nil {
			fail("封面", err)
		} else if err := copyFile(poster, filepath.Join(dir, "fanart.jpg")); err != nil {
			fail("背景图", err)
		}
	}
	if archive.NFO {
		if err := writeNFO(mediaPath, task); err != nil {
			fail("NFO", err)
		}
		if task.PageCount > 1 {
			if err := writeTVShowNFO(dir, task); err != nil {
				fail("剧集 NFO", err)
			}
		}
		if task.AuthorName != "" {
			if err := writePersonNFO(dir, task); err != nil {
				fail("UP主 NFO", err)
			}
		}
	}
	if archive.Subtitles {
		subs, err := client.Subtitles(ctx, task.RemoteID, task.PartID)
		if err != nil {
			fail("字幕", err)
		} else {
			for _, sub := range subs {
				if sub.AI && !archive.AISubtitles {
					continue
				}
				items, err := client.SubtitleBody(ctx, sub.URL)
				if err != nil {
					fail("字幕 "+sub.Lan, err)
					continue
				}
				lan := sanitizeSegment(sub.Lan)
				if lan == "" {
					lan = "und"
				}
				path := base + "." + lan + ".srt"
				if err := os.WriteFile(path, media.SubtitleSRT(items), 0o644); err != nil {
					fail("字幕 "+sub.Lan, err)
				}
			}
		}
	}
	if archive.Danmaku {
		items, err := client.Danmaku(ctx, task.PartID, task.Duration)
		if err != nil {
			fail("弹幕", err)
		} else if len(items) > 0 {
			path := base + ".zh-CN.default.ass"
			if err := os.WriteFile(path, media.DanmakuASS(items, 1920, 1080), 0o644); err != nil {
				fail("弹幕", err)
			}
		}
	}
	return strings.Join(warnings, "；")
}

// writeDirectSidecars writes the metadata/artwork that does not depend on a
// Some sites have no danmaku or subtitle endpoints, but
// their stored title, summary, tags, duration and cover can still produce the
// same NFO/poster/fanart layout.
func (m *Manager) writeDirectSidecars(ctx context.Context, settings Settings, task *store.DownloadTask, mediaPath string, hc *http.Client) string {
	dir := filepath.Dir(mediaPath)
	archive := settings.ArchiveFor(task.Site)
	var warnings []string
	fail := func(kind string, err error) {
		if err != nil {
			warnings = append(warnings, kind+"："+err.Error())
		}
	}
	if archive.Cover && hasHTTPURL(task.CoverURL) {
		poster := filepath.Join(dir, "poster.jpg")
		if err := m.fetchImageWithReferer(ctx, task.CoverURL, poster, hc, directImageReferer(task)); err != nil {
			fail("封面", err)
		} else if err := copyFile(poster, filepath.Join(dir, "fanart.jpg")); err != nil {
			fail("背景图", err)
		}
	}
	if archive.NFO {
		if err := writeNFO(mediaPath, task); err != nil {
			fail("NFO", err)
		}
		if task.AuthorName != "" {
			if err := writePersonNFO(dir, task); err != nil {
				fail("作者 NFO", err)
			}
		}
	}
	return strings.Join(warnings, "；")
}

func hasHTTPURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}

func directImageReferer(task *store.DownloadTask) string {
	if task.PageURL != "" {
		return task.PageURL
	}
	if site, ok := sites.Get(task.Site); ok {
		return site.DownloadInfo.ImageReferer
	}
	return ""
}

func actorRole(task *store.DownloadTask) string {
	if site, ok := sites.Get(task.Site); ok && site.DownloadInfo.ActorRole != "" {
		return site.DownloadInfo.ActorRole
	}
	return "作者"
}

func writeNFO(mediaPath string, task *store.DownloadTask) error {
	base := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath))
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8" standalone="yes"?>` + "\n")
	tag := "movie"
	if task.PageCount > 1 {
		tag = "episodedetails"
	}
	b.WriteString("<" + tag + ">\n")
	title := task.VideoTitle
	if task.PageCount > 1 {
		title = task.PageTitleOrVideo()
	}
	writeXMLTag(&b, "title", title)
	writeXMLTag(&b, "originaltitle", task.VideoTitle)
	if task.PageCount > 1 {
		writeXMLTag(&b, "showtitle", task.VideoTitle)
		writeXMLTag(&b, "season", "1")
		writeXMLTag(&b, "episode", strconv.Itoa(task.PageNo))
	}
	plot := task.Summary
	if plot == "" {
		plot = task.VideoTitle
	}
	writeXMLTag(&b, "plot", plot)
	if task.Duration > 0 {
		writeXMLTag(&b, "runtime", strconv.Itoa((task.Duration+59)/60))
	}
	if task.PubTime > 0 {
		writeXMLTag(&b, "premiered", formatDate(task.PubTime))
		writeXMLTag(&b, "year", formatDate(task.PubTime)[:4])
	}
	for _, tagName := range task.Tags {
		writeXMLTag(&b, "tag", tagName)
	}
	if task.AuthorName != "" {
		b.WriteString("<actor>\n")
		writeXMLTag(&b, "name", task.AuthorName)
		writeXMLTag(&b, "role", actorRole(task))
		b.WriteString("</actor>\n")
	}
	writeXMLTag(&b, "uniqueid", task.RemoteID)
	if fileSize(filepath.Join(filepath.Dir(mediaPath), "poster.jpg")) > 0 {
		writeXMLTag(&b, "thumb", "poster.jpg")
	}
	b.WriteString("</" + tag + ">\n")
	return os.WriteFile(base+".nfo", []byte(b.String()), 0o644)
}

func writeTVShowNFO(dir string, task *store.DownloadTask) error {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8" standalone="yes"?>` + "\n")
	b.WriteString("<tvshow>\n")
	writeXMLTag(&b, "title", task.VideoTitle)
	writeXMLTag(&b, "plot", task.Summary)
	writeXMLTag(&b, "season", "1")
	if task.AuthorName != "" {
		b.WriteString("<actor>\n")
		writeXMLTag(&b, "name", task.AuthorName)
		writeXMLTag(&b, "role", actorRole(task))
		b.WriteString("</actor>\n")
	}
	b.WriteString("</tvshow>\n")
	return os.WriteFile(filepath.Join(dir, "tvshow.nfo"), []byte(b.String()), 0o644)
}

func writePersonNFO(dir string, task *store.DownloadTask) error {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8" standalone="yes"?>` + "\n")
	b.WriteString("<person>\n")
	writeXMLTag(&b, "name", task.AuthorName)
	if task.AuthorID != "" {
		writeXMLTag(&b, "uniqueid", task.AuthorID)
	}
	biography := actorRole(task) + "的作品"
	writeXMLTag(&b, "biography", biography)
	b.WriteString("</person>\n")
	return os.WriteFile(filepath.Join(dir, "person.nfo"), []byte(b.String()), 0o644)
}

func writeXMLTag(b *strings.Builder, tag, value string) {
	if value == "" {
		return
	}
	b.WriteString("<" + tag + ">")
	_ = xml.EscapeText(b, []byte(value))
	b.WriteString("</" + tag + ">\n")
}

func (m *Manager) taskEvent(id int64, state string, quality int, errMsg string) map[string]any {
	return map[string]any{
		"id":             id,
		"state":          state,
		"actual_quality": quality,
		"error":          errMsg,
	}
}

func retryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "FFmpeg"),
		strings.Contains(msg, "没有可用"),
		strings.Contains(msg, "no downloadable"),
		strings.Contains(msg, "no usable"),
		strings.Contains(msg, "Cloudflare"),
		strings.Contains(msg, "authentication"),
		strings.Contains(msg, "HTTP 400"),
		strings.Contains(msg, "HTTP 401"),
		strings.Contains(msg, "HTTP 403"),
		strings.Contains(msg, "HTTP 404"):
		return false
	default:
		return true
	}
}

func buildOutputPath(settings Settings, task *store.DownloadTask) string {
	vars := map[string]string{
		"id":          task.RemoteID,
		"remote_id":   task.RemoteID,
		"title":       task.VideoTitle,
		"author_name": task.AuthorName,
		"author_id":   task.AuthorID,
		"pubtime":     formatDate(task.PubTime),
		"fav_time":    formatDate(task.FavTime),
		"ptitle":      task.PageTitle,
		"page_title":  task.PageTitle,
		"pid":         strconv.Itoa(task.PageNo),
		"page_no":     strconv.Itoa(task.PageNo),
		"part_id":     strconv.FormatInt(task.PartID, 10),
	}
	naming := settings.NamingFor(task.Site)
	videoDir := renderTemplate(naming.Video, vars)
	if siteDir := sanitizeSegment(task.Site); siteDir != "" {
		videoDir = filepath.Join(siteDir, videoDir)
	}
	pageName := renderTemplate(naming.Page, vars)
	if pageName == "" {
		pageName = sanitizeValue(task.PageTitle)
	}
	if pageName == "" {
		pageName = sanitizeValue(task.VideoTitle)
	}
	if task.PageCount > 1 && !strings.Contains(naming.Page, "pid") && !strings.Contains(naming.Page, "page_no") {
		pageName = fmt.Sprintf("%s - P%02d", pageName, task.PageNo)
	}
	return filepath.Join(settings.MediaDir, videoDir, safePath(pageName)+".mp4")
}

func renderTemplate(tmpl string, vars map[string]string) string {
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", sanitizeValue(v))
	}
	out = strings.ReplaceAll(out, "{{ page_title }}", sanitizeValue(vars["page_title"]))
	out = strings.ReplaceAll(out, "{{ title }}", sanitizeValue(vars["title"]))
	out = strings.ReplaceAll(out, "{{ author_name }}", sanitizeValue(vars["author_name"]))
	if strings.TrimSpace(out) == "" {
		return ""
	}
	return safePath(out)
}

// sanitizeValue makes one template variable safe inside a path segment.
// Slashes inside titles must not silently create extra directories.
func sanitizeValue(v string) string {
	return sanitizeSegment(v)
}

func sanitizeSegment(v string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(v) {
		switch {
		case r < 32:
			continue
		case r == '/', r == '\\', strings.ContainsRune(`<>:"|?*`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	s := strings.Trim(b.String(), ". ")
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	switch lower {
	case "con", "prn", "aux", "nul", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		s = "_" + s
	}
	if runes := []rune(s); len(runes) > 120 {
		s = string(runes[:120])
	}
	return s
}

func safePath(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, "\\", "/")
	parts := strings.Split(v, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "." || p == ".." {
			continue
		}
		if s := sanitizeSegment(p); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return "untitled"
	}
	return strings.Join(out, "/")
}

func formatDate(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("2006-01-02")
}

func codecName(id int) string {
	switch id {
	case 7:
		return "avc"
	case 12:
		return "hevc"
	case 13:
		return "av1"
	default:
		return ""
	}
}

func fileSize(path string) int64 {
	if st, err := os.Stat(path); err == nil {
		return st.Size()
	}
	return 0
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
