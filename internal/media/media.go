// Package media contains site-neutral playable stream metadata and sidecar
// renderers shared by adapters and the download manager.
package media

import "sort"

type VideoPage struct {
	PartID   int64
	Page     int
	Title    string
	Duration int
	Width    int
	Height   int
}

type VideoDetail struct {
	RemoteID    string
	Title       string
	CoverURL    string
	Summary     string
	PublishedAt int64
	Duration    int
	AuthorID    string
	AuthorName  string
	Pages       []VideoPage
}

type Stream struct {
	URL        string
	BackupURLs []string
	QualityID  int
	CodecID    int
	Width      int
	Height     int
	Bandwidth  int
	MimeType   string
}

type PlayInfo struct {
	Quality     int
	Duration    int
	Progressive bool
	Video       []Stream
	Audio       []Stream
}

// SelectStreams picks the best video stream at or below the requested quality
// and the best audio stream. Empty codecPriority means any codec.
func (p *PlayInfo) SelectStreams(requestedQuality int, codecPriority []string) (video *Stream, audio *Stream, quality int) {
	candidates := make([]Stream, 0, len(p.Video))
	for _, s := range p.Video {
		if s.URL == "" {
			continue
		}
		if requestedQuality > 0 && s.QualityID > requestedQuality {
			continue
		}
		candidates = append(candidates, s)
	}
	if len(candidates) == 0 {
		for _, s := range p.Video {
			if s.URL != "" {
				candidates = append(candidates, s)
			}
		}
	}
	if len(candidates) == 0 {
		return nil, nil, 0
	}
	rank := func(codecID int) int {
		name := codecName(codecID)
		for i, v := range codecPriority {
			if v == name {
				return i
			}
		}
		return len(codecPriority)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].QualityID != candidates[j].QualityID {
			return candidates[i].QualityID > candidates[j].QualityID
		}
		ri, rj := rank(candidates[i].CodecID), rank(candidates[j].CodecID)
		if ri != rj {
			return ri < rj
		}
		return candidates[i].Bandwidth > candidates[j].Bandwidth
	})
	v := candidates[0]
	quality = v.QualityID
	if len(p.Audio) > 0 {
		best := p.Audio[0]
		for _, a := range p.Audio[1:] {
			if a.Bandwidth > best.Bandwidth {
				best = a
			}
		}
		if best.URL != "" {
			audio = &best
		}
	}
	return &v, audio, quality
}

func codecName(id int) string {
	switch id {
	case 7:
		return "avc"
	case 12:
		return "hevc"
	case 13:
		return "av1"
	default:
		return "unknown"
	}
}

// Danmaku is one bullet comment. Progress is milliseconds from the start of
// the page; Mode follows a scrolling/bottom/top convention (1-3 scrolling, 4 bottom,
// 5 top, 6 reverse, 7 advanced, 8 code, 9 BAS).
type Danmaku struct {
	ID       int64
	Progress int64
	Mode     int
	FontSize int
	Color    uint32
	Content  string
}

type Subtitle struct {
	Lan    string
	LanDoc string
	URL    string
	AI     bool
}

type SubtitleItem struct {
	From    float64 `json:"from"`
	To      float64 `json:"to"`
	Content string  `json:"content"`
}
