package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

var ErrNoAccount = errors.New("no account configured")

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	if err := execScript(db, schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func execScript(db *sql.DB, script string) error {
	for _, stmt := range strings.Split(script, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// ---------- accounts ----------

type Account struct {
	ID           int64  `json:"id"`
	Site         string `json:"site"`
	RemoteUID    string `json:"remote_uid"`
	Name         string `json:"name"`
	AvatarURL    string `json:"avatar_url"`
	State        string `json:"state"`
	Credential   []byte `json:"-"`
	ProxyEnc     []byte `json:"-"`
	CredentialAt int64  `json:"-"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

func (s *Store) GetAccount(ctx context.Context) (*Account, error) {
	return s.getAccount(ctx, "ORDER BY CASE WHEN state = 'active' THEN 0 ELSE 1 END, id DESC LIMIT 1")
}

func (s *Store) GetAccountByID(ctx context.Context, id int64) (*Account, error) {
	return s.getAccount(ctx, "AND id = ? LIMIT 1", id)
}

// ListAccounts returns connected accounts, newest first. It includes active
// accounts and accounts whose login expired but whose credential is retained,
// so the UI can offer re-login without hiding local data.
func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, site, remote_uid, name, avatar_url, credential_enc, proxy_enc, credential_updated_at, state, created_at, updated_at
		FROM accounts
		WHERE state IN ('active', 'needs_reauth') AND length(credential_enc) > 0
		ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Site, &a.RemoteUID, &a.Name, &a.AvatarURL,
			&a.Credential, &a.ProxyEnc, &a.CredentialAt, &a.State, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) getAccount(ctx context.Context, suffix string, args ...any) (*Account, error) {
	q := `SELECT id, site, remote_uid, name, avatar_url, credential_enc, proxy_enc, credential_updated_at, state, created_at, updated_at
	      FROM accounts WHERE 1=1 ` + suffix
	a := &Account{}
	err := s.db.QueryRowContext(ctx, q, args...).Scan(
		&a.ID, &a.Site, &a.RemoteUID, &a.Name, &a.AvatarURL,
		&a.Credential, &a.ProxyEnc, &a.CredentialAt, &a.State, &a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoAccount
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Store) UpsertAccount(ctx context.Context, site, uid, name, avatar string, cred []byte, now int64) (int64, error) {
	return s.UpsertAccountWithProxy(ctx, site, uid, name, avatar, cred, nil, now)
}

func (s *Store) UpsertAccountWithProxy(ctx context.Context, site, uid, name, avatar string, cred, proxyEnc []byte, now int64) (int64, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO accounts (site, remote_uid, name, avatar_url, credential_enc, proxy_enc, credential_updated_at, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)
		ON CONFLICT(site, remote_uid) DO UPDATE SET
			name = excluded.name,
			avatar_url = excluded.avatar_url,
			credential_enc = excluded.credential_enc,
			proxy_enc = excluded.proxy_enc,
			credential_updated_at = excluded.credential_updated_at,
			state = 'active',
			updated_at = excluded.updated_at`,
		site, uid, name, avatar, cred, proxyEnc, now, now, now,
	)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM accounts WHERE site = ? AND remote_uid = ?`, site, uid).Scan(&id)
	return id, err
}

// UpdateAccountLogin refreshes an existing account after an explicit re-login.
func (s *Store) UpdateAccountLogin(ctx context.Context, id int64, name, avatar string, cred, proxyEnc []byte, now int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE accounts SET name = ?, avatar_url = ?, credential_enc = ?, proxy_enc = ?,
			credential_updated_at = ?, state = 'active', updated_at = ?
		WHERE id = ?`,
		name, avatar, cred, proxyEnc, now, now, id)
	return err
}

func (s *Store) UpdateAccountProxy(ctx context.Context, id int64, proxyEnc []byte, now int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE accounts SET proxy_enc = ?, updated_at = ? WHERE id = ?`, proxyEnc, now, id)
	return err
}

// DeleteAccount removes the account and, through ON DELETE CASCADE, its
// folders, items, sync runs, events and download tasks. It never touches the
// remote site. Downloaded media files are not deleted here.
func (s *Store) DeleteAccount(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNoAccount
	}
	return nil
}

func (s *Store) SetAccountState(ctx context.Context, id int64, state string, now int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE accounts SET state = ?, updated_at = ? WHERE id = ?`, state, now, id)
	return err
}

func (s *Store) ClearCredential(ctx context.Context, id int64, now int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE accounts SET credential_enc = NULL, state = 'disabled', updated_at = ? WHERE id = ?`, now, id)
	return err
}

// ---------- folders ----------

type FolderInput struct {
	RemoteID    string
	Title       string
	Kind        string
	OwnerID     string
	Privacy     int
	RemoteCount int
}

type Folder struct {
	ID             int64  `json:"id"`
	AccountID      int64  `json:"account_id"`
	RemoteID       string `json:"remote_id"`
	Site           string `json:"site"`
	Title          string `json:"title"`
	Kind           string `json:"kind"`
	OwnerID        string `json:"owner_id"`
	Privacy        int    `json:"privacy"`
	RemoteCount    int    `json:"remote_count"`
	State          string `json:"state"`
	LastFullSyncAt int64  `json:"last_full_sync_at"`
	ItemCount      int    `json:"item_count"`
	LocalTotal     int    `json:"local_total"`
	LocalDone      int    `json:"local_done"`
	AutoDownload   bool   `json:"auto_download"`
}

func (s *Store) ReconcileFolders(ctx context.Context, accountID int64, in []FolderInput, now int64) (map[string]int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	ids := make(map[string]int64, len(in))
	for _, f := range in {
		kind := f.Kind
		if kind == "" {
			kind = "favorite"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO folders (account_id, remote_id, title, kind, owner_id, privacy, remote_count, state, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)
			ON CONFLICT(account_id, remote_id) DO UPDATE SET
				title = excluded.title,
				kind = excluded.kind,
				owner_id = excluded.owner_id,
				privacy = excluded.privacy,
				remote_count = excluded.remote_count,
				state = 'active',
				updated_at = excluded.updated_at`,
			accountID, f.RemoteID, f.Title, kind, f.OwnerID, f.Privacy, f.RemoteCount, now, now,
		); err != nil {
			return nil, err
		}
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM folders WHERE account_id = ? AND remote_id = ?`, accountID, f.RemoteID).Scan(&id); err != nil {
			return nil, err
		}
		ids[f.RemoteID] = id
	}

	if len(ids) == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE folders SET state = 'archived', updated_at = ? WHERE account_id = ? AND state = 'active'`, now, accountID); err != nil {
			return nil, err
		}
	} else {
		ph := make([]string, 0, len(ids))
		args := make([]any, 0, len(ids)+2)
		args = append(args, now, accountID)
		for remoteID := range ids {
			ph = append(ph, "?")
			args = append(args, remoteID)
		}
		q := `UPDATE folders SET state = 'archived', updated_at = ? WHERE account_id = ? AND state = 'active' AND remote_id NOT IN (` + strings.Join(ph, ",") + `)`
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *Store) ListFolders(ctx context.Context, accountID int64) ([]Folder, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.account_id, f.remote_id, f.title, f.kind, f.owner_id, f.privacy, f.remote_count, f.state, f.last_full_sync_at,
		       (SELECT COUNT(*) FROM folder_items fi WHERE fi.folder_id = f.id AND fi.state <> 'removed') AS item_count,
		       COALESCE((SELECT fp.auto_download FROM folder_preferences fp WHERE fp.folder_id = f.id), 0)
		FROM folders f
		WHERE f.account_id = ?
		ORDER BY CASE f.state WHEN 'active' THEN 0 ELSE 1 END, f.kind, f.title`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.AccountID, &f.RemoteID, &f.Title, &f.Kind, &f.OwnerID, &f.Privacy, &f.RemoteCount, &f.State, &f.LastFullSyncAt, &f.ItemCount, &f.AutoDownload); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetFolderAutoDownload updates a preference only when the folder belongs to
// the supplied account. A missing or foreign folder returns false.
func (s *Store) SetFolderAutoDownload(ctx context.Context, accountID, folderID int64, enabled bool, now int64) (bool, error) {
	value := 0
	if enabled {
		value = 1
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO folder_preferences(folder_id, auto_download, updated_at, after_run_id)
		SELECT id, ?, ?, COALESCE((SELECT MAX(id) FROM sync_runs WHERE account_id = folders.account_id), 0)
		FROM folders WHERE id = ? AND account_id = ?
		ON CONFLICT(folder_id) DO UPDATE SET
			after_run_id = CASE WHEN excluded.auto_download = 1 AND folder_preferences.auto_download = 0
				THEN excluded.after_run_id ELSE folder_preferences.after_run_id END,
			auto_download = excluded.auto_download, updated_at = excluded.updated_at`,
		value, now, folderID, accountID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// AddSource registers an additional sync source. Its remote id is stable so reconciliation can
// keep it active alongside favorites.
func (s *Store) AddSource(ctx context.Context, accountID int64, kind, ownerID, remoteID, title string, now int64) (int64, error) {
	kind = strings.TrimSpace(kind)
	ownerID = strings.TrimSpace(ownerID)
	remoteID = strings.TrimSpace(remoteID)
	title = strings.TrimSpace(title)
	if kind == "" || remoteID == "" {
		return 0, errors.New("source remote id is required")
	}
	if title == "" {
		title = kind
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO folders (account_id, remote_id, title, kind, owner_id, privacy, remote_count, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, 0, 'active', ?, ?)
		ON CONFLICT(account_id, remote_id) DO UPDATE SET
			title = excluded.title, kind = excluded.kind, owner_id = excluded.owner_id,
			state = 'active', updated_at = excluded.updated_at`,
		accountID, remoteID, title, kind, ownerID, now, now,
	); err != nil {
		return 0, err
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM folders WHERE account_id = ? AND remote_id = ?`, accountID, remoteID).Scan(&id)
	return id, err
}

func (s *Store) ListSources(ctx context.Context, accountID int64) ([]Folder, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.account_id, f.remote_id, f.title, f.kind, f.owner_id, f.privacy, f.remote_count, f.state, f.last_full_sync_at, 0,
		       COALESCE((SELECT fp.auto_download FROM folder_preferences fp WHERE fp.folder_id = f.id), 0)
		FROM folders f
		WHERE f.account_id = ? AND f.kind <> 'favorite' AND f.state = 'active'
		ORDER BY f.kind, f.title`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.AccountID, &f.RemoteID, &f.Title, &f.Kind, &f.OwnerID, &f.Privacy, &f.RemoteCount, &f.State, &f.LastFullSyncAt, &f.ItemCount, &f.AutoDownload); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) CountActiveLinks(ctx context.Context, folderID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM folder_items WHERE folder_id = ? AND state = 'active'`, folderID).Scan(&n)
	return n, err
}

func (s *Store) SetFolderSynced(ctx context.Context, folderID, now int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE folders SET last_full_sync_at = ?, updated_at = ? WHERE id = ?`, now, now, folderID)
	return err
}

// ---------- items ----------

type ItemInput struct {
	RemoteID    string
	Kind        int
	Title       string
	URL         string
	CoverURL    string
	Duration    int
	AuthorID    string
	AuthorName  string
	Summary     string
	PublishedAt int64
	Attr        int
}

type Item struct {
	ID          int64       `json:"id"`
	AccountID   int64       `json:"account_id"`
	RemoteID    string      `json:"remote_id"`
	Kind        int         `json:"kind"`
	Title       string      `json:"title"`
	URL         string      `json:"url"`
	CoverURL    string      `json:"cover_url"`
	Duration    int         `json:"duration"`
	AuthorID    string      `json:"author_id"`
	AuthorName  string      `json:"author_name"`
	Summary     string      `json:"summary"`
	PublishedAt int64       `json:"published_at"`
	Attr        int         `json:"attr"`
	State       string      `json:"state"`
	FirstSeenAt int64       `json:"first_seen_at"`
	LastSeenAt  int64       `json:"last_seen_at"`
	UpdatedAt   int64       `json:"updated_at"`
	Folders     []FolderRef `json:"folders,omitempty"`
	Local       LocalStatus `json:"local"`
}

type FolderRef struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
}

type Link struct {
	ItemID       int64
	State        string
	MissingCount int
}

func (s *Store) WithTx(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(&Tx{tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

type Tx struct{ tx *sql.Tx }

func (t *Tx) UpsertItem(ctx context.Context, accountID int64, in ItemInput, now int64) (id int64, isNew, changed bool, err error) {
	var (
		curTitle, curURL, curCover, curAuthorID, curAuthorName, curSummary string
		curDuration, curKind, curAttr                                      int
		curPublishedAt                                                     int64
	)
	err = t.tx.QueryRowContext(ctx, `
		SELECT id, title, url, cover_url, duration, kind, attr, author_id, author_name, summary, published_at
		FROM items WHERE account_id = ? AND remote_id = ?`, accountID, in.RemoteID,
	).Scan(&id, &curTitle, &curURL, &curCover, &curDuration, &curKind, &curAttr, &curAuthorID, &curAuthorName, &curSummary, &curPublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		res, execErr := t.tx.ExecContext(ctx, `
			INSERT INTO items (account_id, remote_id, kind, title, url, cover_url, duration, author_id, author_name, summary, published_at, attr, state, first_seen_at, last_seen_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)`,
			accountID, in.RemoteID, in.Kind, in.Title, in.URL, in.CoverURL, in.Duration,
			in.AuthorID, in.AuthorName, in.Summary, in.PublishedAt, in.Attr, now, now, now)
		if execErr != nil {
			return 0, false, false, execErr
		}
		id, err = res.LastInsertId()
		return id, true, false, err
	}
	if err != nil {
		return 0, false, false, err
	}

	changed = curTitle != in.Title || curURL != in.URL || curCover != in.CoverURL ||
		curDuration != in.Duration || curKind != in.Kind || curAttr != in.Attr ||
		curAuthorID != in.AuthorID || curAuthorName != in.AuthorName || curSummary != in.Summary ||
		curPublishedAt != in.PublishedAt
	if changed {
		_, err = t.tx.ExecContext(ctx, `
			UPDATE items SET kind = ?, title = ?, url = ?, cover_url = ?, duration = ?,
				author_id = ?, author_name = ?, summary = ?, published_at = ?, attr = ?,
				last_seen_at = ?, updated_at = ? WHERE id = ?`,
			in.Kind, in.Title, in.URL, in.CoverURL, in.Duration,
			in.AuthorID, in.AuthorName, in.Summary, in.PublishedAt, in.Attr, now, now, id)
	} else {
		_, err = t.tx.ExecContext(ctx, `UPDATE items SET last_seen_at = ? WHERE id = ?`, now, id)
	}
	return id, false, changed, err
}

func (t *Tx) UpsertFolderItem(ctx context.Context, folderID, itemID, favoritedAt int64) (prev string, err error) {
	err = t.tx.QueryRowContext(ctx, `SELECT state FROM folder_items WHERE folder_id = ? AND item_id = ?`, folderID, itemID).Scan(&prev)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = t.tx.ExecContext(ctx, `
			INSERT INTO folder_items (folder_id, item_id, favorited_at, state, missing_count)
			VALUES (?, ?, ?, 'active', 0)`, folderID, itemID, favoritedAt)
		return "", err
	}
	if err != nil {
		return "", err
	}
	_, err = t.tx.ExecContext(ctx, `
		UPDATE folder_items SET favorited_at = ?, state = 'active', missing_since = NULL, missing_count = 0, removed_at = NULL
		WHERE folder_id = ? AND item_id = ?`, favoritedAt, folderID, itemID)
	return prev, err
}

func (t *Tx) ListFolderLinks(ctx context.Context, folderID int64) ([]Link, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT item_id, state, missing_count FROM folder_items WHERE folder_id = ?`, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ItemID, &l.State, &l.MissingCount); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// MarkLinkMissing advances a link through missing -> removed. It returns the
// event kind to record, or "" when no transition happened.
func (t *Tx) MarkLinkMissing(ctx context.Context, folderID, itemID int64, grace int, now int64) (string, error) {
	var state string
	var count int
	err := t.tx.QueryRowContext(ctx, `SELECT state, missing_count FROM folder_items WHERE folder_id = ? AND item_id = ?`, folderID, itemID).Scan(&state, &count)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	switch state {
	case "active":
		_, err = t.tx.ExecContext(ctx, `
			UPDATE folder_items SET state = 'missing', missing_since = ?, missing_count = 1 WHERE folder_id = ? AND item_id = ?`,
			now, folderID, itemID)
		return "missing", err
	case "missing":
		count++
		if count >= grace {
			_, err = t.tx.ExecContext(ctx, `
				UPDATE folder_items SET state = 'removed', missing_count = ?, removed_at = ? WHERE folder_id = ? AND item_id = ?`,
				count, now, folderID, itemID)
			return "removed", err
		}
		_, err = t.tx.ExecContext(ctx, `UPDATE folder_items SET missing_count = ? WHERE folder_id = ? AND item_id = ?`, count, folderID, itemID)
		return "", err
	default:
		return "", nil
	}
}

func (t *Tx) RecomputeItemStates(ctx context.Context, accountID int64) error {
	_, err := t.tx.ExecContext(ctx, `
		UPDATE items SET state = CASE
			WHEN EXISTS (SELECT 1 FROM folder_items fi WHERE fi.item_id = items.id AND fi.state = 'active') THEN 'active'
			WHEN EXISTS (SELECT 1 FROM folder_items fi WHERE fi.item_id = items.id AND fi.state = 'missing') THEN 'missing'
			ELSE 'removed' END
		WHERE account_id = ?`, accountID)
	return err
}

func (t *Tx) InsertEvent(ctx context.Context, runID, itemID, folderID int64, kind string, now int64) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO item_events (run_id, item_id, folder_id, kind, detail, created_at)
		VALUES (?, ?, ?, ?, '', ?)`, runID, itemID, folderID, kind, now)
	return err
}

// ---------- queries ----------

type ItemFilter struct {
	FolderID int64
	State    string
	AuthorID string
	Query    string
	Sort     string
	Page     int
	PageSize int
}

func (s *Store) QueryItems(ctx context.Context, accountID int64, f ItemFilter) ([]Item, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 200 {
		f.PageSize = 60
	}
	where := []string{"i.account_id = ?"}
	args := []any{accountID}
	if f.FolderID > 0 {
		where = append(where, "EXISTS (SELECT 1 FROM folder_items fi WHERE fi.folder_id = ? AND fi.item_id = i.id)")
		args = append(args, f.FolderID)
	}
	if f.State != "" {
		where = append(where, "i.state = ?")
		args = append(args, f.State)
	}
	if f.AuthorID != "" {
		where = append(where, "i.author_id = ?")
		args = append(args, f.AuthorID)
	}
	if f.Query != "" {
		like := "%" + f.Query + "%"
		where = append(where, "(i.title LIKE ? OR i.author_name LIKE ? OR i.summary LIKE ?)")
		args = append(args, like, like, like)
	}
	base := " FROM items i WHERE " + strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*)"+base, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	order := "i.first_seen_at DESC, i.id DESC"
	switch f.Sort {
	case "published":
		order = "i.published_at DESC, i.id DESC"
	case "favorited":
		order = "(SELECT MAX(fi.favorited_at) FROM folder_items fi WHERE fi.item_id = i.id) DESC, i.id DESC"
	case "title":
		order = "i.title ASC, i.id ASC"
	}

	q := `SELECT i.id, i.account_id, i.remote_id, i.kind, i.title, i.url, i.cover_url, i.duration,
		i.author_id, i.author_name, i.summary, i.published_at, i.attr, i.state, i.first_seen_at, i.last_seen_at, i.updated_at` +
		base + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	rows, err := s.db.QueryContext(ctx, q, append(args, f.PageSize, (f.Page-1)*f.PageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := scanItem(rows, &it); err != nil {
			return nil, 0, err
		}
		out = append(out, it)
	}
	return out, total, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanItem(row scanner, it *Item) error {
	return row.Scan(&it.ID, &it.AccountID, &it.RemoteID, &it.Kind, &it.Title, &it.URL, &it.CoverURL,
		&it.Duration, &it.AuthorID, &it.AuthorName, &it.Summary, &it.PublishedAt, &it.Attr, &it.State,
		&it.FirstSeenAt, &it.LastSeenAt, &it.UpdatedAt)
}

// FavoriteTime returns the latest local favorite timestamp for a remote item.
func (s *Store) FavoriteTime(ctx context.Context, accountID int64, remoteID string) int64 {
	var ts int64
	_ = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(fi.favorited_at), 0)
		FROM folder_items fi
		JOIN folders f ON f.id = fi.folder_id
		JOIN items i ON i.id = fi.item_id
		WHERE f.account_id = ? AND i.remote_id = ?`, accountID, remoteID).Scan(&ts)
	return ts
}

// ListFolderItems returns every current item in one folder for direct
// downloads. It intentionally follows the same historical folder membership as
// QueryItems, so removed/missing rows keep their local record.
func (s *Store) ListFolderItems(ctx context.Context, accountID, folderID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT i.id, i.account_id, i.remote_id, i.kind, i.title, i.url, i.cover_url,
			i.duration, i.author_id, i.author_name, i.summary, i.published_at, i.attr, i.state,
			i.first_seen_at, i.last_seen_at, i.updated_at
		FROM items i JOIN folder_items fi ON fi.item_id = i.id
		WHERE i.account_id = ? AND fi.folder_id = ?
		ORDER BY i.id`, accountID, folderID)
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

func (s *Store) GetItem(ctx context.Context, accountID, id int64) (*Item, error) {
	row := s.db.QueryRowContext(ctx, `SELECT i.id, i.account_id, i.remote_id, i.kind, i.title, i.url, i.cover_url, i.duration,
		i.author_id, i.author_name, i.summary, i.published_at, i.attr, i.state, i.first_seen_at, i.last_seen_at, i.updated_at
		FROM items i WHERE i.account_id = ? AND i.id = ?`, accountID, id)
	var it Item
	if err := scanItem(row, &it); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.title, fi.state FROM folders f
		JOIN folder_items fi ON fi.folder_id = f.id
		WHERE fi.item_id = ? ORDER BY f.title`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var fr FolderRef
		if err := rows.Scan(&fr.ID, &fr.Title, &fr.State); err != nil {
			return nil, err
		}
		it.Folders = append(it.Folders, fr)
	}
	return &it, rows.Err()
}

// ---------- runs ----------

type Run struct {
	ID           int64  `json:"id"`
	AccountID    int64  `json:"account_id"`
	Trigger      string `json:"trigger"`
	Scope        string `json:"scope"`
	State        string `json:"state"`
	StartedAt    int64  `json:"started_at"`
	FinishedAt   int64  `json:"finished_at"`
	FoldersTotal int    `json:"folders_total"`
	FoldersDone  int    `json:"folders_done"`
	ItemsSeen    int    `json:"items_seen"`
	ItemsAdded   int    `json:"items_added"`
	ItemsUpdated int    `json:"items_updated"`
	ItemsMissing int    `json:"items_missing"`
	ItemsRemoved int    `json:"items_removed"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

func (s *Store) CreateRun(ctx context.Context, accountID int64, trigger, scope string, now int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_runs (account_id, trigger, scope, state, started_at) VALUES (?, ?, ?, 'running', ?)`,
		accountID, trigger, scope, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FinishRun(ctx context.Context, runID int64, state string, foldersTotal, foldersDone, seen, added, updated, missing, removed int, errCode, errMsg string, now int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sync_runs SET state = ?, finished_at = ?, folders_total = ?, folders_done = ?,
			items_seen = ?, items_added = ?, items_updated = ?, items_missing = ?, items_removed = ?,
			error_code = ?, error_message = ? WHERE id = ?`,
		state, now, foldersTotal, foldersDone, seen, added, updated, missing, removed, errCode, errMsg, runID)
	return err
}

func scanRun(row scanner, r *Run) error {
	var finished sql.NullInt64
	var errCode, errMsg sql.NullString
	if err := row.Scan(&r.ID, &r.AccountID, &r.Trigger, &r.Scope, &r.State, &r.StartedAt, &finished,
		&r.FoldersTotal, &r.FoldersDone, &r.ItemsSeen, &r.ItemsAdded, &r.ItemsUpdated, &r.ItemsMissing, &r.ItemsRemoved,
		&errCode, &errMsg); err != nil {
		return err
	}
	r.FinishedAt = finished.Int64
	r.ErrorCode = errCode.String
	r.ErrorMessage = errMsg.String
	return nil
}

func (s *Store) ListRuns(ctx context.Context, accountID int64, limit int) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, account_id, trigger, scope, state, started_at, finished_at, folders_total, folders_done,
			items_seen, items_added, items_updated, items_missing, items_removed, error_code, error_message
		FROM sync_runs WHERE account_id = ? ORDER BY started_at DESC, id DESC LIMIT ?`, accountID, limit)
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

func (s *Store) GetRun(ctx context.Context, accountID, id int64) (*Run, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, account_id, trigger, scope, state, started_at, finished_at, folders_total, folders_done,
			items_seen, items_added, items_updated, items_missing, items_removed, error_code, error_message
		FROM sync_runs WHERE account_id = ? AND id = ?`, accountID, id)
	var r Run
	if err := scanRun(row, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) LastRun(ctx context.Context, accountID int64) (*Run, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, account_id, trigger, scope, state, started_at, finished_at, folders_total, folders_done,
			items_seen, items_added, items_updated, items_missing, items_removed, error_code, error_message
		FROM sync_runs WHERE account_id = ? ORDER BY started_at DESC, id DESC LIMIT 1`, accountID)
	var r Run
	if err := scanRun(row, &r); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// ---------- export ----------

type ExportFolderItem struct {
	FolderID    int64  `json:"folder_id"`
	ItemID      int64  `json:"item_id"`
	FavoritedAt int64  `json:"favorited_at"`
	State       string `json:"state"`
}

func (s *Store) AllFolders(ctx context.Context, accountID int64) ([]Folder, error) {
	return s.ListFolders(ctx, accountID)
}

func (s *Store) AllItems(ctx context.Context, accountID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id, i.account_id, i.remote_id, i.kind, i.title, i.url, i.cover_url,
		i.duration, i.author_id, i.author_name, i.summary, i.published_at, i.attr, i.state, i.first_seen_at, i.last_seen_at, i.updated_at
		FROM items i WHERE i.account_id = ? ORDER BY i.id`, accountID)
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

func (s *Store) AllFolderItems(ctx context.Context, accountID int64) ([]ExportFolderItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fi.folder_id, fi.item_id, fi.favorited_at, fi.state
		FROM folder_items fi JOIN folders f ON f.id = fi.folder_id
		WHERE f.account_id = ? ORDER BY fi.folder_id, fi.item_id`, accountID)
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

// ---------- overview ----------

func (s *Store) CountItems(ctx context.Context, accountID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE account_id = ?`, accountID).Scan(&n)
	return n, err
}

func (s *Store) CountFolders(ctx context.Context, accountID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM folders WHERE account_id = ? AND state = 'active'`, accountID).Scan(&n)
	return n, err
}
