//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"magpiesync/internal/media"
)

type Subtitle = media.Subtitle
type SubtitleItem = media.SubtitleItem

// SubtitleSRT is kept for callers of the bili package. New code should use
// media.SubtitleSRT.
func SubtitleSRT(items []SubtitleItem) []byte {
	return media.SubtitleSRT(items)
}

// Subtitles lists manual subtitles for a page. AI-generated subtitles are
// marked so callers can choose whether to fetch them.
func (c *Client) Subtitles(ctx context.Context, bvid string, cid int64) ([]Subtitle, error) {
	params := videoIdentityParams(bvid)
	params.Set("cid", strconv.FormatInt(cid, 10))
	var d struct {
		Subtitle *struct {
			Subtitles []struct {
				Lan         string `json:"lan"`
				LanDoc      string `json:"lan_doc"`
				SubtitleURL string `json:"subtitle_url"`
			} `json:"subtitles"`
		} `json:"subtitle"`
	}
	if err := c.apiGet(ctx, "/x/player/wbi/v2", params, true, &d); err != nil {
		return nil, err
	}
	if d.Subtitle == nil {
		return nil, nil
	}
	out := make([]Subtitle, 0, len(d.Subtitle.Subtitles))
	for _, s := range d.Subtitle.Subtitles {
		raw := strings.TrimSpace(s.SubtitleURL)
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "//") {
			raw = "https:" + raw
		}
		out = append(out, Subtitle{
			Lan:    s.Lan,
			LanDoc: s.LanDoc,
			URL:    raw,
			AI:     strings.Contains(raw, "ai_subtitle"),
		})
	}
	return out, nil
}

// SubtitleBody downloads and decodes one subtitle JSON document.
func (c *Client) SubtitleBody(ctx context.Context, rawURL string) ([]SubtitleItem, error) {
	body, contentType, err := c.rawGetURL(ctx, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(strings.ToLower(contentType), "json") {
		return nil, fmt.Errorf("bilibili: unexpected subtitle content type %q", contentType)
	}
	var d struct {
		Body []SubtitleItem `json:"body"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("decode subtitle: %w", err)
	}
	return d.Body, nil
}
