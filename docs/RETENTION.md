# Explicit source artifact retention

The local administration CLI can plan and apply a bounded cleanup for one workspace. It removes duplicate observation storage and clears old staged collection payloads. It does not erase purchasing history, run ledgers, accepted source facts, typed change events, notifications, buyer notes or actions, audits, or idempotency records. Nothing schedules or invokes cleanup automatically.

Migration 0012 removes `bwb_app`'s observation UPDATE and DELETE privileges and adds maintenance indexes. Apply it with the one-off local migration command before running the updated API, worker or retention tooling. Runtime roles retain SELECT and INSERT and cannot run maintenance. The maintenance package requires trusted local administration credentials with PostgreSQL superuser or row-security bypass; it refuses the constrained runtime role. No new privileged database function or runtime deletion grant is introduced.

## Review a plan

Select the intended host, database and workspace before using actual data. Use the existing restricted `DATABASE_URL_FILE` mechanism for local administration credentials. The command below is an invocation template, not evidence that maintenance has run:

```sh
go run ./cmd/bwb retention-plan \
  --workspace WORKSPACE_UUID \
  --days 90 \
  --limit 1000
```

Days must be 1 through 3650, and the combined operation limit must be 1 through 1000. Each plan freezes an exact UTC `before` timestamp and reports counts for `staged_batches` and `observations`, the workspace and generation, the limit, and `more_eligible`. Counts describe the selected bounded batch, not the total number of eligible rows. Staged payload cleanup takes priority over observation compaction. Plans make no persisted changes, including no audit insert. Two plans with the same cutoff and unchanged records select the same candidates.

To apply a reviewed plan in the future, copy its exact `before` timestamp, workspace and limit. Applying requires both an explicit timestamp and `--confirm`; an apply invocation cannot use a moving `--days` cutoff:

```sh
go run ./cmd/bwb retention-apply \
  --workspace WORKSPACE_UUID \
  --before EXACT_RFC3339_TIMESTAMP_FROM_PLAN \
  --limit 1000 \
  --confirm
```

The cutoff must be at least 24 hours old. A narrower historical cutoff remains valid when a plan is reviewed later. Each invocation rechecks candidates and applies at most the specified combined limit in one transaction. `more_eligible: true` means another bounded review/apply may be useful; it does not cause an automatic loop. New observations or another completed maintenance command can change the eligible batch after a plan was produced. The timestamp bounds eligibility; the plan is not a signed immutable list of row IDs.

## What qualifies

A staged payload is eligible only when its run has a non-null finish timestamp earlier than the cutoff and a terminal state: succeeded, rejected, transient_failed, terminal_failed, cancelled or stale_generation. No run referenced by a connection's `active_run_id` is eligible, even if its state or timestamps are inconsistent. Clearing `staged_batch` leaves the run ID, scope, generation, actor, outcome, failure code, timestamps, counts, source revision and job association intact. Rejected payload content can therefore expire without discarding the failure ledger.

An observation is eligible only when all of these conditions hold:

- It belongs to the chosen workspace, is older than the cutoff, and its succeeded run also finished before the cutoff.
- Its normalized JSONB record equals the immediately preceding observation for that same order and generation. Distinct record transitions remain, including fields that did not produce a typed event.
- It is neither the first nor the latest observation, ordered by observation timestamp and UUID. Observations matching the order's recorded first/last timestamps also remain; this conservatively preserves tied endpoint evidence.
- It has no source event in `wb_changes` with the same workspace, generation, order and run, and no connection points to its run as active.

Every event-bearing observation survives, preserving the source-event-to-observation link used in order history. The order's `record` and `first_record` are never changed. All generations are considered independently, so demo reset does not make prior evidence eligible merely by moving the current generation. A workspace row lock uses the same lock order as publication, reset and connection changes. Concurrent compliant writers serialize with maintenance. Mutation count is bounded; computing the observation window can still scan the workspace's history.

Apply writes a `retention.apply` audit with workspace, generation, fixed cutoff, counts, limit, batch time and database role. The audit and cleanup commit together; an audit failure rolls back cleanup. The actor user ID is null because this is a local database operator command, not an authenticated buyer action. Repeating an exhausted apply changes no source storage and records another zero-count maintenance audit. The output includes the committed audit ID. If a connection or storage failure prevents commit confirmation, inspect the workspace's audit records before retrying; a lost commit response can leave a successfully committed transaction.

This is storage compaction with preserved event history. It is not complete personal-data erasure, a supplier-session revocation mechanism, a backup cleanup policy or a guarantee that PostgreSQL/WAL/backups immediately release removed bytes. Database backups retain their own lifecycle, and local encrypted credentials are managed separately through [the credential vault](CREDENTIALS.md). There is no owner dashboard control, scheduled deletion worker, deployment change or live cleanup execution in this implementation.

Disposable integration tests cover dry-run immutability, first/latest and distinct-record retention, event-bearing evidence, active/recent run preservation, foreign workspace isolation, fixed cutoffs, combined batch limits, concurrent application, rollback when auditing fails, repeat application, and runtime UPDATE/DELETE denial. Real workspace cleanup remains an explicit future operator action.
