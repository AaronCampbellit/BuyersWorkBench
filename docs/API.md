# Application API

The authenticated runtime handler catalog and reflected Go response schemas are at `GET /api/v1/openapi.json`. The catalog is generated from actual route registration; this document supplies mutation semantics. Existing authentication and workspace routes keep the `/api/` prefix for compatibility.

All data responses are JSON and no-store. JSON mutations default to a 4 KiB body limit; note, supplier and purchase-order mutations allow 32 KiB to accommodate up to 4,000 Unicode characters of plain-text notes. Connection settings and notification preferences allow 8 KiB; fictional source staging allows 4 MiB. Every mutation requires the exact configured Origin and `X-CSRF-Token` from the current session. Cookies are same-origin. Decimal quantities/costs are strings. Business dates are YYYY-MM-DD; operational instants are UTC RFC3339. Unauthorized and missing scoped records return404. Conflicts return409 with a stable code; invalid domain values return422. Do not depend on raw database errors; responses do not contain them.

## Authentication

| Route | Input/result |
|---|---|
| `POST /api/login` | `{email,password}` → `{user,csrf_token}` and opaque session cookie |
| `GET /api/session` | Current identity and CSRF token;401 when expired/revoked |
| `POST /api/logout` | Revoke current session;204 |
| `POST /api/password` | `{current_password,new_password}`;14–1024 characters, different password; revoke other sessions and clear mandatory first-use flag |
| `POST /api/reauth` | `{password}`; credential confirmation for 5 minutes;204 |
| `GET /api/workspaces` | Membership-authorized workspace list |
| `GET /api/workspaces/{id}` | Scoped workspace metadata |

Temporary-password accounts may access session/password/logout only until changing the password. Reviewer expiry and disable apply to login, existing sessions, domain RLS, jobs, assignees and notifications. Privileged provisioning/disable/expiry/reset are local CLI operations, not public registration endpoints.

## Workspace routes

The prefix below is `/api/v1/workspaces/{w}`. The server verifies membership before trusting the requested workspace. Every mutation below supplies positive `generation`; optimistic edits also supply `revision`. Read the current values again after a409. The UI aggregate is bounded to 500 orders/conditions/suppliers/internal purchase orders, 50 connections, 20 recent runs, 200 history/notes/issue events per order, and 100 notifications; the v1 controlled seed has 4 lines. This is a small demonstration application, not an unbounded ERP query API.

| Route | Input/result |
|---|---|
| `GET /overview` | `workbench.Overview`: generation, seed_version, ready/state, business_date, provenance, orders, priorities, suppliers, quotes, connections, runs, notifications |
| `GET /orders/{id}` | Order, explicit internal requirement, accepted changes, conditions, notes and issue_events |
| `GET /orders/{id}/history` | Accepted observed transitions |
| `GET /purchase-orders` | `{generation,purchase_orders}`; newest 500 internal orders, independent from supplier observations |
| `GET /purchase-orders/{id}` | `{order,events}`; current internal order and latest 200 immutable events |
| `POST /purchase-orders` | `{generation,idempotency_key,...PurchaseOrderInput}` →201 internal draft |
| `PATCH /purchase-orders/{id}` | `{generation,revision,...PurchaseOrderInput}` →200; complete editable draft terms |
| `POST /purchase-orders/{id}/actions` | `{generation,revision,idempotency_key,action,quantity,reference,note,confirm}` →200 internal order |
| `POST /quotes/{id}/purchase-order` | `{generation,revision,idempotency_key}` →201 draft from current eligible offer; revision is the quote comparison revision |
| `GET /priorities`, `/orders`, `/suppliers`, `/connections`, `/notifications`, `/runs` | Bounded lists with `limit=1..100`, `next_cursor`, generation and origin |
| `GET /quotes` | Inputs, exact server calculation and comparison revision |
| `GET /runs/{id}` | Current-generation run state, attempts, safe failure code and counts |
| `POST /suppliers` | `{generation,name,contact_name,email,phone,website,notes,status}` →201 Supplier; owner only in own presentation/private workspace |
| `PATCH /suppliers/{id}` | Same profile fields plus `{generation,revision}` →200 Supplier; owner only; stale generation/revision returns409 |
| `POST /seed` | `{generation}`; initialize seed/source and enqueue baseline,202 Run |
| `POST /refresh` | `{generation}`; coalesce an active lease or enqueue collection,202 Run |
| `POST` or `PATCH /schedule` | `{generation,enabled,cadence_seconds}`; owner only;30–86400 seconds for the fictional connector |
| `POST /demo/scenarios/{key}` | `{generation,idempotency_key}`; source mutation then202 Run |
| `POST /demo/reset` | `{generation,idempotency_key,confirm:true}` → new generation, seed_version and baseline Run |
| `PATCH /exceptions/{id}/workflow` | `{generation,revision,acknowledged,assignee_id,follow_up_date,status}`; nullable assignee/date; edit open/in_progress tasks, or retain a closed/waiting status while editing assignment; structured resolution/reopening is required for closing/reopening |
| `POST /exceptions/{id}/notes` | `{generation,revision,body}`; plain text, no HTML execution;201 Note |
| `POST /exceptions/{id}/resolve` | Structured closeout plus generation/revision/idempotency key;200 updated Exception; details below |
| `POST /exceptions/{id}/reopen` | `{generation,revision,idempotency_key,reason}`;200 updated Exception |
| `PATCH /quotes/{id}` | `{generation,revision,quote:{id,supplier,unit_price,freight,other_charges,approval,arrival_date,uom,currency}}` → new Quotes and calculation; supplier identity stays server-owned |
| `POST /notifications/{id}/read` | `{generation}`; current recipient only;204 |
| `GET /snapshots` | Approved public projections only, separate collected/exported/seeded times |

Lists accept `supplier`; priorities additionally accept `state=active|resolved|all` and `severity=high|medium|info`. Cursors are opaque and signed against the current session, workspace, generation, view and filters. Retain the same filters while paging; a changed generation or removed anchor requires a reload. Aggregates/list windows are intentionally bounded as stated above; no omitted records are claimed as a complete large-account export.

Scenario keys are SC01–SC08, or baseline/delay/partial/confirm/failure/quotes/repeat/recovery. Idempotency-Key header may replace the body key. Keys are scoped to actor, operation, workspace and generation; reusing a key with a different payload conflicts. SC06 resets quote inputs; supplier collection still uses the common pipeline. Reset retains security audit/private data, preserves approved snapshot projections, and fences all old-generation writes. `ready:false,state:initializing` remains visible until baseline publication succeeds.

## Issue resolution

`POST /exceptions/{id}/resolve` requires `generation`, `revision`, `idempotency_key`, `resolution_type`, `action_taken`, `result` and `reference`. Resolution types are `confirmation_received`, `delivery_updated`, `shipment_updated`, `data_corrected` and `other`. Optional string fields are `supplier_contact`, `revised_date` and `confirmed_quantity`; `follow_up_date` is required while the calculated condition is active. Dates are date-only values, quantities are exact nonnegative decimals with at most four fractional places. Text limits are4000 characters each for action/result,500 for reference and200 for contact.

The200 response is the updated Exception. Active source risk yields `awaiting_verification`; already cleared and verifiable risk yields `resolved` and `resolved_at`. `resolution` contains the submitted details, submitting person/time, material revision, quantity unit and condition evidence. An accepted source update can verify waiting work; recurrence or worsening reopens it. Unknown facts cannot establish verification. An inactive condition without sufficient current source proof returns422 `verification_required`. Automatic verification/reopening events include the current accepted source evidence. Buyer claims do not mutate source facts or purchasing receipts.

`POST /exceptions/{id}/reopen` requires `generation`, `revision`, `idempotency_key` and a nonblank `reason`. Both actions accept `Idempotency-Key` as a header fallback. Repeating the same accepted request returns its original result; changing a request under an accepted key or using a stale revision/generation returns409. Invalid/missing resolution data returns422; generic workflow completion/reopening bypasses return `resolution_required`.

Order detail includes `issue_events`, separate from supplier change `history` and buyer `notes`. Events record the action, before/after status, actor/time and evidence; automatic source events are identified separately. Reads and actions retain existing session, Origin/CSRF, membership and current-generation constraints. Reviewer actions apply only to their own sandbox. Event history is append-only for the runtime role. Old `completed` tasks remain available as work needing resolution details.

## Supplier directory

Supplier responses and list entries contain `{id,alias,name,contact_name,email,phone,website,notes,status,revision,origin,order_count,active_conditions,coverage_label}`. `alias` is an immutable server-owned source matching key; editing the display name never changes order or requirement associations. IDs, aliases, origin and counts are not accepted as profile input. Reviewers may read their own supplier directory but cannot create or edit profiles; denied scoped writes return 404.

`name` is required with 1–100 characters after trimming. Optional contact name and phone allow 100 and 50 characters; notes allow 4,000 characters as plain text. Email must be a bare address of at most 254 bytes; website must be an absolute HTTP/HTTPS URL without credentials, at most 2,048 bytes. Text controls are rejected except newlines/tabs in notes. Create defaults an omitted/empty status to `active`; edits require `active` or `inactive`. PATCH submits the complete editable profile: omitted optional text fields become empty. Both mutations are bounded to 32 KiB and require current generation, Origin and CSRF; PATCH also requires the returned revision.

Profiles persist through refresh and repeated seed/collection runs. Counts derive from accepted current-generation observations; a newly entered supplier has zero purchasing activity and no invented performance metric. A private profile has `origin:manual_private` and is usable without a connector; the workspace still reports `no_supplier_data` while no source is configured. Contact metadata does not initiate requests or supplier operations. Presentation profiles have `origin:fictional`.

Demo reset creates a new generation containing the pinned seed profiles, discarding current visibility of manual additions and profile edits in that demo workspace. Reload IDs/revisions after reset. Other workspaces, private contacts and security audit remain intact. Supplier contacts are not copied into reviewer seeds or sanitized snapshots.

## Quotes and internal purchasing lifecycle

Quote `approval` is supplier eligibility, and the calculated winner is a recommendation. Conversion explicitly creates an internal draft; it does not place a supplier order. `overview.purchase_orders` contains internal orders separately from accepted `overview.orders`. Private internal orders are available without a source connection or fictional seed.

`PurchaseOrderInput` is `{supplier_id,description,quantity,uom,currency,unit_price,freight,other_charges,required_date,notes}`. Supplier must be an active profile in the current workspace/generation. Description is required, maximum 500 characters; unit is required, maximum 24; notes allow 4,000 plain-text characters. Currency is USD in this version. Quantity must be positive; all three cost fields must be known nonnegative decimals, at most 18 integer and four fractional digits. Required date is YYYY-MM-DD. Authoritative totals use exact decimal arithmetic and preserve fractional cents. There is one item per internal order.

Internal order responses include the input plus `{id,number,generation,supplier_alias,supplier_name,origin,status,revision,received_quantity,placement_reference,modeled_total,source_quote_id,quote_source,allowed_actions,created_at,updated_at}`. Supplier name/alias are saved order snapshots. `quote_source` is null for manual drafts, otherwise immutable `{comparison_id,revision,quote,quantity,description,required_date,excluded_costs}`. Editing a quote afterward cannot change the snapshot or saved order terms. Stable seed quote IDs bind to explicit source supplier aliases, never editable names. One conversion exists per quote per workspace generation; a new request key returns the existing order instead of duplicating it.

All purchasing mutations require the workspace owner in their own private/presentation workspace. Reviewers can read their own records. Create/conversion/actions require an 8–128 byte idempotency key; retain it for an exact retry, and use a new key for a changed payload. Reusing a key with changed content conflicts. Current generation and revision checks fence stale edits. Repeated receipt requests cannot count goods twice. PATCH is draft-only; use `revise` to return an approved order to draft before editing.

| Action | Allowed source → result | Required evidence |
|---|---|---|
| `approve` | draft → approved | Active supplier and complete validated terms |
| `revise` | approved → draft | Current revision |
| `place` | approved → placed | `confirm:true` acknowledging placement outside the app; nonempty external `reference` |
| `receive` | placed/partially_received → partially_received or received | Positive incremental `quantity`, nonempty receipt `reference`; cumulative quantity cannot exceed ordered quantity |
| `close` | received → closed | Full ordered quantity explicitly received |
| `cancel` | draft/approved/placed/partially_received → cancelled | Nonempty reason in `note`; prior receipts are preserved and the remaining portion is cancelled |

References allow 100 characters, notes 4,000. Invalid transitions return422; stale revisions/generations return409. Closed and cancelled orders have no further actions. `allowed_actions` exposes the state machine; the server validates each action independently. Every create, edit and transition appends an event with actor, time, prior/new state and receipt/reference/reason where applicable. Collection never modifies internal orders. Demo reset hides that demo generation's purchase orders/events and leaves private orders intact. None of these endpoints sends a supplier request, email, cancellation, or inventory/accounting posting.

Accepted supplier orders retain their original source status and quantities. The derived `order.lifecycle` response contains `{fulfillment_state,confirmation_state,cancellation_state,receipt_state,net_ordered,unshipped,unreceived,evidence,issues}`. Fulfillment is quantity-derived or explicitly labeled source-status evidence; contradictions are reported and unknown balances remain empty strings. `receipt_state:unavailable` prevents interpreting supplier delivery as an internal receiving record. Follow-up task completion never changes either order lifecycle.

## Internal CSV import

Owner only. The prefix is the same workspace prefix.

1. `GET /imports/internal-orders/template.csv` downloads the fictional reference dataset.
2. `POST /imports/internal-orders/preview`, Content-Type text/csv, uploads at most 1 MiB/1,000 rows. Required columns: internal_po,line,supplier_alias,internal_part,quantity,uom,required_arrival_date,source_order_key. Optional buyer_alias defaults to demo_buyer. Source keys/suppliers/units must match accepted fictional records; EA is the current unit. Ambiguous dates, formulas, unknown headers, invalid decimals, duplicate mappings and unknown buyers are rejected as a whole.
3. Preview returns `{id,generation,requirements,row_count}` without changing current requirements. Preview expires after 1 hour.
4. `POST /imports/internal-orders/{id}/publish` with `{generation,confirm:true}` atomically consumes the preview, replaces the explicit mappings, audits and enqueues reprocessing. Repeating the same preview returns the same run. A reset invalidates an old-generation preview.

## Sanitized private projections

These routes are owner-only and require a login/password confirmation within 5 minutes. No real source exists yet; synthetic privacy canaries test the implementation, not Lenovo integration.

`POST /api/v1/admin/sanitized-exports/preview` accepts `{workspace_id,source_record_id,generation,safe_description,include_quantities,include_status,include_arrival}`. The workspace must be the owner's private workspace and the accepted record origin must be live_private. A typed allowlist projection gets an independent public UUID; no source identifiers/URLs/labels, raw payload, private lineage, price, serial, account or contact values are serialized. The owner supplies a generic reviewed description.

`POST /api/v1/admin/sanitized-exports/{id}/publish` accepts `{target_workspace_id,generation,approve:true}`. Approval is explicit, generation-locked, idempotent and audited. It inserts only the safe projection into the chosen presentation/reviewer workspace. The private export mapping remains owner-only. Published snapshots are separate immutable facts; fictional scenarios never mutate them or claim they are live-connected records.

## Connector and jobs

`internal/collection` defines an immutable connector registry with fixed key/version, method, authentication-reference type and origin mode. Its registered fictional adapter is the only enabled collector. `internal/domain` validates the normalized original wire contract, exact quantities, dates, declared completeness and server-side run/account/workspace/generation binding. Connection bindings and permitted scope come from trusted configuration; adapter registration alone cannot authorize supplier access. Collection stages bounded batches, quarantines rejected input, preserves accepted current data on failure, and publishes observations/changes/conditions/notifications transactionally after lease and generation checks. River jobs carry identifiers and fences, never passwords, cookies, arbitrary URLs, SQL or scripts.

Order history now exposes canonical source events: existing change fields plus `event_type` (`order.<field>.changed`), `schema_version:1`, `run_id`, `connection_id` and `observation_id`. `id` is the stable event identity. Events retain before/after semantics and `detected_at`; first observations generate no change event. Runtime processes may append accepted changes but cannot update/delete this history.

Notifications add `source_event_id` and `order_id`. Condition notifications retain `exception_id` with an empty `source_event_id`; source-event notifications have an empty `exception_id` and a populated `source_event_id`. Both have an order reference. Status transitions generate source-event notices for active nonexpired workspace members; other fields remain history and can drive condition notifications under the existing policy. Notices are recipient-scoped, replay-deduplicated and committed with accepted source facts. The existing read action applies to either kind and does not alter an order or resolve an issue. No historical notices are generated by migration 0009.

There is no enabled Lenovo connector or arbitrary browser execution endpoint. The future source adapter must pass the same binding/publication checks after actual site discovery. Persisted application schedules demonstrate fictional reconciliation across process restarts; real supplier cadence remains disabled.

Webhook, API, browser, public-page and email method descriptors are modeled, but their live transports, credentials/session stores and any callback routes remain unimplemented. A sparse event/email cannot enter the complete-batch publisher as a full account snapshot. See [collection strategy](COLLECTION_STRATEGY.md) for discovery, reconciliation and future connection-scoped contracts.

## Connection management and source identity

Owner connection controls use the existing workspace prefix. Reviewers can read their own connection summaries and retain the legacy default-demo refresh/scenario actions, but cannot create, edit, pause or schedule connections.

| Route | Input/result |
|---|---|
| `POST /connections` | `{generation,idempotency_key,...ConnectionInput}` →201 connection; at most50 per workspace, with capacity reserved for an uninitialized default demo source |
| `PATCH /connections/{id}` | `{generation,revision,...ConnectionInput}` →200 connection |
| `POST /connections/{id}/refresh` | `{generation}` →202 run; an active matching run coalesces |
| `PATCH /connections/{id}/pause` | `{generation,revision,paused}` →200 connection; pause cancels/fences active work and disables that connection's schedule |
| `PATCH /connections/{id}/schedule` | `{generation,revision,enabled,cadence_seconds}` →200 connection; configured minimum through86400 seconds |
| `POST /connections/{id}/source` | `{generation,revision,idempotency_key,records,requirements}` →202 run; owner-only additional fictional feed, never default/private/live |
| `GET /members` | `{members:[{id,display_name}]}`; active authorized workspace members only, without email addresses |

`ConnectionInput` contains `name` (trimmed1–100 characters), nullable `supplier_id`, nullable `buyer_user_id`, `method`, `minimum_cadence_seconds` and `cadence_seconds`. Methods are `fictional`, `api_polling`, `webhook`, `browser`, `public_scrape` and `email`. Minimum interval is30–86400 seconds; cadence must meet it. Zero/omitted interval values use server defaults. A supplier reference must be an active profile in the current workspace/generation; a buyer reference must be an active, unexpired workspace member. PATCH sends the complete editable configuration. Method identity remains fixed after creation; the default demonstration has multiple seed suppliers and cannot bind to a single supplier.

Connection responses preserve existing source health fields and add `{name,supplier_id,buyer_user_id,method,revision,paused,minimum_cadence_seconds,is_demo_default,runnable}`. `runnable` describes an installed collector, while `paused` independently controls execution. Resume does not enable a schedule or claim collection success. Each run, order and requirement mapping belongs to its connection. Order responses include `connection_id`; the Orders table/evidence display its name and search includes it. `source_key` and original order IDs remain unchanged and can repeat across connections. CSV imports target the default connection unless preview specifies `?connection_id=UUID`; the preview retains that binding through publication. Validation queries the selected connection directly rather than relying on the bounded UI overview.

Non-private workspaces permit fictional feeds. Private workspaces permit pending settings for the other five methods with `connector:unconfigured`, `origin:live_private`, `status:not_configured`, `runnable:false`. These entries cannot execute, enable schedules or acquire successful timestamps. Creating them supplies no authentication material and performs no network access. There is no public secret/session upload field or arbitrary URL/script configuration.

Fictional source staging accepts normalized `domain.Record` objects and an optional requirements array, with fictional origins, distinct stable order/line keys, precise quantities/units and declared date meanings. It validates mappings, a selected supplier alias, identities and suspicious record-count drops. Additional feeds allow all normalized statuses, including shipped/delivered; the default scenario scope remains pinned. Source/requirement changes, generation/revision checks, lease supersession, run creation, audit and retry key commit together. Source acceptance is not publication success: poll the returned run. A repeated accepted key/payload returns its original run even after the source revision advanced. Changed content under that key conflicts. The UI retains its file on failure and uses a new key only when an explicitly reviewed revision changes the request.

Demo reset advances the workspace generation, fences old work and restores only the default seed. Fictional connection IDs, names, methods, cadence values and default-buyer mappings remain; pause flags are cleared, schedules are disabled and next-due/collection-health state is cleared. Additional fictional source payloads are emptied, and generation-scoped supplier bindings are cleared as supplier profiles reset. It does not copy the default seed into every connection. Private connections and notification preferences are unaffected.

## Notification preferences and routing

`GET /notification-preferences` returns `{status_changes,supplier_conditions,revision}` for the current recipient and workspace. Defaults are both true with revision0 until changed. `PATCH` accepts `{generation,revision,status_changes,supplier_conditions}`; both boolean fields and revision are required. Stale changes conflict; an exact retry of the last accepted update is safe. Preferences survive demo reset, affect future notices only and never erase event history, change conditions or backfill old notices.

Condition notices route first to an eligible condition assignee, then an eligible connection buyer, otherwise eligible active workspace members. Status notices use the connection buyer, then active members. Recipient preferences are applied after routing: an assigned buyer who opts out does not cause a broadcast to other members. Inactive/disabled/expired assignees fall through to the next eligible routing choice. Reads remain recipient-scoped and history remains visible under workspace/generation rules. These settings control in-app notices; external email, chat and webhook delivery are not implemented.

The encrypted credential vault and explicit retention commands are local administration capabilities rather than HTTP endpoints. See [credentials](CREDENTIALS.md) and [retention](RETENTION.md). Vault references are not resolved by the current collector runtime.
