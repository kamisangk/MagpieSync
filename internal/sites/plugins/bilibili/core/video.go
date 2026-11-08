//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"magpiesync/internal/media"
)

type VideoPage = media.VideoPage
type VideoDetail = media.VideoDetail
type Stream = media.Stream
type PlayInfo = media.PlayInfo

type playStreamJSON struct {
	ID         int      `json:"id"`
	BaseURL    string   `json:"baseUrl"`
	BaseURL2   string   `json:"base_url"`
	BackupURL  []string `json:"backupUrl"`
	BackupURL2 []string `json:"backup_url"`
	Bandwidth  int      `json:"bandwidth"`
	CodeCID    int      `json:"codecid"`
	Width      int      `json:"width"`
	Height     int      `json:"height"`
	MimeType   string   `json:"mimeType"`
}

func (s playStreamJSON) stream() Stream {
	base := s.BaseURL
	if base == "" {
		base = s.BaseURL2
	}
	backups := s.BackupURL
	if len(backups) == 0 {
		backups = s.BackupURL2
	}
	return Stream{
		URL:        base,
		BackupURLs: backups,
		QualityID:  s.ID,
		CodecID:    s.CodeCID,
		Width:      s.Width,
		Height:     s.Height,
		Bandwidth:  s.Bandwidth,
		MimeType:   s.MimeType,
	}
}

func (c *Client) VideoDetail(ctx context.Context, bvid string) (*VideoDetail, error) {
	var d struct {
		BVID    string `json:"bvid"`
		AID     int64  `json:"aid"`
		Title   string `json:"title"`
		Pic     string `json:"pic"`
		Desc    string `json:"desc"`
		PubDate int64  `json:"pubdate"`
		Owner   struct {
			Mid  int64  `json:"mid"`
			Name string `json:"name"`
		} `json:"owner"`
		Pages []struct {
			CID       int64  `json:"cid"`
			Page      int    `json:"page"`
			Part      string `json:"part"`
			Duration  int    `json:"duration"`
			Dimension struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			} `json:"dimension"`
		} `json:"pages"`
	}
	if err := c.apiGet(ctx, "/x/web-interface/view", videoIdentityParams(bvid), false, &d); err != nil {
		return nil, err
	}
	if d.BVID == "" || len(d.Pages) == 0 {
		return nil, fmt.Errorf("bilibili: empty video detail for %s", bvid)
	}
	out := &VideoDetail{
		RemoteID:    d.BVID,
		Title:       d.Title,
		CoverURL:    d.Pic,
		Summary:     d.Desc,
		PublishedAt: d.PubDate,
		AuthorID:    strconv.FormatInt(d.Owner.Mid, 10),
		AuthorName:  d.Owner.Name,
	}
	for _, p := range d.Pages {
		out.Pages = append(out.Pages, VideoPage{
			PartID:   p.CID,
			Page:     p.Page,
			Title:    p.Part,
			Duration: p.Duration,
			Width:    p.Dimension.Width,
			Height:   p.Dimension.Height,
		})
	}
	return out, nil
}

// VideoTags lists public tags for a video. It is best-effort metadata for NFO.
func (c *Client) VideoTags(ctx context.Context, bvid string) ([]string, error) {
	var d []struct {
		TagName string `json:"tag_name"`
	}
	if err := c.apiGet(ctx, "/x/web-interface/view/detail/tag", videoIdentityParams(bvid), false, &d); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(d))
	for _, t := range d {
		if t.TagName != "" {
			out = append(out, t.TagName)
		}
	}
	return out, nil
}

// PlayURL fetches a DASH/progressive play response. qn=0 asks for the best
// available quality; the caller can pick a lower stream from the result.
func (c *Client) PlayURL(ctx context.Context, bvid string, cid int64, qn int) (*PlayInfo, error) {
	if qn <= 0 {
		qn = 127
	}
	params := url.Values{
		"cid":          {strconv.FormatInt(cid, 10)},
		"qn":           {strconv.Itoa(qn)},
		"fnval":        {"4048"},
		"fnver":        {"0"},
		"fourk":        {"1"},
		"platform":     {"pc"},
		"high_quality": {"1"},
	}
	for key, value := range videoIdentityParams(bvid) {
		params[key] = value
	}
	var d struct {
		Quality int `json:"quality"`
		Dash    struct {
			Duration int              `json:"duration"`
			Video    []playStreamJSON `json:"video"`
			Audio    []playStreamJSON `json:"audio"`
		} `json:"dash"`
		DURL []struct {
			URL       string   `json:"url"`
			BackupURL []string `json:"backup_url"`
		} `json:"durl"`
		Timelength int `json:"timelength"`
	}
	if err := c.apiGet(ctx, "/x/player/wbi/playurl", params, true, &d); err != nil {
		return nil, err
	}
	out := &PlayInfo{Quality: d.Quality, Duration: d.Dash.Duration / 1000, Progressive: len(d.Dash.Video) == 0 && len(d.DURL) > 0}
	if out.Duration == 0 && d.Timelength > 0 {
		out.Duration = d.Timelength / 1000
	}
	for _, v := range d.Dash.Video {
		out.Video = append(out.Video, v.stream())
	}
	for _, a := range d.Dash.Audio {
		out.Audio = append(out.Audio, a.stream())
	}
	for _, u := range d.DURL {
		out.Video = append(out.Video, Stream{URL: u.URL, BackupURLs: u.BackupURL})
	}
	if len(out.Video) == 0 {
		return nil, fmt.Errorf("bilibili: no playable stream for %s (cid %d)", bvid, cid)
	}
	return out, nil
}

// Favorites store numeric archive IDs; other sources may supply BV IDs.
func videoIdentityParams(remoteID string) url.Values {
	remoteID = strings.TrimSpace(remoteID)
	aid := remoteID
	if strings.HasPrefix(strings.ToLower(aid), "av") {
		aid = aid[2:]
	}
	if id, err := strconv.ParseInt(aid, 10, 64); err == nil && id > 0 {
		return url.Values{"aid": {strconv.FormatInt(id, 10)}}
	}
	return url.Values{"bvid": {remoteID}}
}
