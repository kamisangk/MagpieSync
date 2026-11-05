package httpapi

import (
	"net/http"
	"strings"
	"time"

	"magpiesync/internal/sites"
)

type sourceDTO struct {
	ID        int64  `json:"id"`
	Site      string `json:"site"`
	Kind      string `json:"kind"`
	KindLabel string `json:"kind_label,omitempty"`
	OwnerID   string `json:"owner_id"`
	Remote    string `json:"remote_id"`
	Title     string `json:"title"`
	State     string `json:"state"`
	Count     int    `json:"count"`
	LocalDone int    `json:"local_done"`
	LocalMax  int    `json:"local_total"`
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	acct, err := s.currentAccount(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return
	}
	switch r.Method {
	case http.MethodGet:
		folders, err := s.store.ListFolders(r.Context(), acct.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.attachFolderLocal(r.Context(), acct.ID, folders); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]sourceDTO, 0, len(folders))
		for _, f := range folders {
			out = append(out, sourceDTO{
				ID: f.ID, Site: acct.Site, Kind: f.Kind, KindLabel: sites.CategoryName(acct.Site, f.Kind),
				OwnerID: f.OwnerID, Remote: f.RemoteID, Title: f.Title, State: f.State, Count: f.ItemCount,
				LocalDone: f.LocalDone, LocalMax: f.LocalTotal,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"sources": out})
	case http.MethodPost:
		var body struct {
			Site    string `json:"site"`
			Kind    string `json:"kind"`
			Ref     string `json:"ref"`
			OwnerID string `json:"owner_id"`
			ID      string `json:"id"`
			Title   string `json:"title"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		siteID := strings.TrimSpace(body.Site)
		if siteID == "" {
			siteID = acct.Site
		}
		site, ok := sites.Get(siteID)
		if !ok {
			writeError(w, http.StatusBadRequest, "该站点未编译进当前版本")
			return
		}
		if !site.Ready {
			writeError(w, http.StatusBadRequest, site.Name+" 适配器尚未接入")
			return
		}
		if site.ID != acct.Site {
			writeError(w, http.StatusBadRequest, "请先连接该站点账号")
			return
		}
		drv, ok := sites.DriverFor(siteID)
		if !ok {
			writeError(w, http.StatusBadRequest, "该站点未编译进当前版本")
			return
		}
		builder, ok := drv.(sites.SourceBuilder)
		if !ok {
			writeError(w, http.StatusBadRequest, "该站点不支持手动添加来源")
			return
		}
		kind := strings.TrimSpace(body.Kind)
		spec, err := builder.BuildSource(sites.SourceRequest{
			Kind:    kind,
			Ref:     body.Ref,
			OwnerID: body.OwnerID,
			ID:      body.ID,
			Title:   body.Title,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		folderID, err := s.store.AddSource(r.Context(), acct.ID, kind, spec.OwnerID, spec.RemoteID, spec.Title, time.Now().Unix())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id": folderID, "site": siteID, "kind": kind, "owner_id": spec.OwnerID, "remote_id": spec.RemoteID, "title": spec.Title,
		})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCollections(w http.ResponseWriter, r *http.Request) {
	acct, ok := s.requireAccount(w, r)
	if !ok {
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		ref = acct.RemoteUID
	}
	drv, ok := sites.DriverFor(acct.Site)
	if !ok {
		writeError(w, http.StatusBadRequest, "该站点未编译进当前版本")
		return
	}
	collections, ok := drv.(sites.CollectionsDriver)
	if !ok {
		writeError(w, http.StatusBadRequest, "该站点不支持合集列表")
		return
	}
	acctCtx, err := s.accountContext(r.Context(), acct)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取凭据失败："+err.Error())
		return
	}
	page, err := collections.Collections(r.Context(), acctCtx, ref)
	if err != nil {
		writeError(w, http.StatusBadGateway, "获取合集失败："+err.Error())
		return
	}
	out := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		out = append(out, map[string]any{
			"kind":     item.Kind,
			"id":       item.ID,
			"owner_id": item.OwnerID,
			"title":    item.Title,
			"cover":    item.CoverURL,
			"count":    item.Count,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"owner_id": page.OwnerID, "collections": out})
}
