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
	Missing  int    `json:"missing"`
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
	rows, err := s.db.QueryContext(ctx, `WITH local AS (`+localStatusSQL+`)
        SELECT remote_id, state, done, total, missing, file_path FROM local
        WHERE account_id = ? AND remote_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var remoteID string
		var status LocalStatus
		if err := rows.Scan(&remoteID, &status.State, &status.Done, &status.Total, &status.Missing, &status.FilePath); err != nil {
			return nil, err
		}
		out[remoteID] = status
	}
	return out, rows.Err()
}

// FolderLocalCounts returns downloaded video counts for every local library.
func (s *Store) FolderLocalCounts(ctx context.Context, accountID int64) (map[int64]FolderLocalCount, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH local AS (`+localStatusSQL+`)
		SELECT f.id,
		       COUNT(DISTINCT i.remote_id),
		       COUNT(DISTINCT CASE WHEN p.state = 'done' THEN i.remote_id END)
		FROM folders f
		JOIN folder_items fi ON fi.folder_id = f.id
		JOIN items i ON i.id = fi.item_id AND i.remote_id <> ''
		LEFT JOIN local p ON p.remote_id = i.remote_id AND p.account_id = i.account_id
		WHERE f.account_id = ? AND `+visibleFolderItemSQL+`
		GROUP BY f.id`, accountID)
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
	err := s.db.QueryRowContext(ctx, `WITH local AS (`+localStatusSQL+`)
        SELECT COUNT(*) FROM local WHERE account_id = ? AND state = 'done'`, accountID).Scan(&n)
	return n, err
}
