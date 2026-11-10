package store

import (
	"context"
	"database/sql"
	"errors"
)

var ErrFolderNotFound = errors.New("folder not found")
var ErrFolderDeletionConflict = errors.New("folder is active or account is syncing")

type FolderCleanupTask struct {
	ID        int64
	AccountID int64
	Site      string
	RemoteID  string
	State     string
	FilePath  string
	Candidate bool
	Protected bool
}

type FolderCleanupSnapshot struct {
	Tasks             []FolderCleanupTask
	RunningDownload   bool
	MediaDir          string
	DeletingAccountID int64
}

type FolderDeletionResult struct {
	FolderID        int64    `json:"folder_id"`
	DeletedFiles    int      `json:"deleted_files"`
	ProtectedVideos int      `json:"protected_videos"`
	SkippedFiles    int      `json:"skipped_files"`
	FailedFiles     int      `json:"failed_files"`
	PartialCleanup  bool     `json:"partial_cleanup"`
	Reasons         []string `json:"reasons,omitempty"`
	MissingTaskIDs  []int64  `json:"-"`
}

// DeleteArchivedFolder reserves the single SQLite writer before checking references.
// The cleanup callback must use only its snapshot and local filesystem operations:
// calling Store methods here would deadlock on the single database connection.
func (s *Store) DeleteArchivedFolder(ctx context.Context, accountID, folderID int64, cleanup func(FolderCleanupSnapshot) FolderDeletionResult, now int64) (FolderDeletionResult, error) {
	result := FolderDeletionResult{FolderID: folderID}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE folders SET updated_at = updated_at WHERE id = ? AND account_id = ?`, folderID, accountID); err != nil {
		return result, err
	}
	var state string
	err = tx.QueryRowContext(ctx, `SELECT state FROM folders WHERE id=? AND account_id=?`, folderID, accountID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrFolderNotFound
	}
	if err != nil {
		return result, err
	}
	var syncing bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sync_runs WHERE account_id=? AND state='running')`, accountID).Scan(&syncing); err != nil {
		return result, err
	}
	if state != "archived" || syncing {
		return result, ErrFolderDeletionConflict
	}
	if cleanup != nil {
		snapshot := FolderCleanupSnapshot{MediaDir: "./media"}
		var dir string
		err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='download.media_dir'`).Scan(&dir)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
		if dir != "" {
			snapshot.MediaDir = dir
		}
		rows, err := tx.QueryContext(ctx, `SELECT d.id,d.account_id,COALESCE(NULLIF(d.site,''),a.site),d.remote_id,d.state,d.file_path,
   (d.account_id=? AND EXISTS(SELECT 1 FROM items i JOIN folder_items fi ON fi.item_id=i.id WHERE i.account_id=d.account_id AND i.remote_id=d.remote_id AND fi.folder_id=?)),
   (a.site='' OR (d.site<>'' AND d.site<>a.site)
    OR EXISTS(SELECT 1 FROM items i JOIN folder_items fi ON fi.item_id=i.id WHERE i.account_id=d.account_id AND i.remote_id=d.remote_id AND fi.folder_id<>?)
    OR EXISTS(SELECT 1 FROM items i JOIN accounts x ON x.id=i.account_id WHERE x.site=a.site AND i.remote_id=d.remote_id AND i.account_id<>d.account_id)
    OR EXISTS(SELECT 1 FROM download_tasks x JOIN accounts xa ON xa.id=x.account_id WHERE xa.site=a.site AND x.remote_id=d.remote_id AND x.account_id<>d.account_id)
    OR EXISTS(SELECT 1 FROM download_tasks x WHERE x.account_id=d.account_id AND x.remote_id=d.remote_id AND x.state IN ('pending','running','paused')))
   FROM download_tasks d JOIN accounts a ON a.id=d.account_id`, accountID, folderID, folderID)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var task FolderCleanupTask
			if err = rows.Scan(&task.ID, &task.AccountID, &task.Site, &task.RemoteID, &task.State, &task.FilePath, &task.Candidate, &task.Protected); err != nil {
				rows.Close()
				return result, err
			}
			if task.State == "running" {
				snapshot.RunningDownload = true
			}
			snapshot.Tasks = append(snapshot.Tasks, task)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
		result = cleanup(snapshot)
		result.FolderID = folderID
		for _, id := range result.MissingTaskIDs {
			if _, err = tx.ExecContext(ctx, `UPDATE download_tasks SET state='missing', error='本地文件已删除或不存在', finished_at=NULL, updated_at=? WHERE id=? AND account_id=? AND state='done'`, now, id, accountID); err != nil {
				return result, err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM folders WHERE id=? AND account_id=? AND state='archived'`, folderID, accountID); err != nil {
		return result, err
	}
	result.PartialCleanup = result.SkippedFiles > 0 || result.FailedFiles > 0
	return result, tx.Commit()
}
