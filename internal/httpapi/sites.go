package httpapi

import (
	"mime"
	"net/http"
	"path"

	"magpiesync/internal/sites"
)

func (s *Server) handleSiteAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("asset")
	content, ok := sites.Asset(r.PathValue("site"), name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(content)
}

type siteAccountDTO struct {
	ID       int64  `json:"id"`
	UID      string `json:"uid"`
	Name     string `json:"name"`
	Avatar   string `json:"avatar_url,omitempty"`
	State    string `json:"state"`
	Current  bool   `json:"current"`
	HasProxy bool   `json:"has_proxy"`
}

type siteDTO struct {
	ID            string                  `json:"id"`
	Name          string                  `json:"name"`
	Homepage      string                  `json:"homepage,omitempty"`
	Logo          string                  `json:"logo,omitempty"`
	Ready         bool                    `json:"ready"`
	Download      bool                    `json:"download"`
	Qualities     []sites.DownloadQuality `json:"download_qualities,omitempty"`
	ArchiveExtras []string                `json:"archive_extras,omitempty"`
	Naming        *sites.SiteNaming       `json:"naming,omitempty"`
	Login         sites.LoginConfig       `json:"login"`
	Source        sites.SourceConfig      `json:"source"`
	Connected     bool                    `json:"connected"`
	Account       *siteAccountDTO         `json:"account,omitempty"`
	Accounts      []siteAccountDTO        `json:"accounts"`
	Categories    []sites.Category        `json:"categories"`
}

func (s *Server) handleSites(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.store.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	current, _ := s.currentAccount(r.Context())
	var currentID int64
	if current != nil {
		currentID = current.ID
	}
	bySite := make(map[string][]siteAccountDTO, len(accounts))
	for i := range accounts {
		a := accounts[i]
		proxy, _ := s.proxyForAccount(&a)
		bySite[a.Site] = append(bySite[a.Site], siteAccountDTO{
			ID: a.ID, UID: a.RemoteUID, Name: a.Name, Avatar: a.AvatarURL,
			State: a.State, Current: a.ID == currentID, HasProxy: proxy != "",
		})
	}
	list := sites.List()
	out := make([]siteDTO, 0, len(list))
	for _, site := range list {
		siteAccounts := bySite[site.ID]
		var account *siteAccountDTO
		if len(siteAccounts) > 0 {
			first := siteAccounts[0]
			account = &first
		}
		out = append(out, siteDTO{
			ID:            site.ID,
			Name:          site.Name,
			Homepage:      site.Homepage,
			Logo:          site.Logo,
			Ready:         site.Ready,
			Download:      site.Download,
			Qualities:     site.DownloadQualities,
			ArchiveExtras: site.ArchiveExtras,
			Naming:        site.Naming,
			Login:         site.Login,
			Source:        site.Source,
			Connected:     len(siteAccounts) > 0,
			Account:       account,
			Accounts:      siteAccounts,
			Categories:    site.Categories,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": out})
}
