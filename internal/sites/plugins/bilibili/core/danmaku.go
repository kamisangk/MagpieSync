//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"magpiesync/internal/media"
)

type Danmaku = media.Danmaku

// DanmakuASS is kept for callers of the bili package. New code should use
// media.DanmakuASS.
func DanmakuASS(items []Danmaku, width, height int) []byte {
	return media.DanmakuASS(items, width, height)
}

// DanmakuSegments fetches one protobuf danmaku segment (360 seconds).
func (c *Client) DanmakuSegment(ctx context.Context, cid int64, segment int) ([]Danmaku, error) {
	if segment < 1 {
		segment = 1
	}
	params := url.Values{
		"type":          {"1"},
		"oid":           {strconv.FormatInt(cid, 10)},
		"segment_index": {strconv.Itoa(segment)},
	}
	body, contentType, err := c.rawGet(ctx, "/x/v2/dm/wbi/web/seg.so", params, true)
	if err != nil {
		return nil, err
	}
	if apiErr := decodeAPIError(body); apiErr != nil {
		return nil, apiErr
	}
	if strings.Contains(strings.ToLower(contentType), "json") {
		return nil, fmt.Errorf("bilibili: unexpected danmaku response: %s", truncateBody(body))
	}
	return decodeDanmakuSegment(body)
}

// Danmaku fetches all segments for one page. Segments are fetched serially to
// stay within the client's shared rate limit.
func (c *Client) Danmaku(ctx context.Context, cid int64, duration int) ([]Danmaku, error) {
	if duration < 1 {
		duration = 1
	}
	segments := (duration + 359) / 360
	out := make([]Danmaku, 0, segments*80)
	for i := 1; i <= segments; i++ {
		part, err := c.DanmakuSegment(ctx, cid, i)
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Progress < out[j].Progress
	})
	return out, nil
}

func truncateBody(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

func decodeDanmakuSegment(data []byte) ([]Danmaku, error) {
	var out []Danmaku
	for len(data) > 0 {
		key, n := readVarint(data)
		if n <= 0 {
			return out, fmt.Errorf("bilibili: invalid danmaku protobuf")
		}
		data = data[n:]
		field, wire := key>>3, key&7
		switch wire {
		case 0:
			_, n = readVarint(data)
			if n <= 0 {
				return out, fmt.Errorf("bilibili: invalid danmaku varint")
			}
			data = data[n:]
		case 1:
			if len(data) < 8 {
				return out, fmt.Errorf("bilibili: truncated danmaku protobuf")
			}
			data = data[8:]
		case 2:
			size, m := readVarint(data)
			if m <= 0 || int64(size) > int64(len(data)-m) {
				return out, fmt.Errorf("bilibili: truncated danmaku field")
			}
			data = data[m:]
			chunk := data[:size]
			data = data[size:]
			if field == 1 {
				d, err := decodeDanmakuElem(chunk)
				if err != nil {
					return out, err
				}
				out = append(out, d)
			}
		case 5:
			if len(data) < 4 {
				return out, fmt.Errorf("bilibili: truncated danmaku protobuf")
			}
			data = data[4:]
		default:
			return out, fmt.Errorf("bilibili: unsupported danmaku wire type %d", wire)
		}
	}
	return out, nil
}

func decodeDanmakuElem(data []byte) (Danmaku, error) {
	var d Danmaku
	for len(data) > 0 {
		key, n := readVarint(data)
		if n <= 0 {
			return d, fmt.Errorf("bilibili: invalid danmaku element")
		}
		data = data[n:]
		field, wire := key>>3, key&7
		switch wire {
		case 0:
			v, m := readVarint(data)
			if m <= 0 {
				return d, fmt.Errorf("bilibili: invalid danmaku element varint")
			}
			data = data[m:]
			switch field {
			case 1:
				d.ID = int64(v)
			case 2:
				d.Progress = int64(v)
			case 3:
				d.Mode = int(v)
			case 4:
				d.FontSize = int(v)
			case 5:
				d.Color = uint32(v)
			}
		case 1:
			if len(data) < 8 {
				return d, fmt.Errorf("bilibili: truncated danmaku element")
			}
			data = data[8:]
		case 2:
			size, m := readVarint(data)
			if m <= 0 || int64(size) > int64(len(data)-m) {
				return d, fmt.Errorf("bilibili: truncated danmaku element string")
			}
			data = data[m:]
			chunk := data[:size]
			data = data[size:]
			if field == 7 {
				d.Content = string(chunk)
			}
		case 5:
			if len(data) < 4 {
				return d, fmt.Errorf("bilibili: truncated danmaku element")
			}
			data = data[4:]
		default:
			return d, fmt.Errorf("bilibili: unsupported danmaku element wire type %d", wire)
		}
	}
	return d, nil
}

func readVarint(data []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(data) && i < 10; i++ {
		b := data[i]
		v |= uint64(b&0x7f) << (7 * uint(i))
		if b < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}
