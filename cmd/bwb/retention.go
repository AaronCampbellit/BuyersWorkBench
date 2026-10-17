package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"time"

	"buyersworkbench/internal/retention"
	"buyersworkbench/internal/store"
	"buyersworkbench/migrations"
)

const retentionUsage = "usage: bwb retention-plan --workspace UUID --days 30 [--limit 1000] | retention-apply --workspace UUID --before RFC3339 --confirm [--limit 1000]"

func parseRetention(args []string, now time.Time) (retention.Request, bool, error) {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "one workspace UUID")
	limit := flags.Int("limit", retention.MaxLimit, "maximum combined payloads and observations")
	var days int
	var before string
	var confirm bool
	if args[0] == "retention-plan" {
		flags.IntVar(&days, "days", 0, "retain at least this many days; 1..3650")
	} else {
		flags.StringVar(&before, "before", "", "fixed cutoff from a reviewed retention plan")
		flags.BoolVar(&confirm, "confirm", false, "apply one bounded retention transaction")
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return retention.Request{}, false, errors.New(retentionUsage)
	}
	if args[0] == "retention-plan" {
		req, err := retention.ForDays(*workspace, days, *limit, now)
		return req, false, err
	}
	if !confirm || before == "" {
		return retention.Request{}, false, errors.New(retentionUsage)
	}
	cutoff, err := time.Parse(time.RFC3339Nano, before)
	if err != nil {
		return retention.Request{}, false, errors.New("retention cutoff must be an RFC3339 timestamp from the plan")
	}
	req := retention.Request{WorkspaceID: *workspace, Before: cutoff, Limit: *limit}
	return req, true, req.Validate(now)
}

func runRetention(ctx context.Context, args []string, out io.Writer) (bool, error) {
	if len(args) == 0 || (args[0] != "retention-plan" && args[0] != "retention-apply") {
		return false, nil
	}
	req, apply, err := parseRetention(args, time.Now())
	if err != nil {
		return true, err
	}
	pool, err := store.Open(ctx)
	if err != nil {
		return true, err
	}
	defer pool.Close()
	if err = migrations.Check(ctx, pool); err != nil {
		return true, errors.New("retention requires the current schema; run the one-off local migration first")
	}
	var result retention.Result
	if apply {
		result, err = retention.Apply(ctx, pool, req)
	} else {
		result, err = retention.Plan(ctx, pool, req)
	}
	if err != nil {
		return true, err
	}
	return true, json.NewEncoder(out).Encode(result)
}
