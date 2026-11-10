package store

import (
	"context"
	"database/sql"
	"errors"
)

var ErrAccountDeletionConflict = errors.New("account is syncing or downloading")

type AccountDeletionResult struct {
	DeletedFiles    int      `json:"deleted_files"`
	ProtectedVideos int      `json:"protected_videos"`
	SkippedFiles    int      `json:"skipped_files"`
	FailedFiles     int      `json:"failed_files"`
	PartialCleanup  bool     `json:"partial_cleanup"`
	Reasons         []string `json:"reasons,omitempty"`
}

// DeleteAccountWithCleanup reserves the writer before checking references. The
// callback may only use its snapshot and the filesystem, never Store methods.
func (s *Store) DeleteAccountWithCleanup(ctx context.Context, accountID int64, cleanup func(FolderCleanupSnapshot) FolderDeletionResult) (AccountDeletionResult, error) {
	result := AccountDeletionResult{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE accounts SET updated_at=updated_at WHERE id=?`, accountID)
	if err != nil {
		return result, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return result, err
	}
	if n == 0 {
		return result, ErrNoAccount
	}
	var busy bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sync_runs WHERE account_id=? AND state='running') OR EXISTS(SELECT 1 FROM download_tasks WHERE account_id=? AND state='running')`, accountID, accountID).Scan(&busy); err != nil {
		return result, err
	}
	if busy {
		return result, ErrAccountDeletionConflict
	}
	if cleanup != nil {
		snapshot := FolderCleanupSnapshot{MediaDir: "./media", DeletingAccountID: accountID}
		var dir string
		err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='download.media_dir'`).Scan(&dir)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
		if dir != "" {
			snapshot.MediaDir = dir
		}
		rows, err := tx.QueryContext(ctx, `SELECT d.id,d.account_id,COALESCE(NULLIF(d.site,''),a.site),d.remote_id,d.state,d.file_path,d.account_id=?,
   (a.site='' OR d.remote_id='' OR (d.site<>'' AND d.site<>a.site)
    OR EXISTS(SELECT 1 FROM items i JOIN accounts x ON x.id=i.account_id WHERE i.account_id<>d.account_id AND i.remote_id=d.remote_id AND (x.site=a.site OR x.site=''))
    OR EXISTS(SELECT 1 FROM download_tasks x JOIN accounts xa ON xa.id=x.account_id WHERE x.account_id<>d.account_id AND x.remote_id=d.remote_id AND (xa.site=a.site OR xa.site='' OR x.site=a.site OR (x.site<>'' AND x.site<>xa.site))))
   FROM download_tasks d JOIN accounts a ON a.id=d.account_id ORDER BY d.id`, accountID)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var task FolderCleanupTask
			if err = rows.Scan(&task.ID, &task.AccountID, &task.Site, &task.RemoteID, &task.State, &task.FilePath, &task.Candidate, &task.Protected); err != nil {
				rows.Close()
				return result, err
			}
			snapshot.RunningDownload = snapshot.RunningDownload || task.State == "running"
			snapshot.Tasks = append(snapshot.Tasks, task)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
		cleaned := cleanup(snapshot)
		result = AccountDeletionResult{DeletedFiles: cleaned.DeletedFiles, ProtectedVideos: cleaned.ProtectedVideos, SkippedFiles: cleaned.SkippedFiles, FailedFiles: cleaned.FailedFiles, PartialCleanup: cleaned.SkippedFiles > 0 || cleaned.FailedFiles > 0, Reasons: cleaned.Reasons}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM accounts WHERE id=?`, accountID); err != nil {
		return result, err
	}
	return result, tx.Commit()
}
