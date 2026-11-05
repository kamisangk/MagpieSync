//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"encoding/json"
	"testing"
)

func TestNavUsesCamelCaseIsLogin(t *testing.T) {
	var d navData
	if err := json.Unmarshal([]byte(`{"isLogin":true,"mid":123,"uname":"tester"}`), &d); err != nil {
		t.Fatal(err)
	}
	if !d.IsLogin || d.Mid != 123 || d.Uname != "tester" {
		t.Fatalf("nav decode = %+v, want logged-in account", d)
	}
}
