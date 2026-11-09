package store

import "context"

// Library visibility uses persisted download evidence from this account.
const retainedItemSQL = `EXISTS (SELECT 1 FROM download_tasks d
 WHERE d.account_id = i.account_id AND d.remote_id = i.remote_id
 AND (d.state IN ('done','missing','pending','running','paused') OR d.file_path <> ''))`

const visibleFolderItemSQL = `(fi.state <> 'removed' OR ` + retainedItemSQL + `)`
const visibleItemSQL = `(NOT EXISTS (SELECT 1 FROM folder_items v WHERE v.item_id = i.id)
 OR EXISTS (SELECT 1 FROM folder_items v WHERE v.item_id = i.id AND v.state <> 'removed')
 OR ` + retainedItemSQL + `)`

// Badge and pre-pagination filter classifications share one projection.
const localStatusSQL = `SELECT account_id, remote_id, total, done, missing, file_path,
 CASE WHEN done = total THEN 'done'
 WHEN running > 0 THEN 'running' WHEN pending > 0 THEN 'pending'
 WHEN paused > 0 THEN 'paused' WHEN done > 0 THEN 'partial'
 WHEN missing > 0 THEN 'missing' WHEN failed > 0 THEN 'failed' ELSE 'none' END AS state
 FROM (SELECT account_id, remote_id, COUNT(*) + MAX(0, MAX(page_count) - COUNT(DISTINCT part_id)) AS total,
 SUM(CASE WHEN state = 'done' THEN 1 ELSE 0 END) AS done,
 SUM(CASE WHEN state = 'missing' THEN 1 ELSE 0 END) AS missing,
 SUM(CASE WHEN state = 'running' THEN 1 ELSE 0 END) AS running,
 SUM(CASE WHEN state = 'pending' THEN 1 ELSE 0 END) AS pending,
 SUM(CASE WHEN state = 'paused' THEN 1 ELSE 0 END) AS paused,
 SUM(CASE WHEN state = 'failed' THEN 1 ELSE 0 END) AS failed,
 MAX(CASE WHEN state = 'done' AND file_path <> '' THEN file_path ELSE '' END) AS file_path
 FROM download_tasks WHERE remote_id <> '' GROUP BY account_id, remote_id)`

func (s *Store) HasFolder(ctx context.Context, accountID, folderID int64) (bool, error) {
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM folders WHERE account_id = ? AND id = ?)`, accountID, folderID).Scan(&found)
	return found, err
}
