# Lenovo browser collector

This standalone Node 24/TypeScript CLI collects the authenticated Lenovo Pro US account's visible orders from the last **90 calendar dates, including today**, using the configured account timezone. It uses Playwright and the website's observed JSON responses. Collection and renewal need no agent, language model, MCP server, Chrome extension or per-run human navigation. Provisioning accepts a portable Scrapeyy export or an operator-selected local Chrome MCP connection; normal sign-in/MFA remains manual.

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
    session-import.enc          optional legacy encrypted initial session
  state/session.enc             created after a verified run
  state/session-candidate.enc   attended capture, awaiting VM verification
  data/
    snapshots/<run UUID>.enc    immutable encrypted observations/changes
    runs/<run UUID>.json        redacted attempt metadata
    last-success.json           atomic accepted snapshot pointer
    last-attempt.json           latest redacted attempt
    session-health.json         expiry clocks bound to the saved state digest
    renewal-test-last.json      finite test progress/result, when run
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

## Import a portable Scrapeyy session

The [Scrapeyy Borrow session feature](https://github.com/AaronCampbellit/Scrapeyy-public/pull/1)
can export a session as PIN/passphrase-encrypted or explicitly selected plaintext
JSON. Its portable file can be imported on multiple VMs/projects without a
destination public key or browser-control tunnel. The feature is merged into
Scrapeyy's `main` branch. Install a build containing Borrow session; ordinary
older Scrapeyy builds do not contain it.

In the signed-in Lenovo order list/detail tab, open Scrapeyy → **Borrow session**.
Include these exact origins for the current collector:

```text
https://account.lenovo.com
https://openapi.lenovo.com
https://www.lenovo.com
https://passport.lenovo.com
https://bpsso.lenovo.com
```

Keep the account order tab open. Related origins contribute cookies even without
an open tab; local/session storage is available only from open permitted tabs
in the same browser profile/container. This collector restores storage only from
the account, passport and business-SSO origins; optional www/openapi storage in
the export is ignored. Other origins are rejected. The extension reports omitted
storage/partitioned cookies and does not guarantee every site's session can
move to another device. No cookie expiration is extended.

A storefront-only (`www.lenovo.com`) export can also be staged: parent-domain
portal cookies may already grant account access. Missing account storage starts
empty, and storefront storage is never relabeled or copied to the account origin.
The importer reports `accountStorageCaptured` and `renewalCookiesComplete` so
successful short-term access is not confused with complete renewal material.
The real Firefox storefront export tested on October 22 authenticated the
isolated collector; a fuller export is still needed to test Passport renewal.

Transfer the downloaded file outside this checkout to the VM using your usual
private file transfer. Keep the existing private configuration and RSA key (or
provision those on a new VM as above), then run from this service directory:

```sh
chmod 600 /absolute/private/path/scrapeyy-export.json
python3 import-scrapeyy.py \
  --private-dir /absolute/path/outside-the-checkout/lenovo-private \
  --input /absolute/private/path/scrapeyy-export.json
python3 run-isolated.py \
  --private-dir /absolute/path/outside-the-checkout/lenovo-private \
  --mode verify-session
```

Encrypted input prompts for the PIN/passphrase privately in the terminal.
Plaintext input needs no secret. For a controlled automation pipe, the optional
`--passphrase-stdin` reads exact UTF-8 bytes; it does not strip a newline. Never
put the secret in command arguments or an environment variable. The export
secret is independent of the VM's RSA key. Short PINs remain susceptible to
offline guessing; a longer passphrase is preferable.

Import authenticates the encrypted envelope where applicable, checks Lenovo
scope/attributes and writes only an RSA-encrypted `session-candidate.enc` using
this VM's key. It holds the collector lock. An imported bundle's account is
**not yet verified**: configuration supplies the expected identity, and the
existing isolated `verify-session` command checks the live account, complete
90-date scope and accepted history before promotion. Import itself makes no
supplier request. Wrong PIN, malformed scope or unsafe input leaves the prior
candidate, active session and accepted observations unchanged.

Files require owner-only mode 0600; directories use 0700. Input and private
directory must be outside the checkout, without symbolic ancestors. The
reader bounds encoded files to 28 MiB (base64 overhead for Scrapeyy's 20 MiB
plaintext limit); this collector's converted Lenovo state is limited to 2 MiB.
Actual partitioned/first-party-isolated cookies are rejected. Firefox's
`partitionKey: null` represents an ordinary unpartitioned cookie and is accepted.
Host-only
versus domain cookie scope and original expiration are retained. An unspecified
SameSite attribute stays omitted so the destination browser applies its default.
Delete transferred plaintext when no longer needed; imports do not remove the
source file automatically. Real Firefox-export/VM account and collection
verification passed on October 22. Long-term renewal and other browser/site
combinations remain separate from that successful portability check.

## Attended recovery from Windows Chrome

Keep the private directory's existing key and independently confirmed account configuration. Sign in normally in the intended Lenovo order list/detail tab on the source laptop. In one PowerShell window, start the extension-backed MCP server:

```powershell
npx.cmd -y @playwright/mcp@0.0.83 --extension --shared-browser-context --host 127.0.0.1 --port 8931
```

In a separate window, forward the VM's loopback port to that server, replacing `YOUR_VM_SSH_HOST` with the existing SSH alias/host:

```powershell
ssh -NT -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 -R 127.0.0.1:8931:127.0.0.1:8931 YOUR_VM_SSH_HOST
```

Both commands must stay running. `--shared-browser-context` lets connected HTTP clients reuse the approved browser while that shared connection remains active. A new HTTP client without it can request a separate extension connection even if an earlier client still appears connected. The VM client uses `http://localhost:8931/mcp`, matching the MCP server's accepted Host header; changing that URL to `127.0.0.1` can return HTTP 403 even through a working tunnel. The extension can show "No clients" until the VM issues its first browser request. A listening SSH port alone is insufficient if the Windows MCP process is absent; that case can close HTTP connections without returning a response. There is no need to paste extension tokens, cookies or passwords into chat.

If browser connection prompts do not appear, the extension provides a local connection token. Stop the Windows MCP process, use the extension's token copy button, then run these commands in that same PowerShell window. They accept either the bare copied token or its displayed environment assignment without echoing it or storing its value in shell history:

```powershell
$env:PLAYWRIGHT_MCP_EXTENSION_TOKEN = ((Get-Clipboard -Raw) -replace '\s','') -replace '^PLAYWRIGHT_MCP_EXTENSION_TOKEN=',''
npx.cmd -y @playwright/mcp@0.0.83 --extension --shared-browser-context --host 127.0.0.1 --port 8931
```

Keep the intended Lenovo tab open in the Chrome profile containing the extension. This token authorizes the local MCP/extension connection; Lenovo sign-in/MFA remains necessary. A server restart invalidates existing MCP HTTP sessions, so start a new VM provisioning attempt afterward. Never commit the token or request it through chat.

From this service directory on the VM, run:

```sh
npm run provision -- --private-dir /absolute/path/outside-the-checkout/lenovo-private
python3 run-isolated.py --private-dir /absolute/path/outside-the-checkout/lenovo-private --mode verify-session
```

When prompted in Chrome, select the signed-in Lenovo order tab and connect. Provisioning reloads that selected page and confirms a successful order response for the configured account. It exports only Lenovo-scoped cookies and that account origin's local/session storage. AES-GCM encryption and public-key wrapping happen in the source MCP process; only ciphertext is transferred. The private RSA key stays on the VM. No raw storage file, response body or screenshot is written.

The provisioning wrapper holds the collector lock and stages `session-candidate.enc`; it never replaces active credentials or accepted facts. `verify-session` launches the isolated VM browser with that candidate. Only a full successful account/90-date/pagination/history check promotes it to `session.enc`. A failed verification preserves the active session and last success. A new private directory can use this path without a legacy import capsule once its directories, key and account configuration are provisioned. The Go vault and interactive VM password/MFA login are not implemented by this command.

## Normal renewal and finite expiry test

Version 0.3.0 records separate expiry clocks for Passport (`LPSWUST`, `LPSWUTGT`) and portal (`gateKeeper-token`, `online-token`) cookies. The earliest known clock limits renewal timing. Long-lived business-SSO or retention cookies do not establish usable account access. Browser expiry metadata is a planning signal, not proof of server-side validity.

Ordinary collection explicitly invokes the website's normal SSO when a known limiting expiry is within ten minutes. `--mode renew` invokes it immediately, including while reads still work:

```sh
python3 run-isolated.py --private-dir /absolute/path/outside-the-checkout/lenovo-private --mode renew
```

Every successful run still requires complete authenticated collection before saving state. Its `renewal` result distinguishes `full`, `portal_only`, `passport_only`, `unchanged`, `unknown` and `not_attempted`. `full` means both observed clocks advanced and collection succeeded; an HTTP 200 or a long cookie date alone is insufficient. `session-health.json` contains expiry metadata and a SHA-256 binding to the encrypted saved state, without cookie values.

The following is a finite experiment, not a recurring collector or production keep-alive:

```sh
python3 run-renewal-test.py --private-dir /absolute/path/outside-the-checkout/lenovo-private --dry-run
python3 run-renewal-test.py --private-dir /absolute/path/outside-the-checkout/lenovo-private
```

It requires fresh verified state and complete Passport/portal clocks. It plans a single renewal ten minutes before the earlier original expiry, then a separate collection ninety seconds after the later original expiry. Both clocks must advance and remain valid beyond that proof point. Otherwise it stops with `not_verified`. It also stops if another run changes the saved state, cancellation occurs or the bounded window expires (default four hours; maximum six). Progress/result is written privately to `data/renewal-test-last.json`. Keep its terminal running until completion; no background service is installed. The dry run makes no supplier requests. Passing this experiment would prove that bounded interval only; repeated natural expiry and multi-day testing remain necessary.

## Collection contract

1. Restore the saved session, or use the encrypted import when no saved session exists. Invalid saved state fails closed.
2. Open the fixed orders page and use Lenovo's normal SSO flow when needed, explicitly requested, or within ten minutes of a known limiting expiry. Authentication recovery allows one fresh collection attempt without filling credentials or submitting challenges.
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

No raw response diagnostic, screenshot or HAR is written. The encrypted session document separately retains the provider-owned browser storage needed for restart. Captures are immutable; a baseline creates no fabricated transition and repeated unchanged facts create zero changes. Connector 0.3.0 reads 0.1.0/0.2.0 history without inventing changes for newly captured tracking or authentication metadata. Losing an already reviewed tracking link fails completeness validation and preserves last success. `changed` compares facts and scope; `changeCount` counts source transitions, so changing the date window alone is not a supplier event. Retention remains an explicit operator decision; this CLI does not delete history automatically.

## Verification and next integration

`npm run check` type-checks and tests invented fixtures for pagination, precise IDs/quantities, date boundaries, shipment dates, components, schema/account errors, retry/recovery bounds, history, encryption/tampering and failure preservation. `make browser-check` runs locked installation and these checks from the root.

Owner-local evidence includes repeated code-only 90-date pulls, live multi-page traversal and a deliberate scope-limit failure that preserved the accepted pointer byte-for-byte. Real records remain encrypted outside this repository. The completed after-deadline test did not recover the order with copied date extension or original-state SSO; previously renewed saved state still worked. See the [sanitized summary](../../docs/evidence/2026-10-20-lenovo-expiry.json). Dependable natural/multi-day SSO and interactive login/MFA recovery remain separate from current-session renewal evidence.

Next integration is trusted private connection/vault resolution, canonical shipment/date/quantity mapping into the Go pipeline, private UI publication, and an explicitly selected permitted cadence. This CLI enables no application schedule or webhook.

Installed tools are local npm packages: Playwright 1.64.1, MCP SDK 1.32.1, TypeScript 7.0.2 and Node definitions 24.19.2. The SDK and its locked supporting dependencies handle attended HTTP initialization, the GET event stream and server heartbeat replies; they are not part of the supplier collection flow. Remove this service's `node_modules` directory to remove them; `npm ci` restores them. The existing shared container image supplies Chromium/Xvfb independently of npm cleanup.
