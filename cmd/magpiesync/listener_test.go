package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"magpiesync/internal/config"
)

func TestListenServesHTTPOverUnixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "magpiesync.sock")
	listener, err := listen(config.Config{UnixSocket: socketPath})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("listener path mode = %v, want socket", info.Mode())
	}

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})

	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}}
	resp, err := client.Get("http://unix/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("Unix HTTP response = %d %q, want 200 %q", resp.StatusCode, body, "ok")
	}
}

func TestListenKeepsStandaloneTCPMode(t *testing.T) {
	listener, err := listen(config.Config{Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if listener.Addr().Network() != "tcp" {
		t.Fatalf("listener network = %q, want tcp", listener.Addr().Network())
	}
}
