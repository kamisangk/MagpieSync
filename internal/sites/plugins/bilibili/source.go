//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bilibili

import (
	"context"
	"errors"
	"strings"

	"magpiesync/internal/sites"
	"magpiesync/internal/sites/plugins/bilibili/core"
	syncpkg "magpiesync/internal/sync"
)

func (driver) NewSource(ctx context.Context, in sites.AccountContext) (syncpkg.Source, error) {
	if in.Account == nil {
		return nil, errors.New("bilibili: missing account")
	}
	cred, err := bili.ParseCredential(in.Credential)
	if err != nil {
		return nil, err
	}
	client, err := bili.NewWithCookiesWithProxy(cred.Cookies, in.Rate, in.ProxyURL)
	if err != nil {
		return nil, err
	}
	adapter := bili.NewAdapter(client, in.Account.RemoteUID)
	if in.Store == nil {
		return adapter, nil
	}
	sources, err := in.Store.ListSources(ctx, in.Account.ID)
	if err != nil {
		return nil, err
	}
	extra := make([]syncpkg.RemoteFolder, 0, len(sources))
	for _, src := range sources {
		extra = append(extra, syncpkg.RemoteFolder{
			RemoteID: src.RemoteID,
			Title:    src.Title,
			Kind:     src.Kind,
			OwnerID:  src.OwnerID,
			Count:    src.RemoteCount,
		})
	}
	adapter.SetSources(extra)
	return adapter, nil
}

func (driver) NewVideoClient(_ context.Context, in sites.AccountContext) (sites.VideoClient, error) {
	cred, err := bili.ParseCredential(in.Credential)
	if err != nil {
		return nil, err
	}
	return bili.NewWithCookiesWithProxy(cred.Cookies, in.Rate, in.ProxyURL)
}

func (driver) Collections(ctx context.Context, in sites.AccountContext, ref string) (*sites.CollectionPage, error) {
	mid := strings.TrimSpace(ref)
	if parsedMid, _ := parseSourceRef(ref); parsedMid != "" {
		mid = parsedMid
	}
	if mid == "" {
		return nil, errors.New("需要 UP 主 mid 或空间链接")
	}
	cred, err := bili.ParseCredential(in.Credential)
	if err != nil {
		return nil, err
	}
	client, err := bili.NewWithCookiesWithProxy(cred.Cookies, in.Rate, in.ProxyURL)
	if err != nil {
		return nil, err
	}
	items, err := client.Collections(ctx, mid)
	if err != nil {
		return nil, err
	}
	out := make([]sites.Collection, 0, len(items))
	for _, item := range items {
		out = append(out, sites.Collection{
			Kind: item.Kind, ID: item.ID, OwnerID: item.Mid,
			Title: item.Title, CoverURL: item.CoverURL, Count: item.Count,
		})
	}
	return &sites.CollectionPage{OwnerID: mid, Items: out}, nil
}
