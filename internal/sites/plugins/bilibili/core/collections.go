//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"context"
	"net/url"
	"strconv"
)

// Collection is a season (合集) or series (系列) owned by an UP主.
type Collection struct {
	Kind     string
	ID       string
	Mid      string
	Title    string
	CoverURL string
	Count    int
}

// Collections discovers public seasons and series for an UP主.
func (c *Client) Collections(ctx context.Context, mid string) ([]Collection, error) {
	out := make([]Collection, 0)
	seen := make(map[string]bool)
	for page := 1; page <= 10; page++ {
		items, err := c.collectionsPage(ctx, mid, page)
		if err != nil {
			return nil, err
		}
		added := 0
		for _, item := range items {
			key := item.Kind + ":" + item.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, item)
			added++
		}
		if added == 0 {
			break
		}
	}
	return out, nil
}

func (c *Client) collectionsPage(ctx context.Context, mid string, page int) ([]Collection, error) {
	params := url.Values{
		"mid":          {mid},
		"page_num":     {strconv.Itoa(page)},
		"page_size":    {"20"},
		"web_location": {"333.1387"},
	}
	var d struct {
		ItemsLists struct {
			Seasons []struct {
				Meta struct {
					SeasonID int64  `json:"season_id"`
					Title    string `json:"title"`
					Cover    string `json:"cover"`
					Total    int    `json:"total"`
				} `json:"meta"`
				Archives []struct {
					BVID string `json:"bvid"`
				} `json:"archives"`
			} `json:"seasons_list"`
			Series []struct {
				Meta struct {
					SeriesID int64  `json:"series_id"`
					Title    string `json:"title"`
					Name     string `json:"name"`
					Cover    string `json:"cover"`
					Total    int    `json:"total"`
				} `json:"meta"`
				Archives []struct {
					BVID string `json:"bvid"`
				} `json:"archives"`
			} `json:"series_list"`
		} `json:"items_lists"`
	}
	if err := c.apiGet(ctx, "/x/polymer/web-space/seasons_series_list", params, false, &d); err != nil {
		return nil, err
	}
	out := make([]Collection, 0, len(d.ItemsLists.Seasons)+len(d.ItemsLists.Series))
	for _, s := range d.ItemsLists.Seasons {
		if s.Meta.SeasonID == 0 {
			continue
		}
		count := s.Meta.Total
		if count == 0 {
			count = len(s.Archives)
		}
		out = append(out, Collection{
			Kind:     "season",
			ID:       strconv.FormatInt(s.Meta.SeasonID, 10),
			Mid:      mid,
			Title:    firstNonEmpty(s.Meta.Title, "合集 "+strconv.FormatInt(s.Meta.SeasonID, 10)),
			CoverURL: normalizeCover(s.Meta.Cover),
			Count:    count,
		})
	}
	for _, s := range d.ItemsLists.Series {
		if s.Meta.SeriesID == 0 {
			continue
		}
		count := s.Meta.Total
		if count == 0 {
			count = len(s.Archives)
		}
		out = append(out, Collection{
			Kind:     "series",
			ID:       strconv.FormatInt(s.Meta.SeriesID, 10),
			Mid:      mid,
			Title:    firstNonEmpty(s.Meta.Title, s.Meta.Name, "系列 "+strconv.FormatInt(s.Meta.SeriesID, 10)),
			CoverURL: normalizeCover(s.Meta.Cover),
			Count:    count,
		})
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
