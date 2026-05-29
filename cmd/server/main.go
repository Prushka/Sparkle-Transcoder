package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sparkle-transcoder/internal/api"
	"sparkle-transcoder/internal/config"
	"sparkle-transcoder/internal/executil"
	"sparkle-transcoder/internal/media"
	"sparkle-transcoder/internal/task"

	log "github.com/sirupsen/logrus"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.Debug {
		log.SetLevel(log.DebugLevel)
	}
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		log.Fatalf("data dir: %v", err)
	}
	if err := os.MkdirAll(cfg.Output, 0755); err != nil {
		log.Fatalf("output dir: %v", err)
	}

	scanner := media.NewScanner(cfg)
	if err := scanner.LoadCache(); err != nil {
		log.Infof("scan cache unavailable: %v", err)
	}
	if cfg.ScanOnStartup {
		go func() {
			if _, err := scanner.Scan(context.Background(), false); err != nil && !errors.Is(err, media.ErrScanRunning) {
				log.Errorf("startup scan failed: %v", err)
			}
		}()
	}

	runner := task.NewRunner(cfg, scanner, executil.LocalRunner{LowPriority: cfg.EnableLowPriority})
	store := task.NewStore(cfg, scanner, runner)
	go func() {
		if err := store.Refresh(context.Background()); err != nil {
			log.Errorf("startup task refresh failed: %v", err)
		}
	}()
	server := api.New(cfg, scanner, store)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()
	api.StartPeriodicScan(ctx, scanner, cfg.ScanInterval)

	errCh := make(chan error, 1)
	go func() {
		log.Infof("starting API on %s", cfg.Addr)
		errCh <- server.Start()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Errorf("shutdown: %v", err)
		}
	case err := <-errCh:
		if err != nil {
			log.Fatalf("server: %v", err)
		}
	}
}
