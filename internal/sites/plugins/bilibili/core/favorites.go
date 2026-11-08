//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type Folder struct {
	RemoteID string
	Title    string
	Privacy  int
	Count    int
}

type Media struct {
	RemoteID    string
	BVID        string
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

type Page struct {
	Items   []Media
	HasMore bool
}

func (c *Client) FavFolders(ctx context.Context, mid string) ([]Folder, error) {
	nav, err := c.Nav(ctx)
	if err != nil {
		return nil, err
	}
	if strconv.FormatInt(nav.Mid, 10) != strings.TrimSpace(mid) {
		return nil, fmt.Errorf("%w: logged-in account does not match favorite owner", ErrAuth)
	}

	var d struct {
		List []struct {
			ID         int64  `json:"id"`
			FID        int64  `json:"fid"`
			Attr       int    `json:"attr"`
			Title      string `json:"title"`
			MediaCount int    `json:"media_count"`
		} `json:"list"`
	}
	params := url.Values{"up_mid": {mid}}
	if err := c.apiGet(ctx, "/x/v3/fav/folder/created/list-all", params, true, &d); err != nil {
		return nil, err
	}
	out := make([]Folder, 0, len(d.List))
	for _, f := range d.List {
		id := f.ID
		if id == 0 {
			id = f.FID
		}
		if id == 0 {
			continue
		}
		out = append(out, Folder{
			RemoteID: strconv.FormatInt(id, 10),
			Title:    f.Title,
			Privacy:  f.Attr,
			Count:    f.MediaCount,
		})
	}
	return out, nil
}

func (c *Client) FavPage(ctx context.Context, mediaID string, pn, ps int) (*Page, error) {
	if ps <= 0 || ps > 20 {
		ps = 20
	}
	if pn < 1 {
		pn = 1
	}
	var d struct {
		Medias  []apiMedia `json:"medias"`
		HasMore bool       `json:"has_more"`
	}
	params := url.Values{
		"media_id": {mediaID},
		"pn":       {strconv.Itoa(pn)},
		"ps":       {strconv.Itoa(ps)},
		"order":    {"mtime"},
		"type":     {"0"},
		"tid":      {"0"},
		"platform": {"web"},
	}
	if err := c.apiGet(ctx, "/x/v3/fav/resource/list", params, true, &d); err != nil {
		return nil, err
	}
	out := &Page{HasMore: d.HasMore}
	for _, m := range d.Medias {
		if m.ID == 0 {
			continue
		}
		bvid := m.BVVID
		if bvid == "" {
			bvid = m.BVID
		}
		link := m.Link
		if bvid != "" {
			link = "https://www.bilibili.com/video/" + bvid
		}
		if link == "" {
			link = fmt.Sprintf("https://www.bilibili.com/video/av%d", m.ID)
		}
		out.Items = append(out.Items, Media{
			RemoteID:    strconv.FormatInt(m.ID, 10),
			BVID:        bvid,
			Kind:        m.Type,
			Title:       m.Title,
			URL:         link,
			CoverURL:    m.Cover,
			Duration:    m.Duration,
			AuthorID:    strconv.FormatInt(m.Upper.Mid, 10),
			AuthorName:  m.Upper.Name,
			Summary:     m.Intro,
			PublishedAt: m.Pubtime,
			FavoritedAt: m.FavTime,
			Attr:        m.Attr,
		})
	}
	return out, nil
}

type apiMedia struct {
	ID       int64  `json:"id"`
	Type     int    `json:"type"`
	Title    string `json:"title"`
	Cover    string `json:"cover"`
	Intro    string `json:"intro"`
	Duration int    `json:"duration"`
	Upper    struct {
		Mid  int64  `json:"mid"`
		Name string `json:"name"`
	} `json:"upper"`
	Attr    int    `json:"attr"`
	Link    string `json:"link"`
	Pubtime int64  `json:"pubtime"`
	FavTime int64  `json:"fav_time"`
	BVID    string `json:"bvid"`
	BVVID   string `json:"bv_id"`
}
