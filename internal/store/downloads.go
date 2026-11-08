package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type DownloadTask struct {
	ID            int64
	AccountID     int64
	Site          string
	PageURL       string
	RemoteID      string
	PartID        int64
	VideoTitle    string
	PageTitle     string
	PageNo        int
	PageCount     int
	Duration      int
	CoverURL      string
	AuthorID      string
	AuthorName    string
	PubTime       int64
	FavTime       int64
	SourceTitle   string
	Tags          []string
	Summary       string
	Quality       int
	State         string
	Progress      float64
	TotalBytes    int64
	Downloaded    int64
	ActualQuality int
	Codec         string
	FilePath      string
	Error         string
	Attempts      int
	CreatedAt     int64
	UpdatedAt     int64
	FinishedAt    int64
}

func (d DownloadTask) PageTitleOrVideo() string {
	if d.PageTitle != "" {
		return d.PageTitle
	}
	return d.VideoTitle
}

type DownloadTaskInput struct {
	Site        string
	PageURL     string
	RemoteID    string
	PartID      int64
	VideoTitle  string
	PageTitle   string
	PageNo      int
	PageCount   int
	Duration    int
	CoverURL    string
	AuthorID    string
	AuthorName  string
	PubTime     int64
	FavTime     int64
	SourceTitle string
	Tags        []string
	Summary     string
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	return err
}

func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) SetSettings(ctx context.Context, values map[string]string, now int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for k, v := range values {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings(key, value, updated_at) VALUES(?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			k, v, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CreateDownloadTasks(ctx context.Context, accountID int64, quality int, inputs []DownloadTaskInput, now int64) (created, skipped int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, in := range inputs {
		// A terminal unsuccessful attempt can be intentionally downloaded again
		// with a new quality. Reuse its row so local status stays tied to the
		// same video/page, but replace the stale quality and queue state.
		var existingID int64
		var existingState string
		err := tx.QueryRowContext(ctx, `
			SELECT id, state FROM download_tasks
			WHERE account_id = ? AND remote_id = ? AND part_id = ?
			ORDER BY id DESC LIMIT 1`, accountID, in.RemoteID, in.PartID).Scan(&existingID, &existingState)
		if err == nil && (existingState == "failed" || existingState == "canceled" || existingState == "missing") {
			res, err := tx.ExecContext(ctx, `
				UPDATE download_tasks SET
					site = ?, page_url = ?, video_title = ?, page_title = ?, page_no = ?, page_count = ?, duration = ?,
					cover_url = ?, author_id = ?, author_name = ?, pub_time = ?, fav_time = ?, source_title = ?, tags = ?, summary = ?,
					quality = ?, state = 'pending', progress = 0, total_bytes = 0, downloaded = 0, actual_quality = 0, codec = '',
					file_path = '', error = '', attempts = 0, finished_at = NULL, updated_at = ?
				WHERE id = ?`,
				in.Site, in.PageURL, in.VideoTitle, in.PageTitle, in.PageNo, in.PageCount, in.Duration,
				in.CoverURL, in.AuthorID, in.AuthorName, in.PubTime, in.FavTime, in.SourceTitle,
				strings.Join(in.Tags, "|"), in.Summary, quality, now, existingID)
			if err != nil {
				return created, skipped, err
			}
			if n, err := res.RowsAffected(); err != nil {
				return created, skipped, err
			} else if n > 0 {
				created++
				continue
			}
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO download_tasks(
				account_id, site, page_url, remote_id, part_id, video_title, page_title, page_no, page_count, duration,
				cover_url, author_id, author_name, pub_time, fav_time, source_title, tags, summary, quality,
				state, created_at, updated_at
			)
			SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?, 'pending', ?, ?
			WHERE NOT EXISTS (
				SELECT 1 FROM download_tasks
				WHERE account_id = ? AND remote_id = ? AND part_id = ?
			)`,
			accountID, in.Site, in.PageURL, in.RemoteID, in.PartID, in.VideoTitle, in.PageTitle, in.PageNo, in.PageCount, in.Duration,
			in.CoverURL, in.AuthorID, in.AuthorName, in.PubTime, in.FavTime, in.SourceTitle,
			strings.Join(in.Tags, "|"), in.Summary, quality, now, now,
			accountID, in.RemoteID, in.PartID)
		if err != nil {
			return created, skipped, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return created, skipped, err
		}
		if n > 0 {
			created++
		} else {
			skipped++
		}
	}
	return created, skipped, tx.Commit()
}

const downloadColumns = `id, account_id, site, page_url, remote_id, part_id, video_title, page_title, page_no, page_count,
	duration, cover_url, author_id, author_name, pub_time, fav_time, source_title, tags, summary, quality,
	state, progress, total_bytes, downloaded, actual_quality, codec, file_path, error,
	attempts, created_at, updated_at, COALESCE(finished_at, 0)`

func scanDownload(row interface{ Scan(...any) error }) (*DownloadTask, error) {
	var d DownloadTask
	var tags string
	err := row.Scan(&d.ID, &d.AccountID, &d.Site, &d.PageURL, &d.RemoteID, &d.PartID, &d.VideoTitle, &d.PageTitle,
		&d.PageNo, &d.PageCount, &d.Duration, &d.CoverURL, &d.AuthorID, &d.AuthorName,
		&d.PubTime, &d.FavTime, &d.SourceTitle, &tags, &d.Summary, &d.Quality, &d.State, &d.Progress,
		&d.TotalBytes, &d.Downloaded, &d.ActualQuality, &d.Codec, &d.FilePath, &d.Error,
		&d.Attempts, &d.CreatedAt, &d.UpdatedAt, &d.FinishedAt)
	if err != nil {
		return nil, err
	}
	if tags != "" {
		d.Tags = strings.Split(tags, "|")
	}
	return &d, nil
}

func (s *Store) ListDownloadTasks(ctx context.Context, accountID int64, state string, limit int) ([]DownloadTask, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT ` + downloadColumns + ` FROM download_tasks WHERE account_id = ?`
	args := []any{accountID}
	if state != "" {
		query += ` AND state = ?`
		args = append(args, state)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DownloadTask, 0, limit)
	for rows.Next() {
		d, err := scanDownload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (s *Store) GetDownloadTask(ctx context.Context, id int64) (*DownloadTask, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+downloadColumns+` FROM download_tasks WHERE id = ?`, id)
	return scanDownload(row)
}

func (s *Store) ClaimDownloadTask(ctx context.Context, accountID int64, now int64) (*DownloadTask, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE download_tasks
		SET state = 'running', attempts = attempts + 1, error = '', updated_at = ?
		WHERE id = (
			SELECT id FROM download_tasks
			WHERE account_id = ? AND state = 'pending'
			ORDER BY id LIMIT 1
		)
		RETURNING `+downloadColumns, now, accountID)
	d, err := scanDownload(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

// ClaimAnyDownloadTask claims the next pending task across all accounts. The
// download manager resolves the account from the task itself.
func (s *Store) ClaimAnyDownloadTask(ctx context.Context, now int64) (*DownloadTask, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE download_tasks
		SET state = 'running', attempts = attempts + 1, error = '', updated_at = ?
		WHERE id = (
			SELECT id FROM download_tasks
			WHERE state = 'pending'
			ORDER BY id LIMIT 1
		)
		RETURNING `+downloadColumns, now)
	d, err := scanDownload(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func (s *Store) UpdateDownloadProgress(ctx context.Context, id int64, progress float64, downloaded, total int64, now int64) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_tasks SET progress = ?, downloaded = ?, total_bytes = ?, updated_at = ?
		WHERE id = ?`, progress, downloaded, total, now, id)
	return err
}

func (s *Store) FinishDownloadTask(ctx context.Context, id int64, state, filePath, errMsg string, actualQuality int, codec string, size int64, now int64) error {
	var finished any
	if state == "done" || state == "failed" {
		finished = now
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_tasks
		SET state = ?, file_path = ?, error = ?, actual_quality = ?, codec = ?,
		    progress = CASE WHEN ? = 'done' THEN 1 ELSE progress END,
		    downloaded = CASE WHEN ? > 0 THEN ? ELSE downloaded END,
		    total_bytes = CASE WHEN ? > 0 THEN ? ELSE total_bytes END,
		    finished_at = ?, updated_at = ?
		WHERE id = ?`,
		state, filePath, errMsg, actualQuality, codec, state, size, size, size, size, finished, now, id)
	return err
}

func (s *Store) ResetDownloadTask(ctx context.Context, id, now int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_tasks
		SET state = 'pending', progress = 0, downloaded = 0, total_bytes = 0, error = '',
		    attempts = 0, finished_at = NULL, updated_at = ?
		WHERE id = ? AND state IN ('failed', 'done', 'canceled')`, now, id)
	return err
}

// MarkDownloadTaskMissing moves a finished download task to the missing state
// because its output file no longer exists on disk. It is a state transition
// only; the row and its metadata are preserved. No-op when the task is not
// currently done.
func (s *Store) MarkDownloadTaskMissing(ctx context.Context, id int64, errMsg string, now int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_tasks
		SET state = 'missing', error = ?, finished_at = NULL, updated_at = ?
		WHERE id = ? AND state = 'done'`, errMsg, now, id)
	return err
}

// ReappearDownloadTask restores a missing task back to done once its output
// file is present again, clearing the error. No-op when the task is not
// currently missing.
func (s *Store) ReappearDownloadTask(ctx context.Context, id int64, now int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_tasks
		SET state = 'done', progress = 1, error = '', finished_at = ?, updated_at = ?
		WHERE id = ? AND state = 'missing'`, now, now, id)
	return err
}

// RequeueDownloadTask returns a failed attempt to the queue for automatic retry.
func (s *Store) RequeueDownloadTask(ctx context.Context, id int64, errMsg string, now int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_tasks
		SET state = 'pending', progress = 0, downloaded = 0, total_bytes = 0,
		    error = ?, finished_at = NULL, updated_at = ?
		WHERE id = ? AND state = 'running'`, errMsg, now, id)
	return err
}

// PauseDownloadTask moves a running task to paused. The manager cancels its
// per-task context separately; partial files are kept for a later resume.
func (s *Store) PauseDownloadTask(ctx context.Context, id, now int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_tasks SET state = 'paused', error = '已暂停', updated_at = ?
		WHERE id = ? AND state IN ('pending', 'running')`, now, id)
	return err
}

// ResumeDownloadTask puts a paused task back in the pending queue.
func (s *Store) ResumeDownloadTask(ctx context.Context, id, now int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_tasks
		SET state = 'pending', error = '', attempts = 0, updated_at = ?
		WHERE id = ? AND state = 'paused'`, now, id)
	return err
}

// CancelDownloadTask marks a task canceled and keeps it out of the queue.
func (s *Store) CancelDownloadTask(ctx context.Context, id, now int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE download_tasks SET state = 'canceled', error = '已取消', updated_at = ?
		WHERE id = ? AND state IN ('pending', 'running', 'paused')`, now, id)
	return err
}

// ResetRunningDownloadTasks recovers tasks left "running" by a crash/restart.
func (s *Store) ResetRunningDownloadTasks(ctx context.Context, now int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE download_tasks
		SET state = 'pending', progress = 0, downloaded = 0, total_bytes = 0,
		    error = '进程重启后恢复', updated_at = ?
		WHERE state = 'running'`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) DownloadCounts(ctx context.Context, accountID int64) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT state, COUNT(*) FROM download_tasks WHERE account_id = ? GROUP BY state`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{"pending": 0, "running": 0, "done": 0, "missing": 0, "failed": 0, "paused": 0, "canceled": 0}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[state] = n
	}
	return out, rows.Err()
}

func UnixNow() int64 { return time.Now().Unix() }

func (s *Store) ListFolderRemoteIDs(ctx context.Context, accountID, folderID int64) ([]string, error) {
	query := `SELECT DISTINCT i.remote_id FROM items i WHERE i.account_id = ? AND i.remote_id <> ''`
	args := []any{accountID}
	if folderID > 0 {
		query += ` AND EXISTS (SELECT 1 FROM folder_items fi WHERE fi.folder_id = ? AND fi.item_id = i.id)`
		args = append(args, folderID)
	}
	query += ` ORDER BY i.id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var remoteID string
		if err := rows.Scan(&remoteID); err != nil {
			return nil, err
		}
		out = append(out, remoteID)
	}
	return out, rows.Err()
}
