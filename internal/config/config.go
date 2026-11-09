package config

import (
	"flag"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
)

// Config holds runtime configuration. Precedence: flag > environment > default.
type Config struct {
	Listen        string
	UnixSocket    string
	BasePath      string
	DataDir       string
	Secret        string
	Rate          float64
	Schedule      string
	ResetPassword bool
}

func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("magpiesync", flag.ContinueOnError)
	listen := fs.String("listen", envOr("MAGPIESYNC_LISTEN", "127.0.0.1:8787"), "HTTP listen address")
	unixSocket := fs.String("unix-socket", os.Getenv("MAGPIESYNC_UNIX_SOCKET"), "HTTP Unix socket path (overrides -listen)")
	basePath := fs.String("base-path", os.Getenv("MAGPIESYNC_BASE_PATH"), "HTTP path prefix used by a reverse proxy")
	dataDir := fs.String("data-dir", envOr("MAGPIESYNC_DATA_DIR", "./data"), "data directory")
	rate := fs.Float64("rate", envFloat("MAGPIESYNC_RATE", 1.5), "maximum requests per second per site client")
	schedule := fs.String("schedule", os.Getenv("MAGPIESYNC_SCHEDULE"), "initial cron schedule for automatic sync (saved settings take precedence)")
	resetPassword := fs.Bool("reset-password", false, "remove the local Web UI password and exit; the next start asks for a new one")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if *rate <= 0 || *rate > 20 {
		return Config{}, fmt.Errorf("rate must be in (0, 20], got %v", *rate)
	}
	if *listen == "" {
		return Config{}, fmt.Errorf("listen address must not be empty")
	}
	if *basePath != "" && !validBasePath(*basePath) {
		return Config{}, fmt.Errorf("base path must be an absolute URL path without a trailing slash or special characters")
	}
	return Config{
		Listen:        *listen,
		UnixSocket:    *unixSocket,
		BasePath:      *basePath,
		DataDir:       *dataDir,
		Secret:        os.Getenv("MAGPIESYNC_SECRET"),
		Rate:          *rate,
		Schedule:      *schedule,
		ResetPassword: *resetPassword,
	}, nil
}

func validBasePath(value string) bool {
	if !strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || path.Clean(value) != value {
		return false
	}
	for _, r := range value {
		if !(r == '/' || r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return value != "/"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
