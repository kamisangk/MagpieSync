package httpapi

import (
	"context"
	"errors"

	"magpiesync/internal/siteproxy"
	"magpiesync/internal/sites"
)

func (s *Server) proxyForSite(ctx context.Context, site string) (string, error) {
	return siteproxy.Load(ctx, s.store, s.vault, site)
}

func (s *Server) siteProxySettings(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string)
	for _, site := range sites.List() {
		proxy, err := s.proxyForSite(ctx, site.ID)
		if err != nil {
			return nil, err
		}
		out[site.ID] = proxy
	}
	return out, nil
}

func (s *Server) siteProxySettingsEnc(proxies map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(proxies))
	for site, proxy := range proxies {
		if _, ok := sites.Get(site); !ok {
			return nil, errors.New("不支持的代理站点")
		}
		encoded, err := siteproxy.Encode(s.vault, site, proxy)
		if err != nil {
			return nil, err
		}
		out[siteproxy.Key(site)] = encoded
	}
	return out, nil
}
