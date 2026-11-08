package httpapi

import (
	"net/http"
	"time"

	"github.com/robfig/cron/v3"

	"magpiesync/internal/scheduler"
)

func (s *Server) SetScheduleUpdater(update func(cron.Schedule)) { s.scheduleUpdater = update }

func (s *Server) handleSyncPreview(w http.ResponseWriter, r *http.Request) {
	plan, err := scheduler.Parse(r.URL.Query().Get("expression"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	nextRuns := make([]time.Time, 0, 5)
	if plan != nil {
		next := time.Now()
		for range 5 {
			next = plan.Next(next)
			if next.IsZero() {
				break
			}
			nextRuns = append(nextRuns, next)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"next_runs": nextRuns})
}
