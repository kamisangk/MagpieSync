//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// WatchLaterPage lists the account's "watch later" videos.
func (c *Client) WatchLaterPage(ctx context.Context, pn, ps int) (*Page, error) {
	if pn < 1 {
		pn = 1
	}
	if ps < 1 || ps > 100 {
		ps = 20
	}
	var d struct {
		Count int `json:"count"`
		List  []struct {
			AID      int64  `json:"aid"`
			BVID     string `json:"bvid"`
			Title    string `json:"title"`
			Pic      string `json:"pic"`
			Duration int    `json:"duration"`
			Pubdate  int64  `json:"pubdate"`
			AddTime  int64  `json:"add_time"`
			Owner    struct {
				Mid  int64  `json:"mid"`
				Name string `json:"name"`
			} `json:"owner"`
		} `json:"list"`
	}
	params := url.Values{"pn": {strconv.Itoa(pn)}, "ps": {strconv.Itoa(ps)}}
	if err := c.apiGet(ctx, "/x/v2/history/toview", params, true, &d); err != nil {
		return nil, err
	}
	out := &Page{HasMore: pn*ps < d.Count}
	for _, m := range d.List {
		if m.AID == 0 && m.BVID == "" {
			continue
		}
		bvid := m.BVID
		link := videoURL(bvid, m.AID)
		out.Items = append(out.Items, Media{
			RemoteID:    remoteID(bvid, m.AID),
			BVID:        bvid,
			Kind:        2,
			Title:       m.Title,
			URL:         link,
			CoverURL:    normalizeCover(m.Pic),
			Duration:    m.Duration,
			AuthorID:    strconv.FormatInt(m.Owner.Mid, 10),
			AuthorName:  m.Owner.Name,
			PublishedAt: m.Pubdate,
			FavoritedAt: m.AddTime,
		})
	}
	return out, nil
}

// SpaceVideosPage lists an UP主's public video submissions.
func (c *Client) SpaceVideosPage(ctx context.Context, mid string, pn, ps int) (*Page, error) {
	if pn < 1 {
		pn = 1
	}
	if ps < 1 || ps > 50 {
		ps = 20
	}
	var d struct {
		List struct {
			VList []struct {
				AID      int64  `json:"aid"`
				BVID     string `json:"bvid"`
				Title    string `json:"title"`
				Pic      string `json:"pic"`
				Desc     string `json:"description"`
				Duration int    `json:"duration"`
				Pubdate  int64  `json:"pubdate"`
				Author   string `json:"author"`
				Mid      int64  `json:"mid"`
			} `json:"vlist"`
		} `json:"list"`
		Page struct {
			Count int `json:"count"`
			Pn    int `json:"pn"`
			Ps    int `json:"ps"`
		} `json:"page"`
	}
	params := url.Values{
		"mid":          {mid},
		"pn":           {strconv.Itoa(pn)},
		"ps":           {strconv.Itoa(ps)},
		"order":        {"pubdate"},
		"platform":     {"web"},
		"web_location": {"1550101"},
	}
	if err := c.apiGet(ctx, "/x/space/wbi/arc/search", params, true, &d); err != nil {
		return nil, err
	}
	out := &Page{HasMore: pn*ps < d.Page.Count}
	for _, m := range d.List.VList {
		bvid := m.BVID
		out.Items = append(out.Items, Media{
			RemoteID:    remoteID(bvid, m.AID),
			BVID:        bvid,
			Kind:        2,
			Title:       m.Title,
			URL:         videoURL(bvid, m.AID),
			CoverURL:    normalizeCover(m.Pic),
			Duration:    m.Duration,
			AuthorID:    strconv.FormatInt(m.Mid, 10),
			AuthorName:  m.Author,
			Summary:     m.Desc,
			PublishedAt: m.Pubdate,
		})
	}
	return out, nil
}

// SeasonArchivesPage lists a collection (season) owned by an UP主.
func (c *Client) SeasonArchivesPage(ctx context.Context, mid, seasonID string, pn, ps int) (*Page, error) {
	if pn < 1 {
		pn = 1
	}
	if ps < 1 || ps > 30 {
		ps = 20
	}
	var d struct {
		Archives []struct {
			BVID     string `json:"bvid"`
			AID      int64  `json:"aid"`
			Title    string `json:"title"`
			Pic      string `json:"pic"`
			Duration int    `json:"duration"`
			Pubdate  int64  `json:"pubdate"`
			Stat     struct {
				View int64 `json:"view"`
			} `json:"stat"`
		} `json:"archives"`
		Page struct {
			Total int `json:"total"`
		} `json:"page"`
	}
	params := url.Values{
		"mid":          {mid},
		"season_id":    {seasonID},
		"page_num":     {strconv.Itoa(pn)},
		"page_size":    {strconv.Itoa(ps)},
		"sort_reverse": {"false"},
		"web_location": {"333.999"},
	}
	if err := c.apiGet(ctx, "/x/polymer/web-space/seasons_archives_list", params, false, &d); err != nil {
		return nil, err
	}
	out := &Page{HasMore: pn*ps < d.Page.Total}
	for _, m := range d.Archives {
		out.Items = append(out.Items, Media{
			RemoteID:    remoteID(m.BVID, m.AID),
			BVID:        m.BVID,
			Kind:        2,
			Title:       m.Title,
			URL:         videoURL(m.BVID, m.AID),
			CoverURL:    normalizeCover(m.Pic),
			Duration:    m.Duration,
			PublishedAt: m.Pubdate,
		})
	}
	return out, nil
}

// SeriesArchivesPage lists a series owned by an UP主.
func (c *Client) SeriesArchivesPage(ctx context.Context, mid, seriesID string, pn, ps int) (*Page, error) {
	if pn < 1 {
		pn = 1
	}
	if ps < 1 || ps > 30 {
		ps = 20
	}
	var d struct {
		Archives []struct {
			BVID     string `json:"bvid"`
			AID      int64  `json:"aid"`
			Title    string `json:"title"`
			Pic      string `json:"pic"`
			Duration int    `json:"duration"`
			Pubdate  int64  `json:"pubdate"`
		} `json:"archives"`
		Page struct {
			Total int `json:"total"`
			Pn    int `json:"pn"`
			Ps    int `json:"ps"`
		} `json:"page"`
	}
	params := url.Values{
		"mid":         {mid},
		"series_id":   {seriesID},
		"pn":          {strconv.Itoa(pn)},
		"ps":          {strconv.Itoa(ps)},
		"only_normal": {"true"},
		"sort":        {"desc"},
	}
	if err := c.apiGet(ctx, "/x/series/archives", params, false, &d); err != nil {
		return nil, err
	}
	out := &Page{HasMore: pn*ps < d.Page.Total}
	for _, m := range d.Archives {
		out.Items = append(out.Items, Media{
			RemoteID:    remoteID(m.BVID, m.AID),
			BVID:        m.BVID,
			Kind:        2,
			Title:       m.Title,
			URL:         videoURL(m.BVID, m.AID),
			CoverURL:    normalizeCover(m.Pic),
			Duration:    m.Duration,
			PublishedAt: m.Pubdate,
		})
	}
	return out, nil
}

func remoteID(bvid string, aid int64) string {
	if bvid != "" {
		return bvid
	}
	if aid != 0 {
		return "av" + strconv.FormatInt(aid, 10)
	}
	return ""
}

func videoURL(bvid string, aid int64) string {
	if bvid != "" {
		return "https://www.bilibili.com/video/" + bvid
	}
	if aid != 0 {
		return fmt.Sprintf("https://www.bilibili.com/video/av%d", aid)
	}
	return ""
}

func normalizeCover(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}

// SourceKindFromRemoteID is used by the adapter to route pages to the right API.
func SourceKindFromRemoteID(remoteID string) string {
	switch {
	case remoteID == "watch_later":
		return "watch_later"
	case strings.HasPrefix(remoteID, "submission:"):
		return "submission"
	case strings.HasPrefix(remoteID, "season:"):
		return "season"
	case strings.HasPrefix(remoteID, "series:"):
		return "series"
	default:
		return "favorite"
	}
}
