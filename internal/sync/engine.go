package sync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"magpiesync/internal/store"
)

var ErrAlreadyRunning = errors.New("a sync is already running for this account")

const (
	defaultPageSize = 20
	maxPages        = 500
	minGuardCount   = 20
)

type Counters struct {
	Seen    int
	Added   int
	Updated int
	Missing int
	Removed int
}

type Progress struct {
	RunID        int64  `json:"run_id"`
	Folder       string `json:"folder,omitempty"`
	FoldersDone  int    `json:"folders_done"`
	FoldersTotal int    `json:"folders_total"`
	ItemsSeen    int    `json:"items_seen"`
	State        string `json:"state"`
	Error        string `json:"error,omitempty"`
}

type Factory func(ctx context.Context, accountID int64) (Source, error)

type Engine struct {
	store    *store.Store
	factory  Factory
	onEvent  func(Progress)
	fatalFn  func(error) bool
	authFn   func(error) bool
	grace    int
	pageSize int

	mu      sync.Mutex
	running map[int64]int64
	cancel  map[int64]context.CancelFunc
}

func NewEngine(st *store.Store, factory Factory) *Engine {
	return &Engine{
		store:    st,
		factory:  factory,
		grace:    2,
		pageSize: defaultPageSize,
		running:  make(map[int64]int64),
		cancel:   make(map[int64]context.CancelFunc),
	}
}

func (e *Engine) SetProgress(fn func(Progress))          { e.onEvent = fn }
func (e *Engine) SetFatalClassifier(fn func(error) bool) { e.fatalFn = fn }
func (e *Engine) SetAuthClassifier(fn func(error) bool)  { e.authFn = fn }
func (e *Engine) SetGrace(n int) {
	if n >= 1 {
		e.grace = n
	}
}

func (e *Engine) IsRunning(accountID int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.running[accountID]
	return ok
}

// AnyRunning reports whether at least one account is syncing. The global
// dashboard uses it because it no longer follows a single active account.
func (e *Engine) AnyRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.running) > 0
}

func (e *Engine) Start(accountID int64, trigger, scope string) (int64, error) {
	e.mu.Lock()
	if _, ok := e.running[accountID]; ok {
		e.mu.Unlock()
		return 0, ErrAlreadyRunning
	}
	runID, err := e.store.CreateRun(context.Background(), accountID, trigger, scope, time.Now().Unix())
	if err != nil {
		e.mu.Unlock()
		return 0, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.running[accountID] = runID
	e.cancel[accountID] = cancel
	e.mu.Unlock()

	go e.run(ctx, runID, accountID, scope)
	return runID, nil
}

func (e *Engine) Cancel(accountID int64) {
	e.mu.Lock()
	cancel := e.cancel[accountID]
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (e *Engine) CancelAll() {
	e.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(e.cancel))
	for _, c := range e.cancel {
		cancels = append(cancels, c)
	}
	e.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

func (e *Engine) progress(p Progress) {
	if e.onEvent != nil {
		e.onEvent(p)
	}
}

func (e *Engine) fatal(err error) bool {
	return e.fatalFn != nil && e.fatalFn(err)
}

func (e *Engine) isAuth(err error) bool {
	return e.authFn != nil && e.authFn(err)
}

func (e *Engine) markReauth(accountID int64) {
	if err := e.store.SetAccountState(context.Background(), accountID, "needs_reauth", time.Now().Unix()); err != nil {
		slog.Error("mark account needs_reauth", "account_id", accountID, "error", err)
	}
}

func (e *Engine) run(ctx context.Context, runID, accountID int64, scope string) {
	var (
		total      int
		done       int
		c          Counters
		state      = "success"
		errCode    string
		errMsg     string
		partial    bool
		suspicious bool
	)

	defer func() {
		switch {
		case ctx.Err() != nil:
			state = "aborted"
		case suspicious:
			state = "suspicious"
		case partial && state == "success":
			state = "partial"
		}
		finishCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := e.store.FinishRun(finishCtx, runID, state, total, done, c.Seen, c.Added, c.Updated, c.Missing, c.Removed, errCode, errMsg, time.Now().Unix()); err != nil {
			slog.Error("finish sync run", "run_id", runID, "error", err)
		}
		e.progress(Progress{RunID: runID, FoldersDone: done, FoldersTotal: total, ItemsSeen: c.Seen, State: state, Error: errMsg})
		e.mu.Lock()
		delete(e.running, accountID)
		delete(e.cancel, accountID)
		e.mu.Unlock()
	}()

	src, err := e.factory(ctx, accountID)
	if err != nil {
		state, errCode, errMsg = "failed", "source", err.Error()
		return
	}
	if closer, ok := src.(io.Closer); ok {
		defer func() {
			if err := closer.Close(); err != nil {
				slog.Warn("close sync source", "account_id", accountID, "error", err)
			}
		}()
	}

	folders, err := src.ListFolders(ctx)
	if err != nil {
		if e.isAuth(err) {
			e.markReauth(accountID)
		}
		state, errCode, errMsg = "failed", classifyCode(err), err.Error()
		return
	}

	inputs := make([]store.FolderInput, 0, len(folders))
	for _, f := range folders {
		inputs = append(inputs, store.FolderInput{
			RemoteID: f.RemoteID, Title: f.Title, Kind: f.Kind, OwnerID: f.OwnerID,
			Privacy: f.Privacy, RemoteCount: f.Count,
		})
	}
	ids, err := e.store.ReconcileFolders(ctx, accountID, inputs, time.Now().Unix())
	if err != nil {
		state, errCode, errMsg = "failed", "store", err.Error()
		return
	}

	selected := 0
	for _, f := range folders {
		if scopeMatches(scope, f.RemoteID) {
			selected++
		}
	}
	total = selected

	for _, rf := range folders {
		if !scopeMatches(scope, rf.RemoteID) {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		folderID := ids[rf.RemoteID]
		if folderID == 0 {
			continue
		}

		items, complete, enumErr := e.enumerate(ctx, src, rf.RemoteID)
		if enumErr != nil {
			done++
			partial = true
			if errMsg == "" {
				errMsg = fmt.Sprintf("%s: %v", rf.Title, enumErr)
			}
			if e.fatal(enumErr) || ctx.Err() != nil {
				if e.isAuth(enumErr) {
					e.markReauth(accountID)
				}
				state, errCode = "failed", classifyCode(enumErr)
				return
			}
			e.progress(Progress{RunID: runID, Folder: rf.Title, FoldersDone: done, FoldersTotal: total, ItemsSeen: c.Seen, State: "running", Error: enumErr.Error()})
			continue
		}

		fc, sus, err := e.reconcileFolder(ctx, runID, accountID, folderID, rf, items, complete)
		if err != nil {
			state, errCode, errMsg = "failed", "store", err.Error()
			return
		}
		if sus {
			suspicious = true
			if errMsg == "" {
				errMsg = fmt.Sprintf("%s: item count dropped sharply, removal step skipped", rf.Title)
			}
		}

		c.Seen += fc.Seen
		c.Added += fc.Added
		c.Updated += fc.Updated
		c.Missing += fc.Missing
		c.Removed += fc.Removed
		done++

		e.progress(Progress{RunID: runID, Folder: rf.Title, FoldersDone: done, FoldersTotal: total, ItemsSeen: c.Seen, State: "running"})
	}

	if ctx.Err() != nil {
		return
	}
	if errMsg != "" && state == "success" && !partial {
		errMsg = ""
	}
}

func scopeMatches(scope, remoteID string) bool {
	if scope == "" || scope == "all" {
		return true
	}
	return scope == "folder:"+remoteID
}

func classifyCode(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.Index(msg, "code "); i >= 0 {
		return msg[i:]
	}
	return "error"
}

func (e *Engine) enumerate(ctx context.Context, src Source, remoteID string) ([]RemoteItem, bool, error) {
	var out []RemoteItem
	for page := 1; page <= maxPages; page++ {
		p, err := src.ListFolderPage(ctx, remoteID, page, e.pageSize)
		if err != nil {
			return out, false, err
		}
		out = append(out, p.Items...)
		if !p.HasMore {
			return out, true, nil
		}
	}
	return out, false, fmt.Errorf("folder %s exceeded %d pages", remoteID, maxPages)
}

func (e *Engine) reconcileFolder(ctx context.Context, runID, accountID, folderID int64, rf RemoteFolder, items []RemoteItem, complete bool) (Counters, bool, error) {
	var c Counters
	c.Seen = len(items)

	prev, err := e.store.CountActiveLinks(ctx, folderID)
	if err != nil {
		return c, false, err
	}
	suspicious := complete && prev >= minGuardCount && len(items)*2 < prev

	now := time.Now().Unix()
	err = e.store.WithTx(ctx, func(tx *store.Tx) error {
		seen := make(map[int64]struct{}, len(items))
		for _, it := range items {
			id, _, changed, err := tx.UpsertItem(ctx, accountID, store.ItemInput{
				RemoteID: it.RemoteID, Kind: it.Kind, Title: it.Title, URL: it.URL,
				CoverURL: it.CoverURL, Duration: it.Duration, AuthorID: it.AuthorID, AuthorName: it.AuthorName,
				Summary: it.Summary, PublishedAt: it.PublishedAt, Attr: it.Attr,
			}, now)
			if err != nil {
				return err
			}
			prevState, err := tx.UpsertFolderItem(ctx, folderID, id, it.FavoritedAt)
			if err != nil {
				return err
			}
			seen[id] = struct{}{}

			switch {
			case prevState == "":
				c.Added++
				if err := tx.InsertEvent(ctx, runID, id, folderID, "added", now); err != nil {
					return err
				}
			case prevState == "missing" || prevState == "removed":
				if err := tx.InsertEvent(ctx, runID, id, folderID, "restored", now); err != nil {
					return err
				}
			}
			if changed {
				c.Updated++
			}
		}

		if complete && !suspicious {
			links, err := tx.ListFolderLinks(ctx, folderID)
			if err != nil {
				return err
			}
			for _, l := range links {
				if _, ok := seen[l.ItemID]; ok {
					continue
				}
				kind, err := tx.MarkLinkMissing(ctx, folderID, l.ItemID, e.grace, now)
				if err != nil {
					return err
				}
				switch kind {
				case "missing":
					c.Missing++
					if err := tx.InsertEvent(ctx, runID, l.ItemID, folderID, "missing", now); err != nil {
						return err
					}
				case "removed":
					c.Removed++
					if err := tx.InsertEvent(ctx, runID, l.ItemID, folderID, "removed", now); err != nil {
						return err
					}
				}
			}
		}

		return tx.RecomputeItemStates(ctx, accountID)
	})
	if err != nil {
		return c, suspicious, err
	}
	if complete && !suspicious {
		if err := e.store.SetFolderSynced(ctx, folderID, now); err != nil {
			return c, suspicious, err
		}
	}
	return c, suspicious, nil
}
