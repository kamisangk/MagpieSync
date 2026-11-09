package config

import "testing"

func TestLoadKeepsStandaloneHTTPDefaults(t *testing.T) {
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:8787" || cfg.UnixSocket != "" || cfg.BasePath != "" {
		t.Fatalf("default listeners = listen:%q socket:%q base:%q", cfg.Listen, cfg.UnixSocket, cfg.BasePath)
	}
}

func TestLoadUnixSocketAndBasePath(t *testing.T) {
	cfg, err := Load([]string{"-unix-socket", "/var/apps/magpiesync/magpiesync.sock", "-base-path", "/app/magpiesync"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UnixSocket != "/var/apps/magpiesync/magpiesync.sock" {
		t.Fatalf("UnixSocket = %q", cfg.UnixSocket)
	}
	if cfg.BasePath != "/app/magpiesync" {
		t.Fatalf("BasePath = %q", cfg.BasePath)
	}
}

func TestLoadRejectsInvalidBasePath(t *testing.T) {
	for _, basePath := range []string{"app/magpiesync", "/app/../other", "/app/name?x=1", "/app/name/"} {
		t.Run(basePath, func(t *testing.T) {
			if _, err := Load([]string{"-base-path", basePath}); err == nil {
				t.Fatalf("Load accepted invalid base path %q", basePath)
			}
		})
	}
}
