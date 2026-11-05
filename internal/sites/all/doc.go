// Package all imports the site drivers selected for this build.
//
// By default every ready site is included. Build with no_<site> tags to
// exclude sites, or with explicit_sites plus site_<site> tags to include only
// the listed sites.
//
// The files in this directory are generated from internal/sites/plugins by
// gen.go; do not edit them by hand. Adding a plugin directory and rebuilding
// (or running `go generate ./internal/sites/all`) is enough to wire it in.
//
//go:generate go run gen.go
package all
