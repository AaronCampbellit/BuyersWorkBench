# Lenovo browser collector

This standalone Node 24/TypeScript CLI collects the authenticated Lenovo Pro US account's visible orders from the last **90 calendar dates, including today**, using the configured account timezone. It uses Playwright and the website's observed JSON responses. It needs no agent, language model, MCP server, Chrome extension or per-run human navigation. Initial session provisioning and interactive login/MFA fallback remain operator tasks.

It is separate from the deployed Go API/worker. It writes encrypted, minimized observations outside the checkout; it does not publish a Go `domain.Batch`, change database configuration or activate recurring collection. See [the collection strategy](../../docs/COLLECTION_STRATEGY.md) and [discovery evidence](../../docs/intake/LENOVO_DISCOVERY.md).

For a fresh machine, private-state transfer, completed expiry findings and ordered integration work, start with [the Lenovo handoff](../../docs/LENOVO_HANDOFF.md). A clone can run all fixture checks without supplier access; live collection requires separately provisioned private state. [config.example.json](config.example.json) contains an invented template, not a usable account.

## Run once

The host needs Linux, a non-root operator with Docker access, Python 3, and Node 24.19 or newer in the Node 24 series. Install locked service dependencies with npm:

```sh
cd services/browser-runner
npm ci
npm run check
python3 run-isolated.py --private-dir /absolute/path/outside-the-checkout/lenovo-private
```

The wrapper runs the official Playwright 1.64.1 image pinned by digest. Chromium runs headed under bundled Xvfb with its sandbox enabled. The non-root container has a read-only root, tmpfs browser files, dropped capabilities, resource/deadline bounds and `--network none`. Its only outbound path is a temporary Unix-socket CONNECT proxy for exact reviewed Lenovo/static hosts on port 443 with public-address validation. Browser policy further restricts API paths and permits only the reviewed dictionary and normal renewal POSTs. No public port, database credential or Docker socket is mounted in the container. A directory lock prevents overlapping collectors; cleanup stops the container and proxy.

The checked-in seccomp profile is the upstream Playwright Docker profile used in the earlier isolated probe, with namespace-local `chroot` additionally permitted so Chromium can start with capabilities dropped. Updating Playwright requires reviewing the matching image, profile and live behavior together.

Provision this private directory from an authorized encrypted capture. Directories require mode 0700 and files 0600, owned by the operator. Never put real configuration or sessions in the checkout:

```text
lenovo-private/
  credentials/                  mounted read-only
    config.json
    key.pem                     RSA private key, at least 3072 bits
    session-import.enc          encrypted initial session
  state/session.enc             created after a verified run
  data/
    snapshots/<run UUID>.enc    immutable encrypted observations/changes
    runs/<run UUID>.json        redacted attempt metadata
    last-success.json           atomic accepted snapshot pointer
    last-attempt.json           latest redacted attempt
  egress/                       temporary proxy socket
```

The private configuration binds an independently confirmed account, not an identity selected by incoming records. Replace the placeholder:

```json
{
  "schema": 1,
  "customerId": "REPLACE_WITH_CONFIRMED_CUSTOMER_ID",
  "timezone": "America/Chicago",
  "pageSize": 25,
  "maxPages": 100,
  "maxOrders": 1000,
  "timeoutSeconds": 300
}
```

Unknown configuration fields are rejected. Page size is 1–100, pages 1–100, source orders 1–10,000, and timeout 30–600 seconds. There is an additional 660-second process bound. Limits never turn a truncated result into a successful snapshot. No supplier URL or secret is accepted through arguments/environment.

The initial capsule may use the explicitly imported `lenovo-vm-probe:v1` context. Updated sessions use `buyersworkbench:lenovo:session:v1`; observations use a distinct authenticated context. Both use AES-256-GCM with fresh nonces and RSA-OAEP-SHA256 wrapped data keys. Session plaintext stays in the process and temporary browser filesystem. The existing Go connection vault is not yet wired to this CLI.

## Collection contract

1. Restore the saved session, or use the encrypted import when no saved session exists. Invalid saved state fails closed.
2. Open the fixed orders page and use Lenovo's normal SSO flow when needed. Authentication recovery allows one fresh collection attempt without filling credentials or submitting challenges.
3. Clear status/search/period filters and traverse the entire visible account list. Validate page numbers, totals, size, version and duplicate identities. The observed endpoint has period buckets rather than an exact 90-date filter, so the complete list is filtered locally; older rows are not persisted. Configured limits bound this traversal.
4. Fetch each selected order's details through fixed endpoints. Validate customer/order/ecommerce identities, shapes, exact quantities and date formats.
5. Repeat the full list traversal and compare membership, placement dates and source status. Changed/truncated lists fail. Observations cover an interval, not a transactional instant at Lenovo.
6. Compare with history. Unexplained missing in-window orders, missing lines or disappearing shipment coverage fail; aging out of the moving window is expected.
7. Save refreshed session state encrypted, write the encrypted observation, then atomically replace `last-success.json`. Schema/authentication/completeness failures retain accepted facts. Storage errors during the final pointer replacement require inspecting that pointer because an OS error after rename may follow an accepted replacement.

Transient reads use at most three attempts with bounded backoff. Short `Retry-After` values are honored; longer limits stop the run. Authentication/challenge, wrong account and schema errors do not enter that retry loop. Errors contain fixed codes, not provider exception text, tokens, response bodies or private URLs.

## Stored facts and unknowns

`lenovo_orders.v1` contains account-bound scope/coverage, source order identities and placement dates, raw order status codes, and line/component/split records. Lines preserve source IDs, product number/description, exact decimal quantities, source status and product type. Missing quantities remain null; units are not invented. Components/services remain separate from physical shipments.

Each shipment record keeps its carrier/tracking/source-delivery identifiers, quantity, ETA and actual delivered date. A forecast row in `shippingStatusVOList` does not establish shipment; a service's `Fulfilled` status does not establish delivery. Separate ETAs are never collapsed into one order date. Numeric IDs beyond JavaScript's safe integer range and fractional quantities retain the original JSON token. Keys use explicit source identities, never array positions. Anonymous forecast rows keep a null shipment identity; vendor ID stability across lifecycle transitions still needs validation.

Dates retain the literal source calendar day. Estimated delivery/ship dates and actual delivered dates remain separate. `upsDeliveryDate` is stored as `carrierReportedDate` with unverified semantics, not actual delivery. A `Get it by Thu. Oct 22` label lacks a year: it is retained in `estimatedArrivalLabel`, with a null parsed date and `arrivalYearMissing: true`. No year, commitment or receipt is fabricated.

Each order also exposes `linkedTrackingInformation` for an eventual **Linked tracking information** view. Entries contain the reviewed HTTPS URL, carrier, tracking number, source provenance and exact line/shipment keys. The same link appears in its shipment's `trackingLink`; absent, invalid, mismatched or unreviewed source links have an explicit status and no clickable URL. The current policy supports the two observed UPS legacy query formats, verifies their number against the shipment, removes query decoration and upgrades HTTP to HTTPS. Unknown hosts, paths, credentials, fragments, duplicate or unknown query parameters and mismatched package numbers cannot become links. No URL is guessed from a tracking number alone.

The allowlist excludes addresses, email/customer names, payment details, pricing, recommendations and serial numbers. Tracking URLs and identifiers remain private, encrypted outside the checkout. Runtime extracts links from the Lenovo detail JSON and never contacts UPS. Carrier-site changes or downtime cannot fail an otherwise complete order pull. A separate owner-authorized VM inspection opened UPS without Lenovo cookies and observed tracking details, but repeated carrier loading was inconsistent; automatic UPS status/ETA enrichment remains unimplemented. Lenovo shipment dates remain labeled as Lenovo observations.

No raw response diagnostic, screenshot or HAR is written. The encrypted session document separately retains the provider-owned browser storage needed for restart. Captures are immutable; a baseline creates no fabricated transition and repeated unchanged facts create zero changes. Connector 0.2.0 reads 0.1.0 history without inventing changes for newly captured tracking coverage. Losing an already reviewed tracking link fails completeness validation and preserves last success. `changed` compares facts and scope; `changeCount` counts source transitions, so changing the date window alone is not a supplier event. Retention remains an explicit operator decision; this CLI does not delete history automatically.

## Verification and next integration

`npm run check` type-checks and tests invented fixtures for pagination, precise IDs/quantities, date boundaries, shipment dates, components, schema/account errors, retry/recovery bounds, history, encryption/tampering and failure preservation. `make browser-check` runs locked installation and these checks from the root.

Owner-local evidence includes repeated code-only 90-date pulls, live multi-page traversal and a deliberate scope-limit failure that preserved the accepted pointer byte-for-byte. Real records remain encrypted outside this repository. The completed after-deadline test did not recover the order with copied date extension or original-state SSO; previously renewed saved state still worked. See the [sanitized summary](../../docs/evidence/2026-10-20-lenovo-expiry.json). Dependable natural/multi-day SSO and interactive login/MFA recovery remain separate from current-session renewal evidence.

Next integration is trusted private connection/vault resolution, canonical shipment/date/quantity mapping into the Go pipeline, private UI publication, and an explicitly selected permitted cadence. This CLI enables no application schedule or webhook.

Installed tools are local npm packages: Playwright 1.64.1, TypeScript 7.0.2 and Node definitions 24.19.2. Remove this service's `node_modules` directory to remove them; `npm ci` restores them. The existing shared container image supplies Chromium/Xvfb independently of npm cleanup.
