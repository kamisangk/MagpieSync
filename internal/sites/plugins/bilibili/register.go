//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bilibili

import (
	"magpiesync/internal/sites"
	"magpiesync/internal/sites/plugins/bilibili/core"
)

func init() {
	sites.Register(sites.Site{
		ID:       "bilibili",
		Name:     "哔哩哔哩",
		Homepage: "https://www.bilibili.com",
		Logo:     "/api/site-assets/bilibili/logo.svg",
		Ready:    true,
		Download: true,
		DownloadQualities: []sites.DownloadQuality{
			{Value: 16, Label: "360P"},
			{Value: 32, Label: "480P"},
			{Value: 64, Label: "720P"},
			{Value: 74, Label: "720P60"},
			{Value: 80, Label: "1080P"},
			{Value: 112, Label: "1080P+"},
			{Value: 116, Label: "1080P60"},
			{Value: 120, Label: "4K"},
			{Value: 125, Label: "HDR"},
			{Value: 126, Label: "杜比视界"},
			{Value: 127, Label: "8K"},
		},
		Categories: []sites.Category{
			{ID: "favorite", Name: "收藏夹"},
			{ID: "collection", Name: "合集", Kinds: []string{"season", "series"}},
			{ID: "submission", Name: "自行投稿"},
			{ID: "watch_later", Name: "稍后再看"},
		},
		ArchiveExtras: []string{"danmaku", "subtitles", "subtitles_ai"},
		Naming: &sites.SiteNaming{
			VideoTemplate: "{{author_name}}/{{title}}",
			PageTemplate:  "{{page_title}}",
			TemplateVars:  []string{"remote_id", "title", "author_name", "author_id", "pubtime", "fav_time", "page_title", "page_no"},
		},
		Login: sites.LoginConfig{
			Methods: []sites.LoginMethod{
				{ID: "qrcode", Label: "扫码登录"},
				{ID: "cookie", Label: "使用 Cookie"},
			},
			QR: &sites.QRConfig{Hint: "使用哔哩哔哩手机客户端扫码"},
			Cookie: &sites.CookieConfig{
				Placeholder: "SESSDATA=...",
				Help:        "从已登录的浏览器复制完整 Cookie，至少需要 SESSDATA。",
			},
		},
		Source: sites.SourceConfig{Kinds: []sites.SourceKind{
			{ID: "watch_later", Label: "稍后再看"},
			{
				ID: "submission", Label: "自行投稿", NeedsRef: true,
				RefLabel:       "UP 主 mid 或空间链接",
				RefPlaceholder: "123456 或 https://space.bilibili.com/123456",
				RefHelp:        "只读取该 UP 主的公开投稿，不会修改任何内容。",
			},
			{
				ID: "season", Label: "合集", NeedsRef: true, Collections: true,
				RefLabel:         "合集链接或 mid:id",
				RefPlaceholder:   "https://space.bilibili.com/123/lists/456 或 123:456",
				RefHelp:          "只填 UP 主 mid 时，先读取公开列表再选择要同步的合集。",
				TitlePlaceholder: "例如：2026 春季番",
			},
			{
				ID: "series", Label: "系列", NeedsRef: true, Collections: true,
				RefLabel:         "系列链接或 mid:id",
				RefPlaceholder:   "https://space.bilibili.com/123/lists/456 或 123:456",
				RefHelp:          "只填 UP 主 mid 时，先读取公开列表再选择要同步的系列。",
				TitlePlaceholder: "例如：我的系列",
			},
		}},
		DownloadInfo: sites.DownloadConfig{
			ActorRole: "UP主",
			Referer:   "https://www.bilibili.com/",
			Origin:    "https://www.bilibili.com",
		},
	})
	sites.RegisterDriver("bilibili", driver{})
}

type driver struct{}

func (driver) SiteID() string { return "bilibili" }

func (driver) IsAuth(err error) bool { return bili.IsAuth(err) }

func (driver) IsFatal(err error) bool { return bili.IsFatal(err) }
