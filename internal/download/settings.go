package download

import (
	"context"
	"os/exec"
	"strconv"
	"strings"

	"magpiesync/internal/sites"
	"magpiesync/internal/store"
)

// ArchiveOptions are the optional sidecar files a site can write next to a
// video. NFO and cover are available on every downloadable site; the remaining
// fields only take effect on sites that declare the matching capability in
// sites.Site.ArchiveExtras.
type ArchiveOptions struct {
	NFO         bool `json:"nfo"`
	Cover       bool `json:"cover"`
	Danmaku     bool `json:"danmaku"`
	Subtitles   bool `json:"subtitles"`
	AISubtitles bool `json:"subtitles_ai"`
}

type Settings struct {
	MediaDir         string
	FFmpegPath       string
	SiteQualities    map[string]int
	MissingCheck     map[string]bool
	Archive          map[string]ArchiveOptions
	Naming           map[string]Naming
	Codecs           []string
	Concurrency      int
	ChunkConcurrency int
	ChunkThreshold   int64
}

// Naming holds one site's file naming templates.
type Naming struct {
	Video string `json:"video_template"`
	Page  string `json:"page_template"`
}

func DefaultSettings() Settings {
	return Settings{
		MediaDir:         "./media",
		FFmpegPath:       "ffmpeg",
		SiteQualities:    map[string]int{},
		MissingCheck:     map[string]bool{},
		Archive:          map[string]ArchiveOptions{},
		Naming:           map[string]Naming{},
		Codecs:           []string{"avc", "hevc", "av1"},
		Concurrency:      2,
		ChunkConcurrency: 4,
		ChunkThreshold:   20 << 20,
	}
}

func LoadSettings(ctx context.Context, s *store.Store) (Settings, error) {
	out := DefaultSettings()
	values, err := s.Settings(ctx)
	if err != nil {
		return out, err
	}
	if v := values["download.media_dir"]; v != "" {
		out.MediaDir = v
	}
	if v := values["download.ffmpeg"]; v != "" {
		out.FFmpegPath = v
	}
	if v := values["download.codecs"]; v != "" {
		out.Codecs = splitCSV(v)
	}
	if v := values["download.concurrency"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 8 {
			out.Concurrency = n
		}
	}
	// Naming templates are declared by each plugin and may be overridden per site.
	for _, site := range sites.List() {
		if !site.Download {
			continue
		}
		n := Naming{}
		if site.Naming != nil {
			n = Naming{Video: site.Naming.VideoTemplate, Page: site.Naming.PageTemplate}
		}
		out.Naming[site.ID] = n
	}
	for key, value := range values {
		switch {
		case strings.HasPrefix(key, "download.video_template."):
			siteID := strings.TrimPrefix(key, "download.video_template.")
			n := out.Naming[siteID]
			n.Video = value
			out.Naming[siteID] = n
		case strings.HasPrefix(key, "download.page_template."):
			siteID := strings.TrimPrefix(key, "download.page_template.")
			n := out.Naming[siteID]
			n.Page = value
			out.Naming[siteID] = n
		}
	}
	// Shared sidecars default on; optional formats default on only when declared.
	for _, site := range sites.List() {
		if !site.Download {
			continue
		}
		opts := ArchiveOptions{NFO: true, Cover: true}
		for _, extra := range site.ArchiveExtras {
			switch extra {
			case "danmaku":
				opts.Danmaku = true
			case "subtitles":
				opts.Subtitles = true
			case "subtitles_ai":
				opts.AISubtitles = true
			}
		}
		out.Archive[site.ID] = opts
	}
	for key, value := range values {
		switch {
		case strings.HasPrefix(key, "download.nfo."):
			siteID := strings.TrimPrefix(key, "download.nfo.")
			opts := out.Archive[siteID]
			opts.NFO = parseBool(value)
			out.Archive[siteID] = opts
		case strings.HasPrefix(key, "download.cover."):
			siteID := strings.TrimPrefix(key, "download.cover.")
			opts := out.Archive[siteID]
			opts.Cover = parseBool(value)
			out.Archive[siteID] = opts
		case strings.HasPrefix(key, "download.danmaku."):
			siteID := strings.TrimPrefix(key, "download.danmaku.")
			opts := out.Archive[siteID]
			opts.Danmaku = parseBool(value)
			out.Archive[siteID] = opts
		case strings.HasPrefix(key, "download.subtitles."):
			siteID := strings.TrimPrefix(key, "download.subtitles.")
			opts := out.Archive[siteID]
			opts.Subtitles = parseBool(value)
			out.Archive[siteID] = opts
		case strings.HasPrefix(key, "download.subtitle_ai."):
			siteID := strings.TrimPrefix(key, "download.subtitle_ai.")
			opts := out.Archive[siteID]
			opts.AISubtitles = parseBool(value)
			out.Archive[siteID] = opts
		}
	}
	if v := values["download.chunk_concurrency"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 8 {
			out.ChunkConcurrency = n
		}
	}
	if v := values["download.chunk_threshold_mb"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			out.ChunkThreshold = int64(n) << 20
		}
	}
	// Quality is configured per site and defaults to automatic selection.
	for key, value := range values {
		const prefix = "download.quality."
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		siteID := strings.TrimPrefix(key, prefix)
		if siteID == "" {
			continue
		}
		if n, err := strconv.Atoi(value); err == nil && n >= 0 {
			out.SiteQualities[siteID] = n
		}
	}
	// The missing-file check is opt-in per site and defaults to off.
	for key, value := range values {
		const prefix = "download.check_missing."
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		siteID := strings.TrimPrefix(key, prefix)
		if siteID == "" {
			continue
		}
		out.MissingCheck[siteID] = value == "1" || strings.EqualFold(value, "true")
	}
	for _, site := range sites.List() {
		if !site.Download {
			continue
		}
		if _, ok := out.SiteQualities[site.ID]; !ok {
			out.SiteQualities[site.ID] = 0
		}
	}
	return out, nil
}

// MissingCheckFor reports whether the missing-file check is enabled for one
// site. LoadSettings never populates it by default, so this returns false for
// any site the developer has not explicitly turned on.
func (s Settings) MissingCheckFor(siteID string) bool {
	return s.MissingCheck[siteID]
}

// ArchiveFor returns the sidecar options for one site. Sites that do not
// declare an extra in sites.Site.ArchiveExtras simply never write it, so the
// zero value is the disabled set.
func (s Settings) ArchiveFor(siteID string) ArchiveOptions {
	return s.Archive[siteID]
}

// NamingFor returns the naming templates for one site, falling back to that
// site's registered defaults when nothing has been configured.
func (s Settings) NamingFor(siteID string) Naming {
	if n, ok := s.Naming[siteID]; ok {
		return n
	}
	if site, ok := sites.Get(siteID); ok && site.Naming != nil {
		return Naming{Video: site.Naming.VideoTemplate, Page: site.Naming.PageTemplate}
	}
	return Naming{}
}

// QualityFor returns the configured quality for one site. Zero means automatic.
func (s Settings) QualityFor(siteID string) int {
	return s.SiteQualities[siteID]
}

// NormalizeQualityForSite retains an explicitly offered quality or clamps a
// requested pixel height to the closest option the site declares.
func NormalizeQualityForSite(site sites.Site, quality int) int {
	if quality <= 0 {
		return 0
	}
	if len(site.DownloadQualities) == 0 {
		return quality
	}
	for _, q := range site.DownloadQualities {
		if q.Value == quality {
			return quality
		}
	}
	want := quality
	best := 0
	lowest := 0
	for _, q := range site.DownloadQualities {
		if lowest == 0 || q.Value < lowest {
			lowest = q.Value
		}
		if q.Value <= want && q.Value > best {
			best = q.Value
		}
	}
	if best > 0 {
		return best
	}
	return lowest
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"avc", "hevc", "av1"}
	}
	return out
}

func (s Settings) Values() map[string]string {
	out := map[string]string{
		"download.media_dir":          s.MediaDir,
		"download.ffmpeg":             s.FFmpegPath,
		"download.codecs":             strings.Join(s.Codecs, ","),
		"download.concurrency":        strconv.Itoa(s.Concurrency),
		"download.chunk_concurrency":  strconv.Itoa(s.ChunkConcurrency),
		"download.chunk_threshold_mb": strconv.FormatInt(s.ChunkThreshold>>20, 10),
	}
	for siteID, opts := range s.Archive {
		if strings.TrimSpace(siteID) == "" {
			continue
		}
		out["download.nfo."+siteID] = boolString(opts.NFO)
		out["download.cover."+siteID] = boolString(opts.Cover)
		site, ok := sites.Get(siteID)
		if !ok || len(site.ArchiveExtras) == 0 {
			continue
		}
		for _, extra := range site.ArchiveExtras {
			switch extra {
			case "danmaku":
				out["download.danmaku."+siteID] = boolString(opts.Danmaku)
			case "subtitles":
				out["download.subtitles."+siteID] = boolString(opts.Subtitles)
			case "subtitles_ai":
				out["download.subtitle_ai."+siteID] = boolString(opts.AISubtitles)
			}
		}
	}
	for siteID, naming := range s.Naming {
		if strings.TrimSpace(siteID) == "" {
			continue
		}
		out["download.video_template."+siteID] = naming.Video
		out["download.page_template."+siteID] = naming.Page
	}
	for siteID, quality := range s.SiteQualities {
		if strings.TrimSpace(siteID) == "" {
			continue
		}
		out["download.quality."+siteID] = strconv.Itoa(quality)
	}
	for siteID, enabled := range s.MissingCheck {
		if strings.TrimSpace(siteID) == "" {
			continue
		}
		out["download.check_missing."+siteID] = boolString(enabled)
	}
	return out
}

func boolString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func parseBool(v string) bool {
	return v == "1" || strings.EqualFold(v, "true")
}

func FFmpegAvailable(path string) bool {
	if strings.TrimSpace(path) == "" {
		path = "ffmpeg"
	}
	_, err := exec.LookPath(path)
	return err == nil
}
