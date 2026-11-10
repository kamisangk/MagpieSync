package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"magpiesync/internal/config"
	"magpiesync/internal/download"
	"magpiesync/internal/httpapi"
	"magpiesync/internal/netproxy"
	"magpiesync/internal/scheduler"
	"magpiesync/internal/siteproxy"
	"magpiesync/internal/sites"
	_ "magpiesync/internal/sites/all"
	"magpiesync/internal/store"
	syncpkg "magpiesync/internal/sync"
	"magpiesync/internal/vault"
	"magpiesync/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "magpiesync:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if host, _, err := net.SplitHostPort(cfg.Listen); cfg.UnixSocket == "" && err == nil {
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			slog.Warn("HTTP API is reachable from the network. The local password protects the UI, but still bind to 127.0.0.1 unless you trust the network.", "listen", cfg.Listen)
		}
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "magpiesync.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	if cfg.ResetPassword {
		if err := st.DeleteSetting(context.Background(), "auth.password"); err != nil {
			return fmt.Errorf("reset local password: %w", err)
		}
		slog.Info("local Web UI password cleared; start MagpieSync again to set a new password")
		return nil
	}

	v, err := vault.New(cfg.Secret, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("init vault: %w", err)
	}

	accountContext := func(ctx context.Context, accountID int64) (sites.AccountContext, error) {
		account, err := st.GetAccountByID(ctx, accountID)
		if err != nil {
			return sites.AccountContext{}, err
		}
		plain, err := v.Decrypt(account.Credential, account.Site+":"+account.RemoteUID)
		if err != nil {
			return sites.AccountContext{}, fmt.Errorf("decrypt %s credential: %w", account.Site, err)
		}
		proxyURL, err := siteproxy.Load(ctx, st, v, account.Site)
		if err != nil {
			return sites.AccountContext{}, err
		}
		return sites.AccountContext{
			Account:    account,
			Credential: plain,
			ProxyURL:   proxyURL,
			Rate:       cfg.Rate,
			Store:      st,
		}, nil
	}

	newHTTPClient := func(ctx context.Context, accountID int64) (*http.Client, error) {
		account, err := st.GetAccountByID(ctx, accountID)
		if err != nil {
			return nil, err
		}
		proxyURL, err := siteproxy.Load(ctx, st, v, account.Site)
		if err != nil {
			return nil, err
		}
		return netproxy.NewClient(proxyURL, 0)
	}

	engine := syncpkg.NewEngine(st, func(ctx context.Context, accountID int64) (syncpkg.Source, error) {
		acct, err := accountContext(ctx, accountID)
		if err != nil {
			return nil, err
		}
		drv, ok := sites.DriverFor(acct.Account.Site)
		if !ok {
			return nil, fmt.Errorf("site %q is not compiled into this build", acct.Account.Site)
		}
		return drv.NewSource(ctx, acct)
	})
	engine.SetGrace(2)

	hub := httpapi.NewHub()

	downloadSettings := func(ctx context.Context) (download.Settings, error) {
		return download.LoadSettings(ctx, st)
	}
	dl := download.NewManager(st, func(ctx context.Context, accountID int64) (download.VideoClient, error) {
		acct, err := accountContext(ctx, accountID)
		if err != nil {
			return nil, err
		}
		drv, ok := sites.DriverFor(acct.Account.Site)
		if !ok {
			return nil, fmt.Errorf("site %q is not compiled into this build", acct.Account.Site)
		}
		video, ok := drv.(sites.VideoDriver)
		if !ok {
			return nil, fmt.Errorf("site %q does not support video downloads", acct.Account.Site)
		}
		return video.NewVideoClient(ctx, acct)
	}, newHTTPClient, downloadSettings, func(kind string, data any) {
		hub.Publish(map[string]any{"type": kind, "download": data})
	})
	dl.SetDirectProvider(func(ctx context.Context, accountID int64, siteID string) (download.DirectResolver, error) {
		acct, err := accountContext(ctx, accountID)
		if err != nil {
			return nil, err
		}
		if acct.Account.Site != siteID {
			return nil, fmt.Errorf("account does not belong to site %q", siteID)
		}
		drv, ok := sites.DriverFor(siteID)
		if !ok {
			return nil, fmt.Errorf("site %q is not compiled into this build", siteID)
		}
		direct, ok := drv.(sites.DirectDriver)
		if !ok {
			return nil, fmt.Errorf("download is not supported for site %q", siteID)
		}
		resolver, err := direct.NewDirectResolver(ctx, acct)
		if err != nil {
			return nil, err
		}
		return newSiteResolver(func(ctx context.Context, pageURL string) ([]download.DirectOption, error) {
			opts, err := resolver.Options(ctx, pageURL)
			if err != nil {
				return nil, err
			}
			out := make([]download.DirectOption, 0, len(opts))
			for _, o := range opts {
				out = append(out, download.DirectOption{Quality: o.Quality, Label: o.Label, URL: o.URL, Headers: o.Headers})
			}
			return out, nil
		}, resolver.Close), nil
	})
	dl.SetAuthClassifier(sites.IsAuth)
	dl.Start(context.Background())
	defer dl.Stop()
	if settings, err := download.LoadSettings(context.Background(), st); err == nil && !download.FFmpegAvailable(settings.FFmpegPath) {
		slog.Warn("FFmpeg not found; DASH video/audio merging will fail until it is installed or configured", "path", settings.FFmpegPath)
	}

	api := httpapi.New(cfg, st, v, engine, hub)
	api.SetDownloadManager(dl)
	srv := &http.Server{
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	listener, err := listen(cfg)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	schedule, err := scheduler.Load(context.Background(), st, cfg.Schedule)
	if err != nil {
		return fmt.Errorf("load sync schedule: %w", err)
	}
	plan, err := scheduler.Parse(schedule)
	if err != nil {
		return fmt.Errorf("invalid sync schedule: %w", err)
	}
	scheduledSync := scheduler.New(func() {
		syncActiveAccounts(st, engine.Start)
	})
	scheduledSync.Configure(plan)
	api.SetScheduleUpdater(scheduledSync.Configure)
	defer scheduledSync.Stop()

	errCh := make(chan error, 1)
	go func() {
		address := cfg.Listen
		if cfg.UnixSocket != "" {
			address = cfg.UnixSocket
		}
		slog.Info("magpiesync listening", "addr", address, "data_dir", cfg.DataDir, "version", version.Version)
		errCh <- srv.Serve(listener)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	slog.Info("shutting down")
	scheduledSync.Stop()
	engine.CancelAll()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func listen(cfg config.Config) (net.Listener, error) {
	if cfg.UnixSocket == "" {
		return net.Listen("tcp", cfg.Listen)
	}
	listener, err := net.Listen("unix", cfg.UnixSocket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(cfg.UnixSocket, 0o666); err != nil {
		listener.Close()
		os.Remove(cfg.UnixSocket)
		return nil, fmt.Errorf("set Unix socket permissions: %w", err)
	}
	return listener, nil
}

type siteResolver struct {
	options func(context.Context, string) ([]download.DirectOption, error)
	close   func() error
}

func newSiteResolver(options func(context.Context, string) ([]download.DirectOption, error), closeFn func() error) download.DirectResolver {
	return &siteResolver{options: options, close: closeFn}
}

func (r *siteResolver) Options(ctx context.Context, pageURL string) ([]download.DirectOption, error) {
	return r.options(ctx, pageURL)
}

func (r *siteResolver) Close() error {
	if r.close != nil {
		return r.close()
	}
	return nil
}

func syncActiveAccounts(st *store.Store, start func(int64, string, string) (int64, error)) {
	accounts, err := st.ListAccounts(context.Background())
	if err != nil || len(accounts) == 0 {
		slog.Warn("scheduled sync skipped", "error", err)
		return
	}
	for i := range accounts {
		account := accounts[i]
		if account.State != "active" {
			continue
		}
		if _, err := start(account.ID, "scheduled", "all"); err != nil {
			slog.Warn("scheduled sync not started", "account_id", account.ID, "error", err)
		}
	}
}
