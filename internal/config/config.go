package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
)

// Config holds runtime configuration. Precedence: flag > environment > default.
type Config struct {
	Listen        string
	DataDir       string
	Secret        string
	Rate          float64
	Schedule      string
	ResetPassword bool
}

func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("magpiesync", flag.ContinueOnError)
	listen := fs.String("listen", envOr("MAGPIESYNC_LISTEN", "127.0.0.1:8787"), "HTTP listen address")
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
	return Config{
		Listen:        *listen,
		DataDir:       *dataDir,
		Secret:        os.Getenv("MAGPIESYNC_SECRET"),
		Rate:          *rate,
		Schedule:      *schedule,
		ResetPassword: *resetPassword,
	}, nil
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
