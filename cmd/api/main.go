package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"buyersworkbench/internal/httpapi"
	"buyersworkbench/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("API stopped", "reason", err.Error())
		os.Exit(1)
	}
}

func run() error {
	config, err := httpapi.ConfigFromEnv()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := store.Open(ctx)
	if err != nil {
		return errors.New("database initialization failed; verify local configuration")
	}
	defer pool.Close()
	handler, err := httpapi.New(pool, config, slog.Default())
	if err != nil {
		return err
	}
	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	errCh := make(chan error, 1)
	go func() { slog.Info("API listening", "address", addr); errCh <- server.ListenAndServe() }()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("HTTP listener failed")
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return errors.New("HTTP shutdown failed")
		}
		return nil
	}
}
