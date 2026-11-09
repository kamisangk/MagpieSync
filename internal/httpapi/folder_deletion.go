package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"magpiesync/internal/download"
	"magpiesync/internal/store"
)

func (s *Server) handleFolderDelete(w http.ResponseWriter, r *http.Request) {
	account, ok := s.requireAccount(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的来源")
		return
	}
	var body struct {
		DeleteLocalFiles *bool `json:"delete_local_files"`
	}
	if err := readJSON(r, &body); err != nil || body.DeleteLocalFiles == nil {
		writeError(w, http.StatusBadRequest, "需要 delete_local_files 选项")
		return
	}
	var result store.FolderDeletionResult
	if s.download != nil {
		result, err = s.download.DeleteArchivedFolder(r.Context(), account.ID, id, *body.DeleteLocalFiles, time.Now().Unix())
	} else {
		result, err = download.DeleteArchivedFolder(r.Context(), s.store, account.ID, id, *body.DeleteLocalFiles, time.Now().Unix())
	}
	if errors.Is(err, store.ErrFolderNotFound) {
		writeError(w, http.StatusNotFound, "来源不存在")
		return
	}
	if errors.Is(err, store.ErrFolderDeletionConflict) {
		writeError(w, http.StatusConflict, "仅可删除已归档且未在同步中的来源")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除来源未能完成，请刷新并检查下载文件状态")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
