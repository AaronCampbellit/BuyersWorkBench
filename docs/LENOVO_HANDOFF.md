# Continue Lenovo collection on another machine

Updated October 21, 2026. This is the starting point for the next developer or agent. Read [PROJECT.md](../PROJECT.md), [BUILD_STATUS.md](BUILD_STATUS.md), the [runner README](../services/browser-runner/README.md) and decisions [0011](decisions/0011-lenovo-session-portability-probe.md), [0012](decisions/0012-lenovo-session-renewal-experiments.md), [0013](decisions/0013-lenovo-coded-collection.md) and [0014](decisions/0014-lenovo-linked-tracking.md).

## Where work stopped

The standalone TypeScript collector in `services/browser-runner` is implemented at version 0.2.0. It collects all visible Lenovo Pro US small-business account orders placed in the last 90 inclusive calendar dates in the configured timezone, across all statuses. The runtime uses ordinary Playwright/browser JSON reads and needs no agent, MCP connection, Windows laptop or extension after initial provisioning. Full-list traversal, selected detail reads and a second full-list traversal must all validate before acceptance.

Order/line/component/split/shipment parsing preserves large integer IDs, exact quantities, separate shipment dates and explicit unknowns. Each order's `linkedTrackingInformation` joins reviewed HTTPS UPS links to its exact line and shipment keys. Runtime extracts links from Lenovo JSON; it does not contact UPS. Encrypted session updates, immutable observations, comparisons and redacted run metadata are stored outside the checkout.

The Go application still runs fictional connectors. The Lenovo CLI is not wired into its credential vault, worker, database or private Orders view. `lenovo_orders.v1` is an extraction document, not a `domain.Batch`. There are twelve application migrations; this collector added none. No recurring Lenovo job or application deployment was enabled. A local readable tracking export demonstrated the intended order associations, but that private artifact is not part of Git or a deployed screen.

## Findings to preserve

| Area | Verified result and limit |
|---|---|
| Source | Lenovo Pro US for Small Business, `account.lenovo.com/us/lenovopro/en/account/order/list.html`. Other Lenovo account experiences and supplier-supported API eligibility remain unknown. |
| Read endpoints | HTTPS `openapi.lenovo.com/us/lenovopro/en/ofp/auth/mobileOrder/list` and `/ofp/order/authDetail`. Browser code owns their reviewed parameters and account binding. These are observed website reads, not a promised public API contract. |
| Scope/coverage | Traverse all visible pages within configured limits, filter 90 placement dates locally, fetch each selected detail, then re-read all list pages. Live small-page pagination and unchanged repeat captures passed. A deliberate order-limit failure preserved last success. |
| Privacy/identity | Account/customer/order/ecommerce identities must match the independently confirmed configuration. No names, addresses, payment records, recommendations or serial numbers belong in the minimized capture. Tests contain invented records only. |
| Shipment semantics | Service `Fulfilled` does not mean physical delivery. Multiple shipment ETAs cannot become one order ETA. A yearless arrival label keeps a null parsed date. `upsDeliveryDate` has unverified semantics and is retained separately as `carrierReportedDate`. Source units and lifecycle identity changes still need canonical mapping. |
| Session portability | Scoped Lenovo cookies and account local/session storage, encrypted before transfer, worked in an independent VM browser. IndexedDB was not captured. |
| Normal renewal | Headed Chromium under Xvfb completed the website's normal SSO and renewal POSTs, saved new encrypted state and supported independent restart. Three consecutive final code-driven forced-client-expiry trials passed. Default headless SSO had a 403/opaque-response problem; do not switch to headless merely because authenticated reads work. |
| Cookie lifetime | Normal activity did not extend the original cookie deadline. After that deadline, extending copied cookie dates without SSO did not recover the selected order. The original capsule and a headed SSO attempt also failed to recover it in that bounded follow-up. A previously normally renewed capsule still read the same order. Browser expiry edits are not a renewal method. |
| Expiry limits | The real-deadline follow-up completed October 20; it is no longer queued or pending. It did not establish the server timeout, indefinite SSO, multi-day reliability, revocation recovery or an attended login/MFA path. Successful earlier forced-expiry trials do not close those gates. See the [sanitized evidence summary](evidence/2026-10-20-lenovo-expiry.json). |
| Overnight check | On October 21, a fresh CLI process reused the unchanged saved VM session after about 17.2 hours and returned `authentication_required`; normal recovery did not complete. The saved session and last-success pointer were unchanged. No recurring renewal had been configured and no password/MFA or Windows source browser was used. Latest accepted observations remained from October 20. See the [overnight evidence](evidence/2026-10-21-lenovo-overnight.json); require attended recovery before claiming fresh data. |
| Product flags | Do not infer physical delivery eligibility from the current `physicalProduct` boolean alone. Observed raw source values were 0 for computer/dock lines and 1 for support components, so the field's enum semantics require verification before canonical integration. The visualization uses source line relation/status and explicit shipment facts instead. |
| UPS | Both observed legacy HTTP link formats contain the shipment's `trackNums`; validate the exact match and emit a minimal HTTPS link. A cookie-empty VM browser reached UPS and once received matching details through its normal read-only `Track/GetStatus` POST. Other bounded attempts were inconsistent. Keep UPS status/ETA enrichment separate from Lenovo coverage and buyer receipt. |

Earlier exploratory startup failures stopped with `source_schema_invalid` and accepted no data. Their cause was not reproduced or confirmed. Preserve this uncertainty; successful final trials do not establish fully unattended reliability.

## Fresh clone: checks without supplier access

The collector wrapper targets Linux with Unix sockets, file permissions, `fcntl` and a non-root operator who can run Docker. Linux amd64 was the tested host. Native Windows/macOS execution and other architectures have not been verified; use a supported Linux VM for the runner. Local Windows Chrome was only the initial attended discovery/provisioning source.

Use Node 24.19 or newer within the Node 24 series, npm, Python 3, Make and Docker. Follow the repository's Go toolchain and [application setup](../README.md#run-locally) for Go/application work. Do not change system defaults to match this historical host.

```sh
git clone https://github.com/AaronCampbellit/BuyersWorkBench.git
cd BuyersWorkBench
make browser-check
```

`make browser-check` installs the locked project-local dependencies and runs the type check plus 24 invented-fixture tests; it makes no supplier request. Playwright 1.64.1, TypeScript 7.0.2 and `@types/node` 24.19.2 are locked in the service. Node 24's JSON reviver source token is required for lossless supplier IDs. Do not replace it with a round-tripping `JSON.parse` in the browser.

For the whole source checkout:

```sh
make check
make test-integration
```

The first command includes Go tests/vet and the locked web build. The second starts and cleans up its own disposable PostgreSQL container on a random loopback port; it does not use the deployed database. Neither command provisions a Lenovo session. Running the application itself additionally needs local setup, one-off migrations and owner provisioning as described in the root README.

## What does not travel in Git

No private key, session capsule, actual account configuration, source snapshot, tracking report, password, extension token, tunnel configuration or database backup is committed. `node_modules`, pulled container images and temporary experiment containers also do not travel with a clone. There is no required private file hidden in an earlier commit.

To continue the existing private observation history, the owner must securely transfer the complete operator-owned private directory from the previous machine, separately from Git. Keep `credentials/config.json`, `credentials/key.pem`, the encrypted initial capsule, `state/session.enc`, `data/snapshots/`, `data/runs/` and both data pointers together. Do not transfer a running process's proxy socket; start with an empty `egress/`. Stop collection during the transfer so captures and the accepted pointer are consistent. Preserve directory mode 0700 and regular-file mode 0600 with ownership set to the destination operator; the wrapper refuses symbolic ancestors and overlapping executions. Keep the key backed up independently from ciphertext.

The saved session may have expired, been revoked or be unsuitable for a different host even if its browser cookie date is in the future. Do not assume yesterday's capsule can authenticate a new machine. If it cannot, use the normal attended sign-in/MFA path in an authorized browser, validate the intended account and provision a new scoped encrypted session through a trusted local capture workflow. Initial capture/login tooling is not productized in this repository; do not interpret `run-isolated.py` as a login command. Do not ask for passwords, cookies or tokens in chat, patch a token or extend cookie dates to claim renewal.

For a new private directory, use the invented [configuration example](../services/browser-runner/config.example.json) as a template and independently confirm the real customer ID. Provision an RSA key of at least 3072 bits and a capsule compatible with `src/session.ts` and `src/crypto.ts`:

```text
schema: 1
capturedAt: ISO timestamp
storageState: scoped Lenovo cookies and permitted origin localStorage
sessionStorage: account.lenovo.com origin plus its values
locale, timezone: source browser settings
baseline.customerId: independently confirmed account
```

Only Lenovo cookie domains and the account/passport/business-SSO origins are permitted. The initial session must use the collector's authenticated encryption envelope (`RSA-OAEP-SHA256+A256GCM`) with context `buyersworkbench:lenovo:session:v1`. The importer also explicitly accepts the earlier encrypted probe context `lenovo-vm-probe:v1`. Updated sessions always use the collector context; observations use `buyersworkbench:lenovo:observation:v1`. A raw Playwright storage-state JSON is not an accepted encrypted session. Include only scoped provider data; do not copy the whole Chrome profile.

## Run a provisioned collector once

Pull the exact image used by the wrapper; it already contains Chromium and Xvfb, so a host browser download is unnecessary:

```sh
docker pull mcr.microsoft.com/playwright:v1.64.1-noble@sha256:7852dc5a7d72f5fe458a597d690f6c42fbc8e9e26ddfa95f8e6ef552558d9bbf
python3 services/browser-runner/run-isolated.py --private-dir /absolute/path/outside-the-checkout/lenovo-private
```

Use an owner-provisioned directory outside the checkout. Its layout, configuration limits and preservation rules are in the [runner README](../services/browser-runner/README.md). A Linux Docker/Chromium sandbox compatible with the checked-in seccomp profile is required. Retain the pinned Playwright package/image pairing and exact-host/public-IP egress policy when moving machines. No incoming/public port or database access is needed by this CLI.

Success requires exit code zero and an `outcome: success` summary with complete list coverage, a new immutable encrypted snapshot and the matching atomic `last-success.json`. `trackingLinkCount` counts reviewed links; shipment/forecast row counts do not establish physically shipped package counts. Repeat the same bounded pull and inspect comparisons before accepting a migrated setup. An initial baseline and newly supported field coverage must not manufacture earlier changes.

If authentication or a challenge requires attention, stop unattended attempts and recover normally. An invalid saved state fails closed instead of silently falling back to the initial import. A schema/account/coverage failure leaves last success in place. If a storage error says `inspect_last_success`, examine that pointer because an error after its atomic rename can follow successful acceptance. Never publish a failed attempt as fresh supplier facts.

No recurring schedule is defined. Review and agree permitted cadence, operational account/host scope and failure handling before adding one.

## Next implementation work, in order

1. Productize initial scoped encrypted provisioning and normal attended login/MFA recovery; resolve the unexplained startup schema failures and repeat natural expiry, multi-day and revoked-session tests on the target host.
2. Bind the runner to a trusted private workspace/connection and Go vault reference. An incoming source record or presentation/reviewer upload must not select credentials or a live account.
3. Extend the canonical model where required for shipment schedules and tracking, source units, explicit date kinds/provenance and stable lifecycle identities. Do not flatten the extraction into the current line model by discarding facts.
4. Publish a bounded real private run through the existing validation/staging/fenced transaction and prove failure preservation. Keep the independently resettable fictional workspace and source fixtures separate.
5. Render Linked tracking information in the private Orders detail view with reviewed HTTPS URLs and source/freshness labels. Do not inject private records into fictional fixture upload routes or public tests.
6. If UPS status/ETA enrichment is needed, build and repeatedly verify a separate transport with independent freshness, rate/failure handling and permitted source access. Exclude recipient/address/signature/proof-of-delivery data from this link feature.
7. Only after those checks, select an approved cadence, retention policy and recovery/alerting path. Sharing actual Lenovo fields still requires the U09 decision in [OPEN_GATES.md](intake/OPEN_GATES.md).

## Local tools and reproducibility

`npm ci` restores service tools into `services/browser-runner/node_modules`; remove that directory to remove them. The web build similarly restores its locked packages into `apps/web/node_modules`. Go commands populate the user's normal module/build cache. No global tool or system default is required by this change. The pinned Playwright image is a separate Docker cache addition; leave it installed while the runner is in use, and remove that exact image with Docker only when no other work needs it.

The original planning archive remains local and excluded from Git; it is optional historical context, not a build dependency. Machine-specific exploratory scripts, expired-state mutations, private readable reports and local logs are not production transports. Their relevant methods, findings, test limitations and decisions are captured in the documents above; reuse the versioned runner and tests when continuing development.
