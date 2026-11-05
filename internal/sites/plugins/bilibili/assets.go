//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bilibili

import (
	_ "embed"

	"magpiesync/internal/sites"
)

//go:embed assets/logo.svg
var logo []byte

func init() {
	sites.RegisterAsset("bilibili", "logo.svg", logo)
}
