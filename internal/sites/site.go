// Package sites is the compile-time video site catalog. A site adapter is an
// implementation detail; this package only describes which sites are built in.
package sites

import (
	"io/fs"
	"sort"
	"sync"
)

type Category struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Kinds []string `json:"kinds,omitempty"`
}

type DownloadQuality struct {
	Value int    `json:"value"`
	Label string `json:"label"`
}

type Site struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Homepage          string            `json:"homepage,omitempty"`
	Logo              string            `json:"logo,omitempty"`
	Categories        []Category        `json:"categories"`
	Ready             bool              `json:"ready"`
	Download          bool              `json:"download"`
	DownloadQualities []DownloadQuality `json:"download_qualities,omitempty"`
	// ArchiveExtras lists the optional sidecars this site can produce:
	// "danmaku", "subtitles" and "subtitles_ai".
	ArchiveExtras []string `json:"archive_extras,omitempty"`
	// Naming carries this site's default file naming templates and the
	// placeholders it actually supports.
	Naming *SiteNaming `json:"naming,omitempty"`
	// Login describes the login UI this site wants. The web client renders
	// whatever the site declares here; no site ids are known to the client.
	Login LoginConfig `json:"login,omitempty"`
	// Source lists the source kinds a user may add manually for this site.
	Source SourceConfig `json:"source,omitempty"`
	// DownloadInfo carries the sidecar details the shared download manager
	// needs to render files for this site.
	DownloadInfo DownloadConfig `json:"download_info,omitempty"`
}

// LoginConfig describes the login panel: which methods exist, what they say,
// and what the cookie/password/QR forms look like.
type LoginConfig struct {
	Hint     string          `json:"hint,omitempty"`
	Methods  []LoginMethod   `json:"methods,omitempty"`
	QR       *QRConfig       `json:"qr,omitempty"`
	Cookie   *CookieConfig   `json:"cookie,omitempty"`
	Password *PasswordConfig `json:"password,omitempty"`
}

// LoginMethod is one selectable login tab. ID must be "qrcode", "password" or
// "cookie"; the shared client knows how to drive those three flows.
type LoginMethod struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// QRConfig describes the site-specific wording of the QR panel.
type QRConfig struct {
	Hint string `json:"hint,omitempty"`
}

// CookieConfig describes the cookie form. RequiresUA asks the client to show
// the User-Agent field, because the site binds its session to it.
type CookieConfig struct {
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
	RequiresUA  bool   `json:"requires_ua,omitempty"`
	UAHelp      string `json:"ua_help,omitempty"`
}

// PasswordConfig describes the account/password form.
type PasswordConfig struct {
	Hint          string `json:"hint,omitempty"`
	UsernameLabel string `json:"username_label,omitempty"`
	PasswordLabel string `json:"password_label,omitempty"`
}

// SourceConfig lists the manually addable source kinds for a site.
type SourceConfig struct {
	Kinds []SourceKind `json:"kinds,omitempty"`
}

// SourceKind is one entry of the "add source" dialog. The shared client only
// collects a free-form ref string plus an optional title and hands it to the
// site driver, so URL formats stay inside the plugin.
type SourceKind struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	NeedsRef       bool   `json:"needs_ref,omitempty"`
	RefLabel       string `json:"ref_label,omitempty"`
	RefPlaceholder string `json:"ref_placeholder,omitempty"`
	RefHelp        string `json:"ref_help,omitempty"`
	// TitlePlaceholder is the example shown in the optional title field.
	TitlePlaceholder string `json:"title_placeholder,omitempty"`
	// Collections shows the "load lists" button for this kind.
	Collections bool `json:"collections,omitempty"`
}

// DownloadConfig carries the site-specific details the generic download
// manager needs when it writes sidecars.
type DownloadConfig struct {
	// ActorRole is written to the NFO <role> tag (default "作者").
	ActorRole string `json:"actor_role,omitempty"`
	// ImageReferer is used when a task has no page URL for its cover request.
	// It is internal plumbing and never exposed through /api/sites.
	ImageReferer string `json:"-"`
	// Referer/Origin are sent with stream and cover requests for sites whose
	// CDN checks them. Both are internal plumbing.
	Referer string `json:"-"`
	Origin  string `json:"-"`
}

// SiteNaming describes how one site lays out directories and file names.
type SiteNaming struct {
	VideoTemplate string   `json:"video_template"`
	PageTemplate  string   `json:"page_template"`
	TemplateVars  []string `json:"template_vars,omitempty"`
}

// CategoryName resolves a folder kind to the label the site declared. Unknown
// kinds fall back to the raw id so the API never invents a site-specific name.
func CategoryName(siteID, kind string) string {
	if s, ok := Get(siteID); ok {
		for _, c := range s.Categories {
			if c.ID == kind {
				return c.Name
			}
			for _, sourceKind := range c.Kinds {
				if sourceKind == kind {
					return c.Name
				}
			}
		}
	}
	return kind
}

var (
	mu     sync.RWMutex
	items  = map[string]Site{}
	assets = map[string][]byte{}
)

// Register is called from site-specific files; optional sites may be guarded by build tags.
func Register(s Site) {
	mu.Lock()
	defer mu.Unlock()
	items[s.ID] = s
}

func List() []Site {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Site, 0, len(items))
	for _, s := range items {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func Get(id string) (Site, bool) {
	mu.RLock()
	defer mu.RUnlock()
	s, ok := items[id]
	return s, ok
}

// RegisterAsset adds an immutable static asset owned by a plugin.
func RegisterAsset(siteID, name string, content []byte) {
	if siteID == "" || !fs.ValidPath(name) || name == "." || len(content) == 0 {
		panic("sites: invalid plugin asset")
	}
	key := siteID + "/" + name
	mu.Lock()
	defer mu.Unlock()
	if _, exists := assets[key]; exists {
		panic("sites: duplicate plugin asset " + key)
	}
	assets[key] = append([]byte(nil), content...)
}

// Asset returns a copy of an asset registered by the owning plugin.
func Asset(siteID, name string) ([]byte, bool) {
	if siteID == "" || !fs.ValidPath(name) || name == "." {
		return nil, false
	}
	mu.RLock()
	defer mu.RUnlock()
	content, ok := assets[siteID+"/"+name]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), content...), true
}
