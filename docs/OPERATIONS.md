# Operations and handoff

The optional owner preview runs behind a dedicated Cloudflare Tunnel and an owner-only Access policy. The local entry is http://127.0.0.1:4180. See [TUNNEL.md](TUNNEL.md) for hostname configuration, the local resource manifest and connector token handling. Actual owner identity, deployment identifiers and tokens stay outside this public repository. An application reviewer account does not itself grant Cloudflare Access.

## Update the current stack

Run from the repository root. Preserve `.local/`, the ignored deployment manifest and the project PostgreSQL volume. Take an encrypted backup first using `python3 scripts/backup-local.py` and verify changes with `make check` and `make test-integration`. These commands update the existing project only; select the intended Docker context and Compose project before running them.

```sh
make tunnel-build
docker compose -f compose.yaml -f compose.tunnel.yaml --profile tunnel stop api worker public-api
docker compose run --rm --no-deps migrate
docker compose -f compose.yaml -f compose.tunnel.yaml --profile tunnel up -d --no-deps --wait api worker public-api public-web web cloudflared
make status
```

The API/worker never migrate. An unknown or mismatched migration makes readiness fail; use the matching source/image. Do not edit an applied SQL file. Stopping both API variants and the worker before migration avoids serving mixed schema versions. The current source has twelve application migrations. Migration 0010 changes connection/source identity, so the older API/worker must not run while migrations 0009–0012 are applied. The Cloudflare entry may briefly report unavailable during this coordinated update. Do not change unrelated host services, networks or tunnels.

Use `docker compose logs --tail=60 api worker` and `make tunnel-logs` for safe operational logs; avoid full environment, secret or raw private payload dumps. Confirm `/readyz`, exact built assets and the anonymous Access redirect. Compare pre/post-migration record counts without printing private contents, then perform the [isolated encrypted restore check](BACKUP.md). Verify a fictional collection only in an explicitly selected test workspace; do not seed, reset or change existing owner data as a deployment smoke test. A process healthcheck alone is not source freshness.

## Initialize a fictional workspace

Use `make status` to obtain its UUID, then:

```sh
docker compose --profile tools run --rm --no-deps admin seed --workspace WORKSPACE_UUID
```

This is idempotent setup and queues collection; it does not reset prior edits. The UI offers the same explicit initialize action. The private workspace cannot be seeded. Actual collector time differs from the fixed October 19, 2026 fictional business clock.

## Reviewer lifecycle

Provision only actual invited reviewers. The backend supports isolated accounts now; widening Cloudflare Access is a separate explicit owner action. Do not put temporary passwords in command arguments, shell history or chat. Create a mode 0400/0600 file in a private directory with a hidden local prompt, and mount it read-only into the one-off admin container:

```sh
docker compose --profile tools run --rm --no-deps \
  --user "$(id -u):$(id -g)" \
  --volume /absolute/private/reviewer-password:/run/reviewer-password:ro \
  admin create-reviewer --email reviewer@example.com --name 'Invited reviewer' \
  --password-file /run/reviewer-password
```

Remove the temporary file immediately and deliver the temporary secret out of band. The CLI creates only that reviewer's membership and fictional seed, defaults expiry to 14 days, and requires first-use password change. Repeating the same email preserves existing credentials and edits. Add `--expires RFC3339_TIMESTAMP` on initial creation for an explicit expiry. `--include-approved-snapshots` explicitly clones only approved public projection copies from the presentation workspace; it never copies private records/lineage or owner notes.

Use the same admin invocation for:

```text
set-reviewer-expiry --user-id UUID --expires RFC3339_TIMESTAMP
disable-reviewer --user-id UUID
reset-reviewer-password --user-id UUID --password-file /run/reviewer-password
```

Disable and reset revoke sessions immediately. These commands refuse the owner as a reviewer target. Account provisioning is local privileged administration, not public signup. The owner is not silently made a member of each reviewer sandbox; normal owner domain routes still require explicit workspace membership.

## Demonstration rehearsal

Use the fictional presentation workspace. Baseline has 4 lines and a missing-confirmation follow-up; unknown arrival coverage may also appear as data attention. Apply SC02: Atlas moves from Oct21 to Oct28, creating a 3-day risk against Oct25. Save a note, acknowledgment, assignment or follow-up; reload. SC07 advances freshness without duplicate transitions/tasks/notifications. SC08 creates a recovery history event and clears the active delay while preserving human work. SC03 shows 40 shipped/60 unshipped and 100 unreceived; no shortage is inferred. SC05 leaves accepted facts/freshness intact and shows a failed run. Confirming SC04 clears missing confirmation after processing.

Quote Comparison shows modeled totals Atlas 1330.00, Meridian 1125.00, Harbor 990.00. Harbor is late/pending approval; Meridian is the eligible winner, 205.00 below Atlas. Edit a fictional input and verify persisted server recalculation. Supplier Performance states observation coverage and unavailable on-time performance.

Reset needs explicit confirmation and the current generation. It changes only the selected demo workspace, restores its pinned source/quote/requirements, preserves approved snapshot copies, and starts a baseline collection. Additional fictional source payloads and generation-scoped supplier bindings clear. Fictional connections retain their IDs, names, methods, cadence values and default buyers; pause flags clear, schedules disable and next-due/collection-health state clears. The UI stays initializing until the default baseline run succeeds. Old browser edits return409; older jobs cannot publish records or notifications. Users, memberships, private facts, notification preferences and security audit survive. Never use database recreation or volume deletion as a demo reset.

## Source failures and recovery

Inspect Data Health for the last accepted success and run code. Incomplete/invalid batches stay quarantined; previous accepted state is retained. A controlled fictional failure can be cleared with Baseline or a subsequent registered scenario, then collected again. Lease-fenced superseded jobs are harmless. Durable schedules coalesce overdue current-state refreshes instead of replaying missed ticks; observation gaps are not recovered history.

A future real-source challenge/login/schema problem requires owner attention. Never bypass MFA/CAPTCHA or let a reviewer supply an arbitrary source URL. Lenovo discovery, permitted scope/cadence, session storage and browser sandbox/egress are deferred by the owner; the source has no active credentials or schedule now.

## Backup, retention and recovery

See BACKUP.md for encrypted local backups, retention of the latest snapshot on each of the 14 most recent backup dates, key custody and isolated restore rehearsal. Backups contain private credentials/data and must stay restricted. The actual restore script creates only a disposable network-isolated PostgreSQL target, revokes copied sessions/disables schedules, checks constraints/RLS and removes its container. It never restores into the running application. Off-host backup destination, independent key escrow and automated backup cadence are not configured.

## Remaining release gates

Live Lenovo discovery/collection and browser isolation verification are deferred, so a full real-source demo is not claimed. Add external reviewers to Access only after reviewing the current test evidence, intended expiry and sharing scope. Local backup/restore does not establish off-host disaster recovery. The application supports a bounded controlled demo dataset, single USD/EA quote model, date-only arrivals, local identities, and in-app notifications; no supplier write operation or external message delivery exists.
