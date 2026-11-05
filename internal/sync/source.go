package sync

import "context"

// RemoteFolder and RemoteItem are the site-neutral shapes the engine works on.
type RemoteFolder struct {
	RemoteID string
	Title    string
	Kind     string
	OwnerID  string
	Privacy  int
	Count    int
}

type RemoteItem struct {
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
	FavoritedAt int64
	Attr        int
}

type RemotePage struct {
	Items   []RemoteItem
	HasMore bool
}

// Source is read-only by design. Do not add write methods.
type Source interface {
	ListFolders(ctx context.Context) ([]RemoteFolder, error)
	ListFolderPage(ctx context.Context, remoteID string, page, pageSize int) (RemotePage, error)
}
