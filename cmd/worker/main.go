package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"buyersworkbench/internal/jobs"
	"buyersworkbench/internal/store"
	"buyersworkbench/internal/workbench"
	"buyersworkbench/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker stopped", "reason", err.Error())
		os.Exit(1)
	}
}

func run() error {
	probe := flag.String("probe-workspace", "", "enqueue a durable M0 probe for the owner's presentation workspace and exit")
	check := flag.Bool("check", false, "check database and migration readiness and exit")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := store.Open(ctx)
	if err != nil {
		return errors.New("database initialization failed; verify local configuration")
	}
	defer pool.Close()
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = migrations.Check(checkCtx, pool)
	if err == nil {
		err = store.CheckQueue(checkCtx, pool)
	}
	cancel()
	if err != nil {
		return errors.New("database migrations are not ready; run the local migration command")
	}
	if *check {
		fmt.Println("database and queue migrations ready")
		return nil
	}
	if *probe != "" {
		probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		id, err := jobs.EnqueueProbe(probeCtx, pool, *probe)
		if err != nil {
			return err
		}
		fmt.Printf("enqueued foundation probe job %d\n", id)
		return nil
	}
	// River errors can include SQL/job args. Keep its internal logs off by
	// default; operator logs contain only generic lifecycle facts.
	client, err := jobs.NewClient(pool, true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return errors.New("queue initialization failed")
	}
	if err = client.Start(context.Background()); err != nil {
		return errors.New("queue startup failed")
	}
	go workbench.New(pool).RunScheduler(ctx, func(error) { slog.Warn("durable schedule reconciliation failed") })
	slog.Info("worker started", "job_kinds", "foundation_probe,collect_fictional_connection")
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err = client.Stop(shutdownCtx); err != nil {
		return errors.New("queue graceful shutdown failed")
	}
	return nil
}
