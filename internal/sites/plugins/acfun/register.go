//go:build !no_acfun && (acfun || site_acfun) && (!explicit_sites || site_acfun)

// Package acfun carries the AcFun placeholder entry. It stays opt-in so the
// default build only advertises adapters that actually work.
package acfun

import "magpiesync/internal/sites"

func init() {
	sites.Register(sites.Site{
		ID:       "acfun",
		Name:     "AcFun",
		Homepage: "https://www.acfun.cn",
		Logo:     "/api/site-assets/acfun/logo.svg",
		Ready:    false,
		Download: false,
		Categories: []sites.Category{
			{ID: "favorite", Name: "收藏夹"},
			{ID: "collection", Name: "合集"},
			{ID: "submission", Name: "自行投稿"},
			{ID: "watch_later", Name: "稍后再看"},
		},
	})
}
