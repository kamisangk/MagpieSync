package store

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) FolderAutoDownloadForRun(ctx context.Context, accountID, folderID, runID int64) (bool, error) {
	var enabled bool
	err := s.db.QueryRowContext(ctx, `SELECT fp.auto_download FROM folder_preferences fp
		JOIN folders f ON f.id = fp.folder_id WHERE f.id = ? AND f.account_id = ? AND f.state = 'active'
		AND ? > fp.after_run_id`, folderID, accountID, runID).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return enabled, err
}

// ListAutoDownloadItems includes active videos with unfinished or unknown
// pages, but leaves completed and intentionally paused/canceled work alone.
func (s *Store) ListAutoDownloadItems(ctx context.Context, accountID, folderID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.account_id, i.remote_id, i.kind, i.title, i.url, i.cover_url,
			i.duration, i.author_id, i.author_name, i.summary, i.published_at, i.attr, i.state,
			i.first_seen_at, i.last_seen_at, i.updated_at
		FROM items i JOIN folder_items fi ON fi.item_id = i.id
		WHERE i.account_id = ? AND fi.folder_id = ? AND fi.state = 'active' AND i.state = 'active'
		AND (
			NOT EXISTS (SELECT 1 FROM download_tasks d WHERE d.account_id = i.account_id AND d.remote_id = i.remote_id)
			OR EXISTS (SELECT 1 FROM download_tasks d WHERE d.account_id = i.account_id AND d.remote_id = i.remote_id AND d.state IN ('failed', 'missing'))
			OR (SELECT COUNT(DISTINCT d.part_id) FROM download_tasks d WHERE d.account_id = i.account_id AND d.remote_id = i.remote_id)
			   < (SELECT MAX(d.page_count) FROM download_tasks d WHERE d.account_id = i.account_id AND d.remote_id = i.remote_id)
		)
		ORDER BY i.id`, accountID, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var item Item
		if err := scanItem(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
