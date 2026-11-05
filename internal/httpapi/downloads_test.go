package httpapi

import (
	"testing"

	"magpiesync/internal/sites"
	"magpiesync/internal/store"
)

func TestDirectTaskInputs(t *testing.T) {
	items := []store.Item{
		{ID: 1, RemoteID: "abc", Title: "One", URL: "https://example.test/video/1", Duration: 8, CoverURL: "https://example.test/c.jpg", AuthorName: "Artist"},
		{ID: 2, RemoteID: "missing", Title: "No URL"},
	}
	inputs, err := directTaskInputs("synthetic-direct", "Folder", items)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 {
		t.Fatalf("inputs = %+v", inputs)
	}
	in := inputs[0]
	if in.Site != "synthetic-direct" || in.PageURL != "https://example.test/video/1" || in.RemoteID != "abc" ||
		in.VideoTitle != "One" || in.SourceTitle != "Folder" || in.PageCount != 1 || in.Duration != 8 {
		t.Fatalf("input = %+v", in)
	}
}

func TestQualityAllowedForDirectSites(t *testing.T) {
	site := sites.Site{DownloadQualities: []sites.DownloadQuality{{Value: 360}, {Value: 720}, {Value: 1080}}}
	if !qualityAllowed(site, 0) || !qualityAllowed(site, 720) || qualityAllowed(site, 80) {
		t.Fatalf("unexpected quality validation for %+v", site.DownloadQualities)
	}
}

func TestSiteQualityOptionsIncludeAutomatic(t *testing.T) {
	site := sites.Site{DownloadQualities: []sites.DownloadQuality{{Value: 360}, {Value: 720}}}
	options := siteQualityOptions(site)
	if len(options) != len(site.DownloadQualities)+1 {
		t.Fatalf("options = %+v", options)
	}
	if options[0]["value"] != 0 {
		t.Fatalf("first option should be automatic: %+v", options)
	}
	if !qualityAllowed(site, 720) || !qualityAllowed(site, 0) {
		t.Fatalf("site should accept declared qualities: %+v", site.DownloadQualities)
	}
}
