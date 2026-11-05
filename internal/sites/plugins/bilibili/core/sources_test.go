//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import "testing"

func TestSelectStreamsPrefersQualityThenCodec(t *testing.T) {
	p := &PlayInfo{
		Video: []Stream{
			{QualityID: 80, CodecID: 7, Bandwidth: 1000, URL: "avc-1080"},
			{QualityID: 80, CodecID: 12, Bandwidth: 900, URL: "hevc-1080"},
			{QualityID: 64, CodecID: 7, Bandwidth: 500, URL: "avc-720"},
		},
		Audio: []Stream{{QualityID: 30280, URL: "audio"}},
	}
	v, a, qn := p.SelectStreams(80, []string{"hevc", "avc"})
	if v == nil || v.URL != "hevc-1080" {
		t.Fatalf("selected video = %+v", v)
	}
	if a == nil || a.URL != "audio" {
		t.Fatalf("selected audio = %+v", a)
	}
	if qn != 80 {
		t.Fatalf("quality = %d", qn)
	}
}

func TestSelectStreamsDoesNotUpgradeAboveRequest(t *testing.T) {
	p := &PlayInfo{Video: []Stream{
		{QualityID: 120, CodecID: 7, URL: "4k"},
		{QualityID: 80, CodecID: 7, URL: "1080"},
	}}
	v, _, qn := p.SelectStreams(80, nil)
	if v == nil || v.URL != "1080" || qn != 80 {
		t.Fatalf("selected = %+v qn=%d", v, qn)
	}
}

func TestSourceKindFromRemoteID(t *testing.T) {
	cases := map[string]string{
		"watch_later":   "watch_later",
		"submission:42": "submission",
		"season:42:99":  "season",
		"series:42:99":  "series",
		"123456":        "favorite",
	}
	for in, want := range cases {
		if got := SourceKindFromRemoteID(in); got != want {
			t.Errorf("kind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSourceRef(t *testing.T) {
	kind, mid, remote, err := ParseSourceRef("season:42:99")
	if err != nil {
		t.Fatal(err)
	}
	if kind != "season" || mid != "42" || remote != "season:42:99" {
		t.Fatalf("parsed = %q %q %q", kind, mid, remote)
	}
	if _, _, _, err := ParseSourceRef("season:42"); err == nil {
		t.Fatal("expected error for incomplete season")
	}
}
