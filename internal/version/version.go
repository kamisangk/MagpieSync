package version

// Values are overridden at build time with -ldflags.
var (
	Version = "0.10.2"
	Commit  = "dev"
	Date    = "unknown"
)
