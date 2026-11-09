package httpapi

import (
	"net/http"
	"strconv"

	"magpiesync/internal/store"
)

func validItemFilters(f store.ItemFilter) bool {
	if f.State != "" && f.RemoteState != "" {
		return false
	}
	switch f.RemoteState {
	case "", "active", "missing", "removed":
	default:
		return false
	}
	switch f.LocalState {
	case "", "none", "pending", "running", "paused", "done", "partial", "failed", "missing":
	default:
		return false
	}
	return true
}

func (s *Server) itemFolderContext(w http.ResponseWriter, r *http.Request, accountID int64) (int64, bool) {
	raw := r.URL.Query().Get("folder_id")
	if raw == "" {
		return 0, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid folder id")
		return 0, false
	}
	found, err := s.store.HasFolder(r.Context(), accountID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return 0, false
	}
	if !found {
		writeError(w, http.StatusNotFound, "folder not found")
		return 0, false
	}
	return id, true
}
