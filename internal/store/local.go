package store

import (
	"context"
	"strings"
)

// LocalStatus summarizes whether a video has media downloaded on this machine.
// A video may have multiple pages, so Total and Done count download tasks.
type LocalStatus struct {
	State    string `json:"state"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	FilePath string `json:"file_path,omitempty"`
}

// FolderLocalCount summarizes downloaded videos inside one local library.
type FolderLocalCount struct {
	Total int
	Done  int
}

func (s *Store) LocalStatuses(ctx context.Context, accountID int64, remoteIDs []string) (map[string]LocalStatus, error) {
	out := make(map[string]LocalStatus, len(remoteIDs))
	ids := uniqueNonEmpty(remoteIDs)
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, accountID)
	for i, remote_id := range ids {
		placeholders[i] = "?"
		args = append(args, remote_id)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT remote_id,
		       COUNT(*),
		       SUM(CASE WHEN state = 'done' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN state = 'running' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN state = 'pending' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN state = 'paused' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN state = 'failed' THEN 1 ELSE 0 END),
		       MAX(CASE WHEN state = 'done' AND file_path <> '' THEN file_path ELSE '' END)
		FROM download_tasks
		WHERE account_id = ? AND remote_id IN (`+strings.Join(placeholders, ",")+`)
		GROUP BY remote_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			remote_id                string
			total, done              int
			running, pending, paused int
			failed                   int
			filePath                 string
		)
		if err := rows.Scan(&remote_id, &total, &done, &running, &pending, &paused, &failed, &filePath); err != nil {
			return nil, err
		}
		status := LocalStatus{Total: total, Done: done, FilePath: filePath}
		switch {
		case total > 0 && done == total:
			status.State = "done"
		case running > 0:
			status.State = "running"
		case pending > 0:
			status.State = "pending"
		case paused > 0:
			status.State = "paused"
		case done > 0:
			status.State = "partial"
		case failed > 0:
			status.State = "failed"
		default:
			status.State = "none"
		}
		out[remote_id] = status
	}
	return out, rows.Err()
}

// FolderLocalCounts returns downloaded video counts for every local library.
func (s *Store) FolderLocalCounts(ctx context.Context, accountID int64) (map[int64]FolderLocalCount, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH per_item AS (
			SELECT remote_id,
			       COUNT(*) AS total,
			       SUM(CASE WHEN state = 'done' THEN 1 ELSE 0 END) AS done
			FROM download_tasks
			WHERE account_id = ? AND remote_id <> ''
			GROUP BY remote_id
		)
		SELECT f.id,
		       COUNT(DISTINCT i.remote_id),
		       COUNT(DISTINCT CASE WHEN p.done = p.total AND p.total > 0 THEN i.remote_id END)
		FROM folders f
		JOIN folder_items fi ON fi.folder_id = f.id AND fi.state <> 'removed'
		JOIN items i ON i.id = fi.item_id AND i.remote_id <> ''
		LEFT JOIN per_item p ON p.remote_id = i.remote_id
		WHERE f.account_id = ?
		GROUP BY f.id`, accountID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]FolderLocalCount)
	for rows.Next() {
		var id int64
		var c FolderLocalCount
		if err := rows.Scan(&id, &c.Total, &c.Done); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

// DownloadedRemoteIDs returns videos that already have at least one completed page.
func (s *Store) DownloadedRemoteIDs(ctx context.Context, accountID int64, remoteIDs []string) (map[string]bool, error) {
	ids := uniqueNonEmpty(remoteIDs)
	out := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, accountID)
	for i, remote_id := range ids {
		placeholders[i] = "?"
		args = append(args, remote_id)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT remote_id FROM download_tasks
		WHERE account_id = ? AND state = 'done' AND remote_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var remote_id string
		if err := rows.Scan(&remote_id); err != nil {
			return nil, err
		}
		out[remote_id] = true
	}
	return out, rows.Err()
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func (s *Store) CountDownloadedVideos(ctx context.Context, accountID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT remote_id
			FROM download_tasks
			WHERE account_id = ? AND remote_id <> ''
			GROUP BY remote_id
			HAVING SUM(CASE WHEN state = 'done' THEN 1 ELSE 0 END) = COUNT(*)
		)`, accountID).Scan(&n)
	return n, err
}
