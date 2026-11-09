package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"magpiesync/internal/download"
	"magpiesync/internal/sites"
	"magpiesync/internal/store"
)

func (s *Server) handleFolderAutoDownload(w http.ResponseWriter, r *http.Request) {
	account, ok := s.requireAccount(w, r)
	if !ok {
		return
	}
	site, ok := sites.Get(account.Site)
	if !ok || !site.Download {
		writeError(w, http.StatusBadRequest, "该站点暂不支持下载")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的收藏夹")
		return
	}
	var body struct {
		AutoDownload *bool `json:"auto_download"`
	}
	if err := readJSON(r, &body); err != nil || body.AutoDownload == nil {
		writeError(w, http.StatusBadRequest, "需要 auto_download 开关")
		return
	}
	found, err := s.store.SetFolderAutoDownload(r.Context(), account.ID, id, *body.AutoDownload, time.Now().Unix())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存收藏夹设置失败")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "收藏夹不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folder_id": id, "auto_download": *body.AutoDownload})
}

// autoDownloadAfterSync runs after the sync result is persisted. Queueing is
// separate from the sync outcome and always resolves the run's own account.
func (s *Server) autoDownloadAfterSync(ctx context.Context, runID int64) {
	if s.download == nil {
		return
	}
	s.autoDownloadMu.Lock()
	defer s.autoDownloadMu.Unlock()
	if ctx.Err() != nil {
		return
	}
	run, err := s.store.GetRunAll(ctx, runID)
	if err != nil || run.State != "success" {
		return
	}
	account, err := s.store.GetAccountByID(ctx, run.AccountID)
	if err != nil || account.State != "active" {
		return
	}
	site, ok := sites.Get(account.Site)
	if !ok || !site.Download {
		return
	}
	driver, ok := sites.DriverFor(account.Site)
	if !ok {
		return
	}
	_, direct := driver.(sites.DirectDriver)
	builder, buildsTasks := driver.(sites.TaskBuilder)
	if !direct && !buildsTasks {
		return
	}
	settings, err := download.LoadSettings(ctx, s.store)
	if err != nil {
		s.autoDownloadError(runID, 0)
		return
	}
	folders, err := s.store.ListFolders(ctx, account.ID)
	if err != nil {
		s.autoDownloadError(runID, 0)
		return
	}
	var accountContext sites.AccountContext
	if !direct {
		accountContext, err = s.accountContext(ctx, account)
		if err != nil {
			s.autoDownloadError(runID, 0)
			return
		}
	}
	for _, folder := range folders {
		if ctx.Err() != nil {
			return
		}
		if !folder.AutoDownload || folder.State != "active" || !autoDownloadScopeMatches(run.Scope, folder.RemoteID) {
			continue
		}
		items, err := s.store.ListAutoDownloadItems(ctx, account.ID, folder.ID)
		if err != nil {
			s.autoDownloadError(runID, folder.ID)
			continue
		}
		created, skipped := 0, 0
		failed := false
		stop := false
		for _, item := range items {
			if ctx.Err() != nil {
				return
			}
			enabled, preferenceErr := s.store.FolderAutoDownloadForRun(ctx, account.ID, folder.ID, runID)
			if preferenceErr != nil {
				failed = true
				break
			}
			if !enabled {
				break
			}
			batch := []store.Item{item}
			var inputs []store.DownloadTaskInput
			if direct {
				inputs, err = directTaskInputs(account.Site, folder.Title, batch)
			} else {
				inputs, err = builder.BuildTasks(ctx, accountContext, sites.TaskRequest{Items: batch, SourceTitle: folder.Title, Quality: settings.QualityFor(site.ID)})
			}
			if err != nil || len(inputs) == 0 {
				failed = true
				if err != nil && driver.IsAuth(err) {
					if stateErr := s.store.SetAccountState(ctx, account.ID, "needs_reauth", time.Now().Unix()); stateErr != nil {
						slog.Error("mark account needs_reauth from automatic download", "account_id", account.ID)
					}
					stop = true
				} else if err != nil && driver.IsFatal(err) {
					stop = true
				}
				if stop {
					break
				}
				continue
			}
			n, ignored, enqueueErr := s.download.EnqueueAutomatic(ctx, account.ID, folder.ID, runID, settings.QualityFor(site.ID), inputs)
			created += n
			skipped += ignored
			if enqueueErr != nil {
				failed = true
				break
			}
		}
		if failed {
			s.autoDownloadError(runID, folder.ID)
		}
		if created > 0 || skipped > 0 {
			s.hub.Publish(map[string]any{"type": "download_enqueued", "created": created, "skipped": skipped, "source_title": folder.Title, "automatic": true, "run_id": runID})
		}
		if stop {
			return
		}
	}
}

func autoDownloadScopeMatches(scope, remoteID string) bool {
	return scope == "all" || scope == "folder:"+remoteID
}

func (s *Server) autoDownloadError(runID, folderID int64) {
	slog.Warn("automatic download enqueue failed", "run_id", runID, "folder_id", folderID)
	s.hub.Publish(map[string]any{"type": "download_enqueued", "error": "同步后自动创建下载任务失败，请手动重试或等待下次同步", "automatic": true, "run_id": runID})
}
