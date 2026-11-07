package httpapi

import (
	"github.com/robfig/cron/v3"

	"magpiesync/internal/scheduler"
	"net/http"
	"time"
)

func (s *Server) SetScheduleUpdater(update func(cron.Schedule)) { s.scheduleUpdater = update }

func (s *Server) handleSyncPreview(w http.ResponseWriter, r *http.Request) {
	plan, err := scheduler.Parse(r.URL.Query().Get("expression"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var next *time.Time
	if plan != nil {
		t := plan.Next(time.Now())
		next = &t
	}
	writeJSON(w, http.StatusOK, map[string]any{"next_run": next})
}
