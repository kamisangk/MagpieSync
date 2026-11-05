//go:build !no_acfun && (acfun || site_acfun) && (!explicit_sites || site_acfun)

package acfun

import (
	_ "embed"

	"magpiesync/internal/sites"
)

//go:embed assets/logo.svg
var logo []byte

func init() {
	sites.RegisterAsset("acfun", "logo.svg", logo)
}
