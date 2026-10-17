# BuyersWorkBench project

Created October 16, 2026. A learning project for purchasing workflows, reliable data ingestion and event-driven automation.

BuyersWorkBench turns validated supplier observations and explicit internal requirements into buyer follow-up work. It is an independent project built around explicitly fictional examples and interchangeable supplier connections.

Development currently focuses on the supplier-independent collection and purchasing foundation. Live Lenovo collection is deferred. The exact portal and permitted account scope remain pending; public documentation research is recorded separately from live discovery. The original planning archive is preserved unchanged locally and excluded from public distribution.

The implemented application has Buyer Priorities, Orders, Supplier Performance, Quote Comparison, Data Health, scenarios, notifications, and persistent buyer actions. Go serves the API and River worker; PostgreSQL holds source revisions, runs, observations, conditions, workflow, quotes, internal purchase orders, schedules, and scoped reviewer sandboxes. React uses the same-origin session API. The fictional connector runs through real validation, comparison, and publication transactions.

The local source now supports named supplier/account connections with independently scoped orders, requirements, schedules, pause fences and collection history. Owners manage these from Data Health, upload clearly fictional JSON fixtures into additional demo feeds, and prepare private connection settings while live collection remains unavailable. Source keys are unique within a connection, preserving identical supplier order numbers across accounts. The original default seed/scenarios retain their identities. Accepted changes are typed append-only source events; status transitions create deduplicated in-app notifications with links to order evidence. Connection buyer routing and per-user notification preferences are implemented. Local administration tooling provides an encrypted credential/session vault and explicit bounded retention compaction; nothing provisions real supplier material or runs cleanup automatically.

This source has twelve application migrations. See the current deployment and verification state in [BUILD_STATUS.md](docs/BUILD_STATUS.md). Lenovo order collection remains deferred. See the [collection strategy](docs/COLLECTION_STRATEGY.md), [Lenovo discovery record](docs/intake/LENOVO_DISCOVERY.md), [connection decision](docs/decisions/0009-connection-management.md), [credential vault](docs/CREDENTIALS.md) and [retention operations](docs/RETENTION.md). Real transports, account authentication/session renewal, browser-runner isolation, partial webhook ingestion and external message delivery remain future work.

The owner can add and edit supplier names, contacts and notes in their presentation/private workspace. Profiles survive refresh; immutable source aliases preserve order matching. Manual suppliers start with zero purchasing activity, private contacts need no live connector, and reviewers have read-only access to their own directory. Demo reset restores that workspace's pinned supplier seed and leaves private contacts intact.

Product labels use concise supplier, component and quote names without repeating the sample-data qualifier. Stored source identities and purchasing snapshots remain intact; see the [product language decision](docs/decisions/0006-product-language.md).

Priorities provides a structured issue closeout with action, outcome, supporting reference and verification follow-up. Active source risk awaits verification; accepted clearing evidence resolves waiting work, and recurring or worsening risk reopens it with history. See the [issue resolution decision](docs/decisions/0007-issue-resolution.md).

Quotes are pre-order offer comparisons. Eligible offers can explicitly create internal draft orders; the owner can also enter drafts manually. Internal orders progress through approval, recorded outside-app placement, explicit partial/full receipts, and closure or cancellation, with revision checks and event history. Supplier observations remain a separate Orders area, with fulfillment/confirmation evidence independent from internal receipts and buyer task completion. These local records send nothing to suppliers. See the [purchasing lifecycle decision](docs/decisions/0004-purchasing-lifecycle.md).

| Milestone | Implementation status |
|---|---|
| M0 foundation | Implemented and verified locally; owner provisioned; environment and unresolved inputs recorded |
| M1 common pipeline | Fictional collection, validation/quarantine, observations/history, and sanitizer preview/approval implemented; live Lenovo discovery and one-order proof explicitly deferred |
| M2 access and ingestion | Session lifecycle, reviewer CLI, expiry/disable, workspace RLS, durable queue/schedules, fenced publication, CSV preview/publication, bounded reads implemented |
| M3 buyer actions | Rules R01–R05, queue/details, persistent workflow/notes, notifications and source health implemented |
| M4 demonstration | SC01–SC08, generation-aware reset, quote edits/calculation, supplier coverage, independent reviewer seeds and approved snapshot copying implemented |
| M5 operations | Owner-only HTTPS deployment, encrypted local backup and isolated restore tooling; current executed release evidence and remaining limits are in BUILD_STATUS.md |

Live Lenovo collection and browser-runner isolation remain a separate future gate. There is no enabled supplier browser, supplier credential, external messaging, or live supplier data in this build. A fictional scenario never closes that gate. External reviewer access is not enabled in the Cloudflare Access policy; the current hostname remains owner-only.

The source model preserves exact decimal strings, date-only semantics, unknown receipts, first-observed history, and field origin. Partial shipment is not labeled shortage; on-time supplier performance is unavailable without actual receipts and an appropriate baseline. Quote costs are modeled USD/EA costs, with excluded charges stated.

Start and operate the project from [README](README.md). See [executed evidence](docs/BUILD_STATUS.md), [API contract](docs/API.md), [operations](docs/OPERATIONS.md), [tunnel](docs/TUNNEL.md), and [open inputs](docs/intake/OPEN_GATES.md). The public documentation describes the implemented contracts and remaining work; the original local archive is optional reference material.
