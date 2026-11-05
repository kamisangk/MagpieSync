//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"magpiesync/internal/sync"
)

// Adapter implements sync.Source for one bilibili account.
type Adapter struct {
	client  *Client
	mid     string
	sources []sync.RemoteFolder
}

func NewAdapter(c *Client, mid string) *Adapter {
	return &Adapter{client: c, mid: mid}
}

// SetSources registers extra read-only sources (watch later, submissions,
// seasons, series) that are synced alongside favorites.
func (a *Adapter) SetSources(sources []sync.RemoteFolder) {
	a.sources = sources
}

func (a *Adapter) ListFolders(ctx context.Context) ([]sync.RemoteFolder, error) {
	folders, err := a.client.FavFolders(ctx, a.mid)
	if err != nil {
		return nil, err
	}
	out := make([]sync.RemoteFolder, 0, len(folders)+len(a.sources))
	for _, f := range folders {
		out = append(out, sync.RemoteFolder{
			RemoteID: f.RemoteID,
			Title:    f.Title,
			Kind:     "favorite",
			OwnerID:  a.mid,
			Privacy:  f.Privacy,
			Count:    f.Count,
		})
	}
	seen := make(map[string]bool, len(out))
	for _, f := range out {
		seen[f.RemoteID] = true
	}
	for _, f := range a.sources {
		if f.RemoteID == "" || seen[f.RemoteID] {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

func (a *Adapter) ListFolderPage(ctx context.Context, remoteID string, page, pageSize int) (sync.RemotePage, error) {
	kind := SourceKindFromRemoteID(remoteID)
	var (
		p   *Page
		err error
	)
	switch kind {
	case "watch_later":
		p, err = a.client.WatchLaterPage(ctx, page, pageSize)
	case "submission":
		mid := strings.TrimPrefix(remoteID, "submission:")
		if mid == "" {
			mid = a.findSourceMid(remoteID)
		}
		p, err = a.client.SpaceVideosPage(ctx, mid, page, pageSize)
	case "season":
		mid, id := splitSourceID(remoteID, "season:")
		p, err = a.client.SeasonArchivesPage(ctx, mid, id, page, pageSize)
	case "series":
		mid, id := splitSourceID(remoteID, "series:")
		p, err = a.client.SeriesArchivesPage(ctx, mid, id, page, pageSize)
	default:
		p, err = a.client.FavPage(ctx, remoteID, page, pageSize)
	}
	if err != nil {
		return sync.RemotePage{}, err
	}
	out := sync.RemotePage{HasMore: p.HasMore}
	for _, m := range p.Items {
		out.Items = append(out.Items, sync.RemoteItem{
			RemoteID:    m.RemoteID,
			Kind:        m.Kind,
			Title:       m.Title,
			URL:         m.URL,
			CoverURL:    m.CoverURL,
			Duration:    m.Duration,
			AuthorID:    m.AuthorID,
			AuthorName:  m.AuthorName,
			Summary:     m.Summary,
			PublishedAt: m.PublishedAt,
			FavoritedAt: m.FavoritedAt,
			Attr:        m.Attr,
		})
	}
	return out, nil
}

func (a *Adapter) findSourceMid(remoteID string) string {
	for _, s := range a.sources {
		if s.RemoteID == remoteID {
			return s.OwnerID
		}
	}
	return ""
}

func splitSourceID(remoteID, prefix string) (string, string) {
	raw := strings.TrimPrefix(remoteID, prefix)
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return "", raw
	}
	return parts[0], parts[1]
}

// ParseSourceRef validates a source reference such as season:123:456.
func ParseSourceRef(raw string) (kind, mid, remoteID string, err error) {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "watch_later":
		return "watch_later", "", "watch_later", nil
	case strings.HasPrefix(raw, "submission:"):
		mid = strings.TrimPrefix(raw, "submission:")
		if mid == "" {
			return "", "", "", errors.New("submission source requires UP mid")
		}
		return "submission", mid, "submission:" + mid, nil
	case strings.HasPrefix(raw, "season:"), strings.HasPrefix(raw, "series:"):
		kind = strings.SplitN(raw, ":", 2)[0]
		mid, id := splitSourceID(raw, kind+":")
		if mid == "" || id == "" {
			return "", "", "", errors.New(kind + " source requires mid and id")
		}
		return kind, mid, kind + ":" + mid + ":" + id, nil
	default:
		return "", "", "", errors.New("unsupported source reference")
	}
}

func ParseIntID(v string) (int64, error) { return strconv.ParseInt(v, 10, 64) }

func IsFatal(err error) bool { return IsAuth(err) || IsRisk(err) }
