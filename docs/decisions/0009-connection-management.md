# Connection management and local collection preparation

Date: October 17, 2026.

Supplier-independent collection infrastructure is implemented before returning to Lenovo order collection. The previous single fictional connection could not distinguish overlapping account source keys or configure independent collection controls. The source now includes migrations 0010–0012 in addition to the source-event foundation from [decision 0008](0008-collection-methods-and-events.md). Current deployment results are recorded separately in [BUILD_STATUS.md](../BUILD_STATUS.md).

## Connection identity and scope

Migration 0010 replaces the workspace-wide connection uniqueness rule with distinct connection identities and one optional default fictional feed. Orders and internal requirements use workspace, generation, connection and source key as their identity. Existing order IDs, observations and changes remain; the migration binds them to the existing default feed. Publication, stable-key checks, count-drop checks, requirement joins, jobs and collection history use the exact connection. One feed's reused external order key cannot select another feed's source facts or requirements.

The owner can create and edit connection settings: name, optional supplier, optional default buyer, intended method, minimum cadence and check interval. Supplier and buyer references must belong to the workspace, and the referenced directory entry/member must be eligible. Current generation and connection revision fence configuration changes. Creation and fictional source staging have retry keys. Connection method remains immutable; moving an existing feed to another extraction method requires a future explicit identity/provenance handover or a new connection. The frontend continues to consume the same normalized purchasing model.

Runnable connections remain exclusively fictional. Private workspaces can save API polling, webhook, browser, public-page extraction and email preparations with `connector=unconfigured` and `status=not_configured`. Those records have no accepted collection success and cannot enqueue or enable schedules. Method selection is a saved intention; it performs no discovery, source contact or authentication. Supplier directory creation still creates no collector.

## Fictional ingestion, scheduling and reset

Additional fictional connections start empty. Owner-only JSON upload accepts bounded normalized records and optional matching internal requirements. It verifies fictional origin, schema, supplier binding, quantities, source identities and mappings. Source replacement, superseded-run fencing, requirement replacement, audit and durable job insertion commit atomically. Collection then uses the registered fictional adapter and ordinary complete-batch publication, change detection and notification path. Uploaded fixture success is never evidence of live supplier collection.

Each connection has its own schedule, configured minimum and next due time. Cadence remains within 30–86,400 seconds and must meet that connection's minimum. Only eligible owner scheduling actors and runnable unpaused fictional connections are reconciled. This stores a minimum; it does not establish any supplier's permission to poll. Supplier quotas, rate-limit handling, jitter and circuit breaking remain live-integration work.

Pause disables scheduling, cancels outstanding runs and advances the connection fence. Late work cannot publish after that change. Resume does not automatically re-enable its schedule. Existing workspace refresh, schedule and scenario endpoints continue to use the default fictional connection.

Demo reset advances workspace generation and preserves historical evidence. It restores the default source, empties additional fictional payloads and clears generation-scoped supplier bindings. It clears fictional connection pause flags and collection health, disables their schedules and clears next-due times. Connection IDs, names, methods, cadence values and default-buyer mappings remain. Private workspaces and local vault files are outside that reset. Member notification preferences remain independent of demo generations.

## Notification routing and preferences

Migration 0011 adds optional connection buyer mapping and self-scoped per-workspace preferences. Condition notices select an eligible condition assignee first, then an eligible connection buyer, then active workspace members. Status-change notices use the connection buyer or active member fallback. Inactive, disabled and expired members are excluded. Buyer aliases in source records are not account IDs.

Both status-change and supplier-condition preferences default to enabled. Preferences filter recipients after assignment selection: an assigned buyer opting out suppresses their notice without redirecting it to unrelated members. Preferences affect future notices only and preserve source facts, events, priorities, previous notices and read state. Opt-in performs no historical backfill. Revision/generation checks protect edits, and a retry of the last accepted values can safely return its accepted revision. Ordinary runtime preference reads/writes remain self-scoped; a narrowly authorized recipient lookup lets publication honor the other recipients' choices without exposing their preference rows through the API. Member choices expose active names and IDs within the authorized workspace, without emails.

## Local credentials and retention

The [credential vault](../CREDENTIALS.md) provides a local Go package and file-based CLI. AES-256-GCM authenticates the envelope version and connection UUID; exclusive key creation, restricted permissions, symlink refusal and atomic value replacement protect local storage. CLI output contains metadata rather than plaintext. It neither accesses PostgreSQL nor verifies connection/workspace ownership. Runtime secret resolution, connection authorization, supplier-side credential/session schemas, expiry/revocation and browser isolation remain unwired. No HTTP credential endpoint or runtime/deployment mount is added. Vault and key recovery are separate from PostgreSQL backup.

Migration 0012 revokes observation UPDATE/DELETE from `bwb_app` and adds maintenance indexes. The [retention CLI](../RETENTION.md) uses explicit local administration credentials to plan and apply bounded workspace compaction. A reviewed fixed cutoff and explicit confirmation are required for apply. Cleanup can clear old terminal-run staged payloads and remove eligible unchanged intermediate observations; it preserves first/latest observations, distinct transitions, event-bearing evidence, run ledgers, source facts, changes, notices, notes/actions, audits and idempotency records. Cleanup and its maintenance audit commit together. Nothing runs this cleanup automatically, and compaction is not full erasure or supplier-session revocation.

## Remaining boundary

No Lenovo or other live connector, supported account API, webhook receiver, browser runner, email transport, partial-event publisher or external notification channel is implemented. Private preparations cannot satisfy those gates. See the [collection strategy](../COLLECTION_STRATEGY.md) and [Lenovo discovery record](../intake/LENOVO_DISCOVERY.md) for the required account evidence and the complete-versus-partial contract.

Apply pending migrations through the one-off local migration command before the updated API/worker; runtime processes never migrate. Schema-aware restore sanitation and runtime probes support versions 8, 9 and 12. Disposable dump/restore verification preserves accepted facts and event/observation lineage, keeps private preparations unconfigured and proves runtime isolation/immutable-history grants. Those compatibility tests access no actual project backup, vault key or live database. Deployment and operational recovery require their own release checks. Executed evidence and skipped checks belong in [BUILD_STATUS.md](../BUILD_STATUS.md); older deployed-build checks do not establish a result for a newer source revision.
