# Supplier collection and event strategy

Updated October 17, 2026. This is the implementation strategy for the multi-method collection requirement. The source contains twelve application migrations; current release and deployment evidence is recorded in [BUILD_STATUS.md](BUILD_STATUS.md). The original reference package remains unchanged in the local archive, outside the public repository. Read this alongside [PROJECT.md](../PROJECT.md), the [connection-management decision](decisions/0009-connection-management.md), [open gates](intake/OPEN_GATES.md) and the [Lenovo discovery record](intake/LENOVO_DISCOVERY.md).

The Workbench should consume validated supplier observations through one pipeline regardless of how they arrive. Select the most reliable **permitted and verified** method for each connection. Replacing a browser extractor with an API adapter must preserve source identity, field meaning, provenance, and the application API used by the frontend.

## Implementation boundary

| Area | Current scope | Work still required |
|---|---|---|
| Connector seam | Versioned connector contract and fictional adapter feeding the existing normalized batch validator/publication path | Real transports, connection-specific authentication and approved source mappings |
| Collection | Fictional source revisions; durable River jobs, leases, generation fences, staged validation and atomic publication | Lenovo or other live account collection; email, scraping, API and webhook transports |
| Connections and schedules | Multiple independently identified connections; connection-scoped orders, requirements, validation, runs and cadence; revision checks and pause fences. Only fictional feeds can collect or schedule. Private workspaces can save unconfigured method settings | Verified real account/configuration binding, supplier-approved rate policies, live authentication, backoff/jitter and circuit breaking |
| Changes and notifications | Accepted observations/history, conditions, buyer workflow and typed status-change events; active assignee/default-buyer routing and per-member workspace preferences | Supplier-origin event ingestion and external email/Teams/webhook delivery |
| Credentials | Local encrypted vault package and file-based administration CLI with connection UUID binding | Trusted runtime secret resolution, host/key custody setup, credential/session expiry and isolated browser execution |
| Retention | Explicit local plan/apply commands compact eligible duplicate observations and clear old staged payloads while preserving event evidence and run ledgers | Agreed live-data policy, any automatic cleanup, broader erasure and separate diagnostic/receipt/replay retention |
| Demo separation | Default fictional scenarios and additional fictional JSON feeds use the common pipeline; reset advances generation and clears additional source payloads; private-to-presentation sanitizer preview/approval exists | Real one-order proof and owner-approved shareable Lenovo fields |
| Deployment and recovery | Optional owner-only HTTPS profile and encrypted local backup/isolated restore tooling. Disposable dump/restore checks passed versions 8, 9 and 12, including private preparations and constrained-role probes; actual release checks are in BUILD_STATUS. Vault recovery is separate from PostgreSQL backup | Supplier runner isolation, approved egress, any inbound callback route, off-host backup and independent key custody |

The database now permits multiple connection identities per workspace. Runnable connections remain `fictional_supplier` / `fictional`. Private preparation records use `unconfigured`, retain no accepted collection success, and cannot enqueue or enable a schedule. Selecting API polling, webhook, browser, public-page extraction or email saves an intended method; it does not implement that transport. A supplier profile remains a directory entry and does not create a collector. Current verification results belong in [BUILD_STATUS.md](BUILD_STATUS.md), not in claims inferred from this design.

### Connection identity and local controls

Migration 0010 namespaces source order/line keys and internal requirements by connection within the workspace and generation. Two supplier feeds can use the same external key without sharing facts, conditions or requirements. Existing order identities and their history are preserved. Completeness/count-drop checks and stable-key checks apply to the publishing connection, and accepted history retains the exact run/connection lineage.

The owner can configure connection name, optional supplier and default buyer, intended method, minimum cadence and check interval. Changes require the current generation and connection revision. The method is immutable for an existing connection; changing extraction method currently requires a new connection. A future supported handover must preserve or explicitly map source identity and provenance before it can replace a real collector. There is no automatic transport discovery or selection.

Additional fictional connections start empty. The owner can upload bounded normalized fictional JSON, optionally including matching internal requirements, through the Data Health controls. Source staging validates supplier binding, origin, identities, quantities and mappings, then atomically retains the revision, fences superseded work and enqueues the common collector. A stable retry key reuses the accepted staging operation. The upload is a fixture mechanism and does not establish supplier access or live metrics.

Scheduling and refresh address a specific connection. Pause disables its schedule, cancels outstanding runs and advances its fence; late publication cannot replace accepted facts. Resume leaves scheduling disabled until explicitly enabled. The legacy workspace refresh/schedule/scenario endpoints continue to address the default fictional connection. Demo reset advances generation, restores the default seed, empties additional fictional payloads and clears generation-scoped supplier bindings. For fictional connections it clears pause flags and collection health, disables schedules and clears next-due times. Connection IDs, names, methods, cadence values and default-buyer mappings remain, as do member notification preferences; prior accepted evidence remains historical. Private connection preparations and vault files are outside demo reset.

## Collection methods

| Method | Appropriate use | Required evidence before enabling |
|---|---|---|
| Native webhooks or supported push feeds | Timely shipment/order updates offered to this account | Subscription eligibility, event coverage, sender authentication, ordering, retries, delivery IDs, test environment and reconciliation path |
| Supported REST/GraphQL API polling | Current order, shipment, price or inventory records | Approved client/scopes, read operation allowlist, pagination, incremental cursors, rate limits and documented field meanings |
| Browser automation | Authorized portal records requiring a normal authenticated session | Exact portal/account binding, normal login/MFA, allowed read actions, verified response/DOM extraction, completeness and isolated runner |
| Public-page scraping | Public listings or availability when permitted | Approved hosts/pages and cadence, page structure, pagination, localization and a reproducible field map; public visibility alone does not establish permission |
| Email ingestion | Approved confirmations or tracking messages | Dedicated authorized mailbox/folder, sender checks, stable message identity, supported templates, safe attachment limits and confidence/matching rules |

Supported exports and attended capture remain valid alternatives when they establish better coverage or authentication reliability. An internal Fetch/XHR/GraphQL request observed in a website is evidence about that site's implementation. It does not establish a supported external API, permission to replay it independently, or a webhook subscription.

An integration may combine methods: an authenticated push message can request an approved API refresh, while a scheduled reconciliation checks for missed deliveries. Do not automatically probe arbitrary supplier URLs or silently switch a connection to a broader access method. The connector advertises capabilities; the owner selects a verified permitted configuration. Later automatic selection can choose only among those approved capabilities and must record the method actually used.

## Discovery and method selection

Complete an implementation discovery record for each account and region before writing a real connector:

1. Establish the clean portal URL, account product/region, authorized operator, allowed records/date window, read-only purpose, permitted automation and cadence. Keep account identifiers and credentials outside tracked documents.
2. Review official developer and commercial integration documentation for APIs, webhooks, event subscriptions, callbacks, supported exports, acknowledgements and shipment notifications. Record exact source URLs, review date, account applicability and unresolved questions.
3. With account discovery authorized, inspect integration settings and normal order-list/detail traffic. Record minimized schemas, methods/operation names, pagination, filters and field semantics. Do not capture or commit complete HARs, cookies, tokens, query secrets or private response bodies.
4. Separate observed browser operations from vendor-supported integration contracts. Confirm subscription eligibility and rate/access terms with the supplier when necessary. Contact requires an explicit instruction; documentation research does not authorize sending messages.
5. Select the least complex verified method, document rejected alternatives and define stop conditions. Ambiguous dates, wrong-account evidence, challenge pages, unsupported schemas and incomplete pagination must prevent publication.
6. Prove one authorized order through collection → validation → private storage → comparison/history → safe UI. Then establish a bounded complete scope and failure recovery. Verify sanitizer output separately before sharing.
7. Enable scheduling only after authentication expiry, retries, account binding, resource/egress isolation and permitted cadence pass. A successful fictional run closes none of these gates.

### Public Lenovo evidence reviewed October 17, 2026

Public documentation establishes that some Lenovo integration programs exist. It does not establish that the owner's unspecified purchasing account is eligible:

| Official source | What it establishes | What remains unknown |
|---|---|---|
| [Enterprise Pro benefits](https://www.lenovo.com/us/en/enterprisepro/benefits/) | Enterprise procurement integrations describe API/cXML/X12/EDIFACT formats, shipment alerts, order acknowledgements and advanced shipment notifications | Account eligibility, actual data contract, read access, subscription setup and delivery/security details |
| [Supply Chain Open Platform quick start](https://scc.lenovo.com/opendocs/en/common/guide/2_1_quick_start.html) | A supplier/partner platform offers controlled API/FTP access and requires coordination and authorized interface permissions | Whether it serves this buyer account rather than a separate supply-chain partner role |
| [API access process](https://scc.lenovo.com/opendocs/en/common/guide/2_2_integration-api.html) | Partner onboarding assigns client credentials and interface permissions; OAuth client credentials are documented | Whether any relevant read interface can be provisioned for this project |
| [Data push API](https://scc.lenovo.com/opendocs/en/common/data-push/4_4_data_push_api.html) | Lenovo documents partner-provided receiving addresses and Basic/OAuth authentication choices for a push protocol | Buyer-order eligibility, enabled topics, event ordering/replay terms and a compatible sender-authentication contract |
| [Logistics API Portal](https://gtms.lenovo.com/api-portal/) | Separate logistics partner integrations include Lenovo-to-partner HTTPS delivery and coordinated onboarding | Applicability to an ordinary purchasing portal or this account |

These sources do not establish a general self-service purchasing webhook, and this review does not establish its absence. No account settings, private order pages, login flows or live endpoints were tested. The exact account URL/product/region and U01–U04/U09 in [OPEN_GATES.md](intake/OPEN_GATES.md) remain unresolved. The [discovery record](intake/LENOVO_DISCOVERY.md) records this limited public research separately from account findings.

## Common processing path

```text
approved source / scheduled check / authenticated inbound delivery
    → connection-bound adapter and extraction
    → staging, normalization and completeness validation
    → accepted observations + current facts + changes in PostgreSQL
    → deterministic internal events and business conditions
    → recipient-scoped in-app notifications and buyer workflow
    → existing same-origin Workbench API and frontend
```

Keep API, worker and local administration CLI in the existing Go module. A browser runner can be a separately isolated process, but returns the same versioned normalized contract; it does not become a second authoritative business model. Runtime processes use constrained `bwb_app` access and never migrate the database. Schema expansion uses a reviewed one-off migration before the API/worker.

The server owns actor authorization, workspace, generation, connection identity, connector/version, origin and permitted source scope. A connector cannot select a target workspace, relabel data as public/fictional, execute arbitrary URLs/scripts, or grant itself broader scope. Collect outside the short publication transaction, then recheck current membership, generation, connection lease/fence, identity and scope before committing. Retain previous accepted facts when a run fails.

Normalized order records preserve stable source order/line keys, supplier identity, exact decimal strings and UOM, explicit unknown quantities, confirmation availability, source status, date labels/kinds and UTC observation time. A ship date is not an arrival commitment; shipped quantity is not delivered/received quantity. `source_event_at`, when genuinely available, is distinct from collection/receipt/detection time. First observed is not original contractual history.

The current model covers the implemented order-line facts and internal requirements. General inventory, price histories, multiple shipment schedules and richer supplier events require explicit versioned models and validation. Do not squeeze unsupported facts into a single date/status field or treat internal purchase-order receipts as supplier evidence.

### Complete batches and partial events

The current publisher accepts a complete normalized observation batch for its bounded requested scope. It validates binding, page/record counts, required identities, allowed status/date scope, size limits and field meaning before changing accepted state. A successful empty scope needs affirmative completeness evidence. Missing records never imply cancellation, delivery or deletion.

A webhook, single email, incremental API delta or individual order update is generally **partial evidence**. It must not be passed to the complete-batch publisher with `complete=true`, satisfy full-connection freshness, or clear unrelated conditions. The first live push implementation should use the authenticated delivery to enqueue/coalesce a bounded authorized reconciliation through the ordinary collection path.

Direct partial publication is a later contract: add an explicit envelope kind, entity/field presence mask, source version/sequence, account binding and scoped freshness. Distinguish an absent field from an explicitly unknown/cleared field. Validate causal ordering; reject/quarantine contradictory or stale updates; require approved authoritative evidence before lifecycle/risk clearing. Keep full-scope and entity/event freshness separate. Multi-part vendor batches become complete only after every expected part is durably assembled and validated within bounded time/size limits.

### Internal events and notification deduplication

An accepted observed transition such as `processing → shipped` generates a Workbench order-status change event containing version/type, workspace/generation, subject, accepted run/observation, before/after status and detection time. It says the Workbench detected a change; it does not assert the exact shipment time. An initial baseline creates current facts without inventing a transition.

Commit observations, field changes, internal events, condition effects and in-app notifications together. The same accepted run/event replay must not create a second event or notification. A later genuine `A → B → A → B` transition is new evidence and may notify again; deduplicating forever on before/after values would incorrectly suppress it. Preserve the existing condition policy: new, recurring or materially worsening risk notifies; unchanged facts do not repeatedly notify.

Notification identity includes the durable event/material revision plus recipient and workspace/generation. Read/unread state belongs to the recipient. Condition notices select an active assigned workspace member first, then an active default buyer configured on the order's connection, then active workspace members. Status-change notices select the active connection buyer, falling back to active members. Disabled, expired and inactive users are ineligible. These are explicit user UUID mappings; source buyer aliases never establish account identity.

Migration 0011 adds self-scoped workspace preferences for status changes and supplier conditions, both enabled by default. Preferences filter the selected recipients after routing: an assigned buyer's opt-out suppresses their notice without broadcasting it to other buyers. Preference changes affect future notices only; they never alter accepted events, priorities, existing notices or read state, and opting in does not replay history. Preferences survive demo reset. The notification panel lets each member edit only their own preferences, with revision/generation checks and safe retry behavior. Email, Teams or external webhook delivery still requires a transactional outbox, recipient authorization, bounded retries and per-channel delivery uniqueness. Do not claim exactly-once delivery from queue deduplication.

## Future inbound webhook boundary

No webhook route or public callback is enabled by this work. A future endpoint is dedicated to a registered supplier integration, resolves its connection from trusted server configuration, and is separated from browser session/CSRF routes. Callback exposure and network changes require the agreed deployment target and scope.

The following is a proposed signed-webhook design, not a statement that Lenovo supports it:

1. Negotiate the supplier's documented authentication and envelope contract before provisioning a receiver. For an HMAC-capable supplier, verify its specified signature over the exact raw bounded body and signed timestamp/delivery identity, use constant-time comparison and support bounded key rotation. Never invent signature header names or verify a reserialized body.
2. Initial receiver limits should be explicit: at most 1 MiB body, fixed content type/operation, short request/processing deadline, no compressed bodies without a decompressed limit, and per-connection ingress/concurrency quotas. Tune only against the approved vendor contract; these are proposed limits, not deployed settings.
3. For signed current deliveries, a proposed timestamp tolerance is five minutes. A supplier retry may carry an old event time, so distinguish signed delivery timestamp from event occurrence. Reconcile permitted late deliveries using the documented retry protocol; do not simply weaken replay checks. Reject untrusted/future timestamps and unknown signing keys.
4. Persist authenticated receipt and dedupe identity atomically before acknowledging success. Enforce uniqueness on integration/connection plus supplier delivery ID (and part/sequence where applicable). Same ID/same body digest returns the prior outcome; same ID/different digest is quarantined and cannot overwrite facts. Delivery-body hashes alone do not establish business identity.
5. A retained replay ledger must cover the supplier's maximum retry/redelivery horizon plus clock allowance. If that horizon is unknown, keep inbound processing disabled until a bounded policy is agreed. Never expire keys/ledger in a way that makes old signed messages newly actionable. Administrative replay rechecks authorization, generation and current ordering and records an audited replay request.
6. Acknowledge only after durable receipt; parse/process asynchronously with bounded backoff/jitter, a maximum attempt count, redacted failure codes and an owner review/dead-letter state. Reject unsupported methods, malformed envelopes and wrong-account messages without enqueueing a broad refresh. Never follow callback URLs or download payload-supplied attachments.
7. Use source version/sequence where documented; event timestamps alone cannot safely order concurrent updates. Duplicate delivery suppression and business transition deduplication are separate. Acknowledge duplicates without repeating effects and reconcile gaps/out-of-order evidence through a permitted read path.

The public Lenovo push documentation lists Basic/OAuth choices rather than a verified HMAC contract for this account. Do not require a fictitious Lenovo signature. If the chosen supported protocol uses token/client authentication, design its replay ledger, message identity, least privilege and token validation against that actual contract; if authentication or replay protection is inadequate, use a supported alternative/reconciliation path. A source IP allowlist can supplement authentication and is not sender proof by itself.

## Credentials, sessions and isolation

Keep supplier secrets and browser state outside source control, application payloads, fixture records, logs and reviewer artifacts. The [local credential vault](CREDENTIALS.md) now supplies a Go package and `supplier-secret-key`, `supplier-secret-set`, `supplier-secret-status` and `supplier-secret-delete` administration commands. It encrypts an opaque bounded value with AES-256-GCM, binds the envelope to the canonical connection UUID, restricts file/directory permissions and reports metadata without plaintext output. The CLI does not access PostgreSQL or verify workspace ownership, and a stored value enables no collector. No runtime or frontend resolves its references, and no runtime mounts or deployment configuration were added.

Before live execution, select the approved host/key custody and wire trusted private-workspace/account authorization to resolution of that connection's reference. Implement the connector-specific credential/session schema, expiry and revocation. Vault/key backup and recovery are separate from PostgreSQL backup; see [CREDENTIALS.md](CREDENTIALS.md). No default supplier credentials, public registration, reviewer secret upload or session-download endpoint is permitted.

Use dedicated per-connection browser profiles/contexts and normal owner-assisted authentication/MFA. Test session portability, expiry, revocation and restart on the selected host. `login_required`, `challenge_required`, permission denial or unsupported authentication stops unattended retries and requires owner action. Do not bypass MFA/CAPTCHA or expose public browser debugging ports. No browser state is pooled between accounts.

The browser runner needs a non-root sandboxed process, bounded temporary storage/resources, cancellation/deadlines and initially one concurrent real run. Enforce approved destinations/redirects and block host/LAN/metadata access at the network boundary; browser routing alone is insufficient. Permit only reviewed read operations, including semantically read-only GraphQL POSTs; deny purchasing, cancellation, returns, payment/account changes and automatic tracking-link navigation.

Email uses a dedicated narrow mailbox/folder and least-privileged access. Sender authentication checks, header claims and message IDs are evidence, not sufficient order matching by themselves. Quarantine ambiguous order matches, template drift and unsupported attachments. Sanitize HTML without executing content; do not follow remote links or load remote images. Treat extracted email status as partial evidence until validated against an approved source authority policy.

Private source facts remain in the owner's private workspace; source events cannot cross into reviewer workspaces. An owner-approved typed sanitizer projection with independent presentation IDs is the only sharing path. Fictional scenarios use the same ingestion pipeline but preserve fictional origin and their pinned business clock. Reset is generation-scoped, cannot erase private facts/security audit, and fences stale jobs/events. Keep source provenance/freshness visible even where concise product labels omit repetitive qualifiers.

## Scheduling, failures and retention

The implemented scheduler persists cadence/next due time separately for each connection, coalesces overdue refreshes and shares the publication lease fence. Configured cadence must meet that connection's minimum within the 30–86,400-second local bounds. Paused/unconfigured connections and unauthorized or expired scheduling actors cannot schedule. It does not reconstruct changes missed between polls. The 30-second lower bound is a fictional test limit, not permission to poll a real supplier that frequently. Live operation still needs supplier/account quotas, bounded date scope, jitter, a verified approved minimum and a circuit breaker. API/webhook/browser changes cannot silently expand cadence or scope.

Transient network/temporary server errors may retry with bounded backoff and jitter; honor supported rate-limit/retry guidance. Login/challenge, wrong account, permission denial, schema ambiguity and incomplete pagination require classified attention rather than aggressive retries. Record run attempts/outcomes, connector version, collection method, coverage, last accepted success and redacted error code. Failed collection never advances accepted freshness or produces successful shipment metrics.

Current PostgreSQL history retains source transitions and prior demo generations. The [explicit retention commands](RETENTION.md) can plan and apply a bounded cleanup for one workspace using local administration credentials. Apply requires a fixed cutoff at least 24 hours old and explicit confirmation. It clears eligible terminal-run staged payloads and compacts unchanged intermediate observations while preserving first/latest records, every distinct normalized transition and event-bearing evidence. Run ledgers, current/first facts, changes, notifications, notes/actions, audits and idempotency records remain. Planning makes no persisted changes; apply audits its cleanup atomically. No worker, scheduler or runtime role invokes maintenance automatically.

This compaction is not full personal-data erasure, supplier-session revocation or an active live-data expiration policy. Encrypted backup tooling retains the latest snapshot on each of the fourteen most recent backup dates; backup retention is separate. Version 8, 9 and 12 dump/restore compatibility has been exercised on disposable fictional databases, including preserved observations/events and private preparations. Those tests use separate temporary databases and access no project backup or vault key. Actual deployment and operational backup recovery results belong in BUILD_STATUS. See [RETENTION.md](RETENTION.md), [CREDENTIALS.md](CREDENTIALS.md) and [BACKUP.md](BACKUP.md).

Before live activation, agree the actual retention policy and verify its recovery implications. Proposed starting points are 90 days for minimized real observations, 30 days for operational logs and 24 hours for explicitly authorized diagnostic artifacts; these are proposals, not active enforcement or compliance requirements. Diagnostics remain off by default. Supplier receipt bodies and replay identities need separate policies when those transports exist, so payload expiry cannot permit repeated effects. Keep only necessary private evidence, and treat backups containing it as private.

## Delivery sequence and acceptance

1. **Local preparation implemented:** multiple connection identities, connection-scoped source facts/requirements and validation, fictional JSON source staging, per-connection schedules/pause fences, private unconfigured method settings, routed in-app notifications/preferences, a local encrypted vault and explicit retention compaction. The current publisher continues to require complete bounded batches. Executed checks and outstanding verification belong in [BUILD_STATUS.md](BUILD_STATUS.md).
2. **Release and recovery verification:** record current database/browser and populated-upgrade checks, retain the verified 8/9/12 dump/restore compatibility tests, and verify constrained runtime permissions during each release. Migrations run once through local administration before updated API/worker startup. No live-source result can be inferred from fictional acceptance.
3. **Deferred authorized Lenovo discovery:** complete [LENOVO_DISCOVERY.md](intake/LENOVO_DISCOVERY.md), establish the exact account, read scope, permitted cadence and supported method, then implement the verified account/field map and prove one real order privately. Record unavailable fields rather than invent them.
4. **Bounded private operation:** integrate trusted vault resolution and normal authentication/session recovery, add the isolated runner and approved egress if the chosen method requires one, and verify wrong-account, expiry, pagination, limits, schema drift and failure preservation. Enable only the approved schedule after those gates pass. Share only an explicitly approved sanitized snapshot; maintain the independently resettable fictional demonstration.
5. **Additional transports when scoped:** implement a verified API/push/browser/scrape/email adapter only against an approved source contract. Direct partial ingestion and external notification delivery remain separate work requiring explicit models, ordering, authorization, replay and delivery checks.

No stage is evidence that a later live check passed. Database, browser-runner and live-source checks must each be reported separately, including skips. Reviewer access continues to depend on the authentication, isolation, sanitization and HTTPS acceptance gates.
