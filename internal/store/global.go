package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Global overview queries intentionally do not depend on the currently active
// account. Content pages remain account-scoped; only the dashboard and sync
// history are global.

func (s *Store) CountItemsAll(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items`).Scan(&n)
	return n, err
}

func (s *Store) CountFoldersAll(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM folders WHERE state = 'active'`).Scan(&n)
	return n, err
}

func (s *Store) CountDownloadedVideosAll(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT account_id, remote_id
			FROM download_tasks
			WHERE remote_id <> ''
			GROUP BY account_id, remote_id
			HAVING SUM(CASE WHEN state = 'done' THEN 1 ELSE 0 END) = COUNT(*)
		)`).Scan(&n)
	return n, err
}

func (s *Store) ListRunsAll(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, account_id, trigger, scope, state, started_at, finished_at, folders_total, folders_done,
			items_seen, items_added, items_updated, items_missing, items_removed, error_code, error_message
		FROM sync_runs ORDER BY started_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		if err := scanRun(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) LastRunAll(ctx context.Context) (*Run, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, account_id, trigger, scope, state, started_at, finished_at, folders_total, folders_done,
			items_seen, items_added, items_updated, items_missing, items_removed, error_code, error_message
		FROM sync_runs ORDER BY started_at DESC, id DESC LIMIT 1`)
	var r Run
	if err := scanRun(row, &r); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func (s *Store) GetRunAll(ctx context.Context, id int64) (*Run, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, account_id, trigger, scope, state, started_at, finished_at, folders_total, folders_done,
			items_seen, items_added, items_updated, items_missing, items_removed, error_code, error_message
		FROM sync_runs WHERE id = ?`, id)
	var r Run
	if err := scanRun(row, &r); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("run %d not found", id)
		}
		return nil, err
	}
	return &r, nil
}

// ---------- global export ----------

func (s *Store) AllFoldersAll(ctx context.Context) ([]Folder, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.account_id, f.remote_id, f.title, f.kind, f.owner_id, f.privacy, f.remote_count, f.state, f.last_full_sync_at,
		       (SELECT COUNT(*) FROM folder_items fi WHERE fi.folder_id = f.id AND fi.state <> 'removed') AS item_count
		FROM folders f
		ORDER BY f.account_id, CASE f.state WHEN 'active' THEN 0 ELSE 1 END, f.kind, f.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.AccountID, &f.RemoteID, &f.Title, &f.Kind, &f.OwnerID, &f.Privacy, &f.RemoteCount, &f.State, &f.LastFullSyncAt, &f.ItemCount); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) AllItemsAll(ctx context.Context) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id, i.account_id, i.remote_id, i.kind, i.title, i.url, i.cover_url,
		i.duration, i.author_id, i.author_name, i.summary, i.published_at, i.attr, i.state, i.first_seen_at, i.last_seen_at, i.updated_at
		FROM items i ORDER BY i.account_id, i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := scanItem(rows, &it); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) AllFolderItemsAll(ctx context.Context) ([]ExportFolderItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fi.folder_id, fi.item_id, fi.favorited_at, fi.state
		FROM folder_items fi JOIN folders f ON f.id = fi.folder_id
		ORDER BY fi.folder_id, fi.item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExportFolderItem
	for rows.Next() {
		var e ExportFolderItem
		if err := rows.Scan(&e.FolderID, &e.ItemID, &e.FavoritedAt, &e.State); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
