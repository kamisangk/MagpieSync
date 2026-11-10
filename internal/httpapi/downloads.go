package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"magpiesync/internal/download"
	"magpiesync/internal/scheduler"
	"magpiesync/internal/sites"
	"magpiesync/internal/store"
)

var errNoDownloadableItems = errors.New("没有可下载的条目")

var allowedQualities = map[int]bool{
	0: true, 16: true, 32: true, 64: true, 74: true,
	80: true, 112: true, 116: true, 120: true, 125: true, 126: true, 127: true,
}

func (s *Server) SetDownloadManager(m *download.Manager) {
	s.download = m
}

func settingsDTO(d download.Settings) map[string]any {
	return map[string]any{
		"media_dir":          d.MediaDir,
		"ffmpeg":             d.FFmpegPath,
		"site_qualities":     d.SiteQualities,
		"missing_check":      d.MissingCheck,
		"archive":            d.Archive,
		"naming":             d.Naming,
		"codecs":             strings.Join(d.Codecs, ","),
		"concurrency":        d.Concurrency,
		"chunk_concurrency":  d.ChunkConcurrency,
		"chunk_threshold_mb": d.ChunkThreshold >> 20,
	}
}

// siteQualityOptions returns the quality choices a site supports. Every site
// offers 0 ("automatic") which means the highest available stream.
func siteQualityOptions(site sites.Site) []map[string]any {
	out := []map[string]any{{"value": 0, "label": "自动（最高可用）"}}
	for _, q := range site.DownloadQualities {
		out = append(out, map[string]any{"value": q.Value, "label": q.Label})
	}
	return out
}

func siteQualityOptionsByID() map[string][]map[string]any {
	out := map[string][]map[string]any{}
	for _, site := range sites.List() {
		if site.Download {
			out[site.ID] = siteQualityOptions(site)
		}
	}
	return out
}

func ffmpegAvailable(path string) bool {
	if strings.TrimSpace(path) == "" {
		path = "ffmpeg"
	}
	_, err := exec.LookPath(path)
	return err == nil
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	schedule, _, err := s.store.GetSetting(r.Context(), scheduler.Key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法读取同步计划")
		return
	}
	dto := func(d download.Settings) map[string]any {
		v := settingsDTO(d)
		v["sync_schedule"] = schedule
		return v
	}
	switch r.Method {
	case http.MethodGet:
		d, err := download.LoadSettings(r.Context(), s.store)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"settings":             dto(d),
			"ffmpeg_available":     ffmpegAvailable(d.FFmpegPath),
			"site_quality_options": siteQualityOptionsByID(),
		})
	case http.MethodPut:
		var body struct {
			SyncSchedule     *string                             `json:"sync_schedule"`
			MediaDir         *string                             `json:"media_dir"`
			FFmpeg           *string                             `json:"ffmpeg"`
			SiteQualities    *map[string]int                     `json:"site_qualities"`
			MissingCheck     *map[string]bool                    `json:"missing_check"`
			Archive          *map[string]download.ArchiveOptions `json:"archive"`
			Naming           *map[string]download.Naming         `json:"naming"`
			Codecs           *string                             `json:"codecs"`
			Concurrency      *int                                `json:"concurrency"`
			ChunkConcurrency *int                                `json:"chunk_concurrency"`
			ChunkThresholdMB *int                                `json:"chunk_threshold_mb"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		var plan cron.Schedule
		if body.SyncSchedule != nil {
			schedule = strings.TrimSpace(*body.SyncSchedule)
			plan, err = scheduler.Parse(schedule)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		d, err := download.LoadSettings(r.Context(), s.store)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if body.MediaDir != nil {
			v := strings.TrimSpace(*body.MediaDir)
			if v == "" {
				writeError(w, http.StatusBadRequest, "媒体目录不能为空")
				return
			}
			out, err := filepath.Abs(v)
			if err != nil {
				writeError(w, http.StatusBadRequest, "媒体目录无效")
				return
			}
			if err := os.MkdirAll(out, 0o755); err != nil {
				writeError(w, http.StatusBadRequest, "无法创建媒体目录："+err.Error())
				return
			}
			d.MediaDir = out
		}
		if body.FFmpeg != nil {
			d.FFmpegPath = strings.TrimSpace(*body.FFmpeg)
			if d.FFmpegPath == "" {
				d.FFmpegPath = "ffmpeg"
			}
		}
		if body.SiteQualities != nil {
			for siteID, quality := range *body.SiteQualities {
				site, ok := sites.Get(siteID)
				if !ok || !site.Download {
					writeError(w, http.StatusBadRequest, "不支持的站点："+siteID)
					return
				}
				if !qualityAllowed(site, quality) {
					writeError(w, http.StatusBadRequest, "站点 "+site.Name+" 不支持该清晰度")
					return
				}
				d.SiteQualities[siteID] = quality
			}
		}
		if body.MissingCheck != nil {
			for siteID, enabled := range *body.MissingCheck {
				site, ok := sites.Get(siteID)
				if !ok || !site.Download {
					writeError(w, http.StatusBadRequest, "不支持的站点："+siteID)
					return
				}
				if enabled {
					d.MissingCheck[siteID] = true
				} else {
					delete(d.MissingCheck, siteID)
				}
			}
		}
		if body.Archive != nil {
			for siteID, opts := range *body.Archive {
				site, ok := sites.Get(siteID)
				if !ok || !site.Download {
					writeError(w, http.StatusBadRequest, "不支持的站点："+siteID)
					return
				}
				// NFO and cover are available everywhere; the remaining extras
				// are dropped for sites that do not declare them.
				if !siteHasArchiveExtras(site, "danmaku") {
					opts.Danmaku = false
				}
				if !siteHasArchiveExtras(site, "subtitles") {
					opts.Subtitles = false
				}
				if !siteHasArchiveExtras(site, "subtitles_ai") {
					opts.AISubtitles = false
				}
				d.Archive[siteID] = opts
			}
		}
		if body.Naming != nil {
			for siteID, naming := range *body.Naming {
				site, ok := sites.Get(siteID)
				if !ok || !site.Download {
					writeError(w, http.StatusBadRequest, "不支持的站点："+siteID)
					return
				}
				video := strings.TrimSpace(naming.Video)
				if video == "" {
					writeError(w, http.StatusBadRequest, site.Name+" 的视频命名模板不能为空")
					return
				}
				d.Naming[siteID] = download.Naming{Video: video, Page: strings.TrimSpace(naming.Page)}
			}
		}
		if body.Codecs != nil {
			d.Codecs = splitCodecs(*body.Codecs)
			if len(d.Codecs) == 0 {
				writeError(w, http.StatusBadRequest, "编码偏好不能为空")
				return
			}
		}
		if body.Concurrency != nil {
			if *body.Concurrency < 1 || *body.Concurrency > 8 {
				writeError(w, http.StatusBadRequest, "并发数必须在 1-8 之间")
				return
			}
			d.Concurrency = *body.Concurrency
		}
		if body.ChunkConcurrency != nil {
			if *body.ChunkConcurrency < 1 || *body.ChunkConcurrency > 8 {
				writeError(w, http.StatusBadRequest, "分片并发数必须在 1-8 之间")
				return
			}
			d.ChunkConcurrency = *body.ChunkConcurrency
		}
		if body.ChunkThresholdMB != nil {
			if *body.ChunkThresholdMB < 0 || *body.ChunkThresholdMB > 4096 {
				writeError(w, http.StatusBadRequest, "分片阈值必须在 0-4096 MB 之间")
				return
			}
			d.ChunkThreshold = int64(*body.ChunkThresholdMB) << 20
		}
		values := d.Values()
		if body.SyncSchedule != nil {
			values[scheduler.Key] = schedule
		}
		if err := s.store.SetSettings(r.Context(), values, time.Now().Unix()); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if body.SyncSchedule != nil && s.scheduleUpdater != nil {
			s.scheduleUpdater(plan)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"settings":         dto(d),
			"ffmpeg_available": ffmpegAvailable(d.FFmpegPath),
			"restart_hint":     "设置已保存，新任务立即生效",
		})
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func splitCodecs(v string) []string {
	out := make([]string, 0, 3)
	for _, p := range strings.Split(v, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "avc" || p == "hevc" || p == "av1" {
			out = append(out, p)
		}
	}
	return out
}

// siteHasArchiveExtras reports whether a site declares the given sidecar file.
func siteHasArchiveExtras(site sites.Site, extra string) bool {
	for _, v := range site.ArchiveExtras {
		if v == extra {
			return true
		}
	}
	return false
}

func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	acct, err := s.currentAccount(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return
	}
	query := r.URL.Query()
	pageMode := query.Has("filter") || query.Has("page") || query.Has("page_size")
	state := query.Get("state")
	limit := 100
	if v := query.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	var filter store.DownloadFilter
	if pageMode {
		if state != "" || query.Has("limit") {
			writeError(w, http.StatusBadRequest, "filter/page cannot be combined with state/limit")
			return
		}
		if query.Has("page") && query.Get("page") == "" || query.Has("page_size") && query.Get("page_size") == "" {
			writeError(w, http.StatusBadRequest, "invalid download pagination")
			return
		}
		page, pageErr := optionalPositiveInt(query.Get("page"), 1)
		pageSize, sizeErr := optionalPositiveInt(query.Get("page_size"), 25)
		if pageErr != nil || sizeErr != nil || pageSize > 100 {
			writeError(w, http.StatusBadRequest, "invalid download pagination")
			return
		}
		maxInt := int(^uint(0) >> 1)
		if page-1 > maxInt/pageSize {
			writeError(w, http.StatusBadRequest, "invalid download pagination")
			return
		}
		filter.Page, filter.PageSize = page, pageSize
		switch q := query.Get("filter"); q {
		case "", "all":
		case "active":
			filter.States = []string{"pending", "running"}
		case "done", "paused", "canceled", "failed", "missing":
			filter.States = []string{q}
		default:
			writeError(w, http.StatusBadRequest, "invalid download filter")
			return
		}
	}
	var tasks []store.DownloadTask
	var total int
	if pageMode {
		tasks, total, err = s.store.QueryDownloadTasks(r.Context(), acct.ID, filter)
	} else {
		tasks, err = s.store.ListDownloadTasks(r.Context(), acct.ID, state, limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d, err := download.LoadSettings(r.Context(), s.store)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Missing-file check: for sites where it is enabled, re-stat each visible
	// finished task's output. A done task whose file disappeared is marked
	// missing; a missing task whose file reappeared is restored to done.
	now := store.UnixNow()
	for _, t := range tasks {
		if !d.MissingCheckFor(t.Site) {
			continue
		}
		if t.State != "done" && t.State != "missing" {
			continue
		}
		if t.FilePath == "" {
			continue
		}
		_, statErr := os.Stat(t.FilePath)
		switch {
		case statErr == nil && t.State == "missing":
			_ = s.store.ReappearDownloadTask(r.Context(), t.ID, now)
		case statErr != nil && t.State == "done":
			_ = s.store.MarkDownloadTaskMissing(r.Context(), t.ID, "output file not found", now)
		}
	}
	counts, err := s.store.DownloadCounts(r.Context(), acct.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Re-read after the reconcile so the DTO reflects the updated state.
	if pageMode {
		tasks, total, err = s.store.QueryDownloadTasks(r.Context(), acct.ID, filter)
		if err == nil {
			lastPage := 1
			if total > 0 {
				lastPage = (total + filter.PageSize - 1) / filter.PageSize
			}
			if filter.Page > lastPage {
				filter.Page = lastPage
				tasks, total, err = s.store.QueryDownloadTasks(r.Context(), acct.ID, filter)
			}
		}
	} else {
		tasks, err = s.store.ListDownloadTasks(r.Context(), acct.ID, state, limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, downloadTaskDTO(t))
	}
	response := map[string]any{
		"tasks":            out,
		"counts":           counts,
		"media_dir":        d.MediaDir,
		"site_qualities":   d.SiteQualities,
		"ffmpeg_available": ffmpegAvailable(d.FFmpegPath),
		"running":          s.download != nil && s.download.Running(),
	}
	if pageMode {
		response["total"] = total
		response["page"] = filter.Page
		response["page_size"] = filter.PageSize
	}
	writeJSON(w, http.StatusOK, response)
}

func optionalPositiveInt(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, errors.New("must be a positive integer")
	}
	return n, nil
}

func downloadTaskDTO(t store.DownloadTask) map[string]any {
	return map[string]any{
		"id":             t.ID,
		"site":           t.Site,
		"page_url":       t.PageURL,
		"remote_id":      t.RemoteID,
		"part_id":        t.PartID,
		"video_title":    t.VideoTitle,
		"page_title":     t.PageTitle,
		"page_no":        t.PageNo,
		"page_count":     t.PageCount,
		"duration":       t.Duration,
		"cover_url":      t.CoverURL,
		"author_name":    t.AuthorName,
		"source_title":   t.SourceTitle,
		"quality":        t.Quality,
		"actual_quality": t.ActualQuality,
		"codec":          t.Codec,
		"state":          t.State,
		"missing":        t.State == "missing",
		"progress":       t.Progress,
		"downloaded":     t.Downloaded,
		"total_bytes":    t.TotalBytes,
		"file_path":      t.FilePath,
		"error":          t.Error,
		"attempts":       t.Attempts,
		"created_at":     t.CreatedAt,
		"updated_at":     t.UpdatedAt,
	}
}

func (s *Server) handleEnqueueDownloads(w http.ResponseWriter, r *http.Request) {
	if s.download == nil {
		writeError(w, http.StatusServiceUnavailable, "下载器未初始化")
		return
	}
	acct, err := s.currentAccount(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return
	}
	site, ok := sites.Get(acct.Site)
	if !ok || !site.Download {
		writeError(w, http.StatusBadRequest, "该站点暂不支持下载")
		return
	}
	var body struct {
		FolderID int64  `json:"folder_id"`
		ItemID   int64  `json:"item_id"`
		RemoteID string `json:"remote_id"`
		Quality  *int   `json:"quality"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	settings, err := download.LoadSettings(r.Context(), s.store)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	quality := settings.QualityFor(site.ID)
	if body.Quality != nil {
		if !qualityAllowed(site, *body.Quality) {
			writeError(w, http.StatusBadRequest, "不支持的清晰度")
			return
		}
		quality = *body.Quality
	}
	drv, drvOK := sites.DriverFor(acct.Site)
	_, isDirect := drv.(sites.DirectDriver)
	var items []store.Item
	sourceTitle := "手动下载"
	switch {
	case body.ItemID > 0:
		item, err := s.store.GetItem(r.Context(), acct.ID, body.ItemID)
		if err != nil {
			writeError(w, http.StatusNotFound, "条目不存在")
			return
		}
		items = []store.Item{*item}
		sourceTitle = item.Title
	case body.FolderID > 0:
		items, err = s.store.ListFolderItems(r.Context(), acct.ID, body.FolderID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		folders, _ := s.store.ListFolders(r.Context(), acct.ID)
		for _, f := range folders {
			if f.ID == body.FolderID {
				sourceTitle = f.Title
				break
			}
		}
	case strings.TrimSpace(body.RemoteID) != "":
		if isDirect {
			writeError(w, http.StatusBadRequest, "该站点请使用 item_id 或 folder_id")
			return
		}
		items = []store.Item{{RemoteID: strings.TrimSpace(body.RemoteID)}}
	default:
		writeError(w, http.StatusBadRequest, "需要 item_id、folder_id 或 remote_id")
		return
	}
	if len(items) == 0 {
		writeError(w, http.StatusBadRequest, "没有可下载的条目")
		return
	}
	if len(items) > 2000 {
		writeError(w, http.StatusBadRequest, "单次最多 2000 个视频")
		return
	}
	accountID := acct.ID
	if isDirect {
		inputs, err := directTaskInputs(acct.Site, sourceTitle, items)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		created, skipped, err := s.download.Enqueue(r.Context(), accountID, quality, inputs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"queued_videos": len(inputs),
			"created":       created,
			"skipped":       skipped,
			"quality":       quality,
		})
		return
	}
	builder, ok := drv.(sites.TaskBuilder)
	if !ok || !drvOK {
		writeError(w, http.StatusBadRequest, "该站点不支持批量下载")
		return
	}
	acctCtx, err := s.accountContext(r.Context(), acct)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取凭据失败："+err.Error())
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		inputs, err := builder.BuildTasks(ctx, acctCtx, sites.TaskRequest{
			Items:       items,
			SourceTitle: sourceTitle,
			Quality:     quality,
		})
		created, skipped := 0, 0
		if err == nil {
			created, skipped, err = s.download.Enqueue(ctx, accountID, quality, inputs)
		}
		event := map[string]any{
			"type":         "download_enqueued",
			"created":      created,
			"skipped":      skipped,
			"videos":       len(items),
			"source_title": sourceTitle,
		}
		if err != nil {
			event["error"] = err.Error()
		}
		s.hub.Publish(event)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"queued_videos": len(items),
		"quality":       quality,
		"async":         true,
	})
}

func qualityAllowed(site sites.Site, quality int) bool {
	if quality == 0 {
		return true
	}
	if len(site.DownloadQualities) > 0 {
		for _, q := range site.DownloadQualities {
			if q.Value == quality {
				return true
			}
		}
		return false
	}
	return allowedQualities[quality]
}

func directTaskInputs(siteID, sourceTitle string, items []store.Item) ([]store.DownloadTaskInput, error) {
	inputs := make([]store.DownloadTaskInput, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.RemoteID)
		if id == "" || strings.TrimSpace(item.URL) == "" {
			continue
		}
		inputs = append(inputs, store.DownloadTaskInput{
			Site:        siteID,
			PageURL:     item.URL,
			RemoteID:    id,
			VideoTitle:  item.Title,
			PageNo:      1,
			PageCount:   1,
			Duration:    item.Duration,
			CoverURL:    item.CoverURL,
			AuthorID:    item.AuthorID,
			AuthorName:  item.AuthorName,
			PubTime:     item.PublishedAt,
			SourceTitle: sourceTitle,
			Summary:     item.Summary,
		})
	}
	if len(inputs) == 0 {
		return nil, errNoDownloadableItems
	}
	return inputs, nil
}

func (s *Server) handleRetryDownload(w http.ResponseWriter, r *http.Request) {
	if s.download == nil {
		writeError(w, http.StatusServiceUnavailable, "下载器未初始化")
		return
	}
	acct, err := s.currentAccount(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	task, err := s.store.GetDownloadTask(r.Context(), id)
	if err != nil || task.AccountID != acct.ID {
		writeError(w, http.StatusNotFound, "下载任务不存在")
		return
	}
	if err := s.store.ResetDownloadTask(r.Context(), id, time.Now().Unix()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) loadDownloadTask(w http.ResponseWriter, r *http.Request) (*store.DownloadTask, bool) {
	acct, err := s.currentAccount(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return nil, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return nil, false
	}
	task, err := s.store.GetDownloadTask(r.Context(), id)
	if err != nil || task.AccountID != acct.ID {
		writeError(w, http.StatusNotFound, "下载任务不存在")
		return nil, false
	}
	return task, true
}

func (s *Server) handlePauseDownload(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadDownloadTask(w, r)
	if !ok {
		return
	}
	if err := s.store.PauseDownloadTask(r.Context(), task.ID, time.Now().Unix()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.download != nil {
		s.download.CancelTask(task.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "paused"})
}

func (s *Server) handleResumeDownload(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadDownloadTask(w, r)
	if !ok {
		return
	}
	if err := s.store.ResumeDownloadTask(r.Context(), task.ID, time.Now().Unix()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "pending"})
}

func (s *Server) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadDownloadTask(w, r)
	if !ok {
		return
	}
	if err := s.store.CancelDownloadTask(r.Context(), task.ID, time.Now().Unix()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.download != nil {
		s.download.CancelTask(task.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "canceled"})
}
