//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bilibili

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"magpiesync/internal/sites"
)

var listURLPattern = regexp.MustCompile(`space\.bilibili\.com/(\d+)/lists/(\d+)`)

// parseSourceRef understands the three shapes the bilibili form accepts: a
// space URL, a compact "mid:id" pair, or a bare numeric mid.
func parseSourceRef(raw string) (mid, id string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ""
	}
	if m := listURLPattern.FindStringSubmatch(value); m != nil {
		return m[1], m[2]
	}
	if pair := strings.SplitN(value, ":", 2); len(pair) == 2 {
		return strings.TrimSpace(pair[0]), strings.TrimSpace(pair[1])
	}
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return value, ""
	}
	return "", ""
}

func (driver) BuildSource(req sites.SourceRequest) (sites.SourceSpec, error) {
	title := strings.TrimSpace(req.Title)
	switch req.Kind {
	case "watch_later":
		if title == "" {
			title = "稍后再看"
		}
		return sites.SourceSpec{OwnerID: req.OwnerID, RemoteID: "watch_later", Title: title}, nil
	case "submission":
		mid := strings.TrimSpace(req.OwnerID)
		if mid == "" {
			mid, _ = parseSourceRef(req.Ref)
		}
		if mid == "" {
			return sites.SourceSpec{}, errors.New("需要 UP 主 mid 或空间链接")
		}
		if title == "" {
			title = "自行投稿 " + mid
		}
		return sites.SourceSpec{OwnerID: mid, RemoteID: "submission:" + mid, Title: title}, nil
	case "season", "series":
		mid := strings.TrimSpace(req.OwnerID)
		id := strings.TrimSpace(req.ID)
		if mid == "" || id == "" {
			parsedMid, parsedID := parseSourceRef(req.Ref)
			if mid == "" {
				mid = parsedMid
			}
			if id == "" {
				id = parsedID
			}
		}
		if mid == "" || id == "" {
			return sites.SourceSpec{}, errors.New("需要 UP 主 mid 和合集/系列 id")
		}
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			return sites.SourceSpec{}, errors.New("合集/系列 id 必须是数字")
		}
		if title == "" {
			title = req.Kind + " " + id
		}
		return sites.SourceSpec{OwnerID: mid, RemoteID: req.Kind + ":" + mid + ":" + id, Title: title}, nil
	default:
		return sites.SourceSpec{}, errors.New("不支持的来源类型")
	}
}
