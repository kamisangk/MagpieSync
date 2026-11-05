package sites

import (
	"bytes"
	"testing"
)

func TestRegisterAssetReturnsIsolatedContent(t *testing.T) {
	want := []byte("asset payload")
	RegisterAsset("synthetic-assets", "icons/mark.svg", want)
	want[0] = 'X'

	got, ok := Asset("synthetic-assets", "icons/mark.svg")
	if !ok || !bytes.Equal(got, []byte("asset payload")) {
		t.Fatalf("Asset() = %q, %v", got, ok)
	}
	got[0] = 'Y'
	again, ok := Asset("synthetic-assets", "icons/mark.svg")
	if !ok || !bytes.Equal(again, []byte("asset payload")) {
		t.Fatalf("Asset() changed registered content: %q, %v", again, ok)
	}
}

func TestAssetRejectsInvalidPath(t *testing.T) {
	for _, name := range []string{"", ".", "../secret", "icons/../secret"} {
		if _, ok := Asset("synthetic-assets", name); ok {
			t.Errorf("Asset accepted path %q", name)
		}
	}
}

func TestListIsStable(t *testing.T) {
	list := List()
	for i := 1; i < len(list); i++ {
		if list[i-1].ID > list[i].ID {
			t.Fatalf("sites are not sorted: %v", list)
		}
	}
}

func TestSitesHaveLogos(t *testing.T) {
	for _, site := range List() {
		if site.Logo == "" {
			t.Fatalf("%s has no logo asset", site.ID)
		}
	}
}
