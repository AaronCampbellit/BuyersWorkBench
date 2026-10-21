# Lenovo discovery record

Updated October 21, 2026. This record contains the discovery checklist and actual findings needed before implementing an account-specific connector. The original reference template remains preserved locally outside the public repository. New-machine setup and continuation are documented in [LENOVO_HANDOFF.md](../LENOVO_HANDOFF.md).

**Status: BOUNDED DISCOVERY, CODED 90-DATE COLLECTION AND CURRENT-SESSION RENEWAL VERIFIED.** On October 20 the owner connected the selected Lenovo Pro US tab, authorized read-only discovery and requested a session-transfer test on the VM. Order-list/detail JSON was observed. Two fresh isolated VM browser launches restored encrypted exported state and read the same authorized order. Subsequent isolated tests completed normal headed SSO renewal, encrypted updated-state persistence and an independent headless read. The owner subsequently selected all visible account orders from the last 90 calendar dates; the standalone coded collector verified that bounded coverage through full pagination and repeated detail reads. Application ingestion, recurring cadence and natural/multi-day expiry recovery remain unverified. No application Lenovo connector or schedule is enabled.

## Authorization and source

| Item | Actual finding |
|---|---|
| Authorized operator and date | Owner-assisted discovery/session tests and manual code-driven collection of the selected 90-date visible-account scope authorized on October 20 |
| Clean account/orders URL | `https://account.lenovo.com/us/lenovopro/en/account/order/list.html`; detail path `/us/lenovopro/en/account/order/detail.html` has private identifying query values that are omitted here |
| Account product/region | Lenovo Pro US for Small Business; Enterprise Pro/supply-chain partner eligibility is not established |
| Approved record/status/date scope | All visible account orders in the last 90 calendar dates, including today in the configured account timezone; all statuses, separate detail/shipment dates |
| Automation permission/restrictions and approved cadence | Owner-authorized manual coded runs in the selected window; supplier-supported terms and recurring cadence remain unverified; application scheduling remains disabled |
| Forbidden actions | Collection design excludes purchase submission, cancellation, returns/refunds, payment/address/account changes and supplier messaging. Runtime saves reviewed tracking links without following them; the owner separately authorized bounded read-only VM inspection of those UPS links. Account-specific constraints remain unverified |
| Shareable source fields | Not approved; U09 remains open |

U01 is identified for this bounded source. U02–U04 have partial discovery evidence below; ongoing automation, full field mapping, authentication recovery and U09 sharing approval remain open. See [OPEN_GATES.md](OPEN_GATES.md).

### Linked tracking information

The shipment array exposes `trackingNumber`, `trackingUrl` and `showTrackPackageBtn`. Observed populated links use `http://wwwapps.ups.com/WebTracking/track` with either a short or decorated query containing the shipment's exact `trackNums`. The collector validates the reviewed format and carrier, constructs a minimal HTTPS link, and attaches it to its original order/line/shipment identities. Missing links remain explicit; unreviewed or mismatched URLs are non-clickable. Actual identifiers and URLs are kept only in owner-controlled encrypted evidence.

An isolated VM browser with empty cookies followed the HTTPS UPS link to `www.ups.com/track` and observed matching package details through the site's read-only `webapis.ups.com/track/api/Track/GetStatus` POST. No Lenovo session was sent. A comparable later attempt did not complete the tracking response within its bounded inspection interval. This proves bounded access, not dependable carrier collection. Production extracts links from Lenovo only; additional UPS status, delivery milestones and ETA enrichment need their own transport, coverage/failure handling and repeated verification. Receipt and proof-of-delivery identity/address/signature fields must not be inferred or imported by this link extractor.

## Public API and webhook research

Reviewed official public Lenovo pages on October 17, 2026; no credentials or private data were involved:

| Source | Limited public finding | Account result |
|---|---|---|
| [Enterprise Pro benefits](https://www.lenovo.com/us/en/enterprisepro/benefits/) | Advertises enterprise procurement integrations, API/cXML/X12/EDIFACT formats, shipment alerts and fulfillment messages | Eligibility and specific subscription/read capabilities unknown |
| [Supply Chain Open Platform quick start](https://scc.lenovo.com/opendocs/en/common/guide/2_1_quick_start.html) | API/FTP integration is coordinated with Lenovo and limited by interface permissions | Applicability to this purchasing account unknown |
| [API access process](https://scc.lenovo.com/opendocs/en/common/guide/2_2_integration-api.html) | Documents partner client-credential onboarding and assigned interface permissions | No client provisioned or token request tested |
| [Data push API](https://scc.lenovo.com/opendocs/en/common/data-push/4_4_data_push_api.html) | Documents partner receiving addresses and authentication choices for push integration | No enabled topic, buyer-account coverage, signature or replay contract established |
| [Logistics API Portal](https://gtms.lenovo.com/api-portal/) | Documents coordinated logistics partner APIs and Lenovo-to-partner delivery | No evidence that this is the owner's purchasing integration |

Some official Lenovo APIs and push integrations exist. This does not establish a supported general purchasing webhook for this account. The October 20 browser probe observed the website's own GET reads under `openapi.lenovo.com/us/lenovopro/en/`: `/ofp/order/authDetail` and `/ofp/auth/mobileOrder/list`. Observing internal website requests does not establish a vendor-supported integration contract or recurring access permission.

Pending discovery:

1. Extend the identified portal and bounded owner-selected order into an explicitly approved recurring account/status/date scope.
2. Establish permitted automation and cadence; promote the VM probe into an operated runner only after recovery and ingestion checks pass.
3. Review account integration/export settings and applicable official program documentation.
4. If eligible, obtain supplier-provisioned client scopes, read/event coverage, subscription test path, limits, sender authentication, retries and ordering details. Any supplier contact requires an explicit instruction to send it.
5. Complete the observed field map and validate line/shipment identity and pagination with minimized private evidence.
6. Select the verified method and complete the one-order proof and failure tests below before scheduling.

## Authentication

| Template item | Actual finding |
|---|---|
| Normal sign-in/MFA path | Existing owner session used; normal SSO renewal without password/MFA succeeded in the VM. Fresh password/MFA login remains untested |
| Dedicated profile/environment | Independent sandboxed Chromium contexts in non-root network-none containers; standalone runner uses bundled Xvfb and an encrypted session between runs |
| Session handling and secure storage reference | Lenovo-scoped state encrypted on the source before transfer; restricted encrypted capsule/key outside the repository. Not bound to an application connection or production vault |
| Portability tested | Yes: immediate Windows-to-VM replay matched the same order/ecommerce/customer identities twice |
| Headless/host mode tested | Playwright 1.64.1, Chromium 156.0.8078.4; headless reads succeeded, while normal SSO renewal succeeded headed under bundled Xvfb |
| Reauthentication/revocation procedure | Normal headed SSO renewal and encrypted state restart verified for the current session. After the original deadline, the original capsule/date-extension/SSO attempts failed while already renewed state still worked. Dependable natural/multi-day recovery, interactive fallback and revocation remain open |
| Attended fallback | Not selected; remains a possible authorized approach |

Application login does not authenticate Lenovo. Normal owner-assisted MFA and connection/account binding must be tested; challenges must stop unattended collection. No public CDP/VNC or reviewer session upload is part of the design.

## Collection evidence

| Template item | Actual finding |
|---|---|
| Chosen method | Browser-response extraction is a verified probe candidate; production method/contract is not yet selected |
| Normal read operations observed | Website order-list and order-detail GET responses succeeded; no supplier mutation action performed |
| Pagination/detail/split shipments | List page/totals/version and duplicate checks implemented. A small page size required three live pages per pass; both complete passes and selected details succeeded. Line/component/split and shipment arrays are preserved |
| Account binding validation | Customer identity pinned by private configuration; order/ecommerce/customer identities matched for every requested live detail across repeated runs. Durable app connection binding remains pending |
| Source field → canonical field map | Candidate fields identified: `orderItemsVO`, `itemId`/`itemNo`/`lineNo`, quantities/statuses, `shippingStatusVOList`, `estimatedArrivalDate`, `deliveredDate`. Stability, units and semantic mapping remain pending |
| Units/date meanings and confirmation semantics | Literal calendar dates are preserved; estimated ship/delivery, shipment ETA and delivered date stay separate. Yearless ETA labels retain an unknown parsed date. Source UOM and carrier-reported date semantics remain unverified |
| Unavailable/ambiguous fields | Unknown; do not substitute fictional values |
| Complete bounded scope and expected record counts | Owner-selected 90-date visible-account scope verified using both full list passes and all selected details; encrypted counts/evidence remain owner-local |
| Source-specific read allowlist/excluded paths | Probe used exact observed Lenovo and static asset hosts; browser policy blocked unapproved hosts/operations. A production operation contract remains pending |
| Sanitizer allowlist/owner approval | Not established for real Lenovo data |

The initial proof must preserve source identity, exact quantities/UOM, explicit unknowns, date labels/kinds, provenance and observation time. A shipment notification or one-order email is partial evidence, not proof of a complete account snapshot. Follow [COLLECTION_STRATEGY.md](../COLLECTION_STRATEGY.md#complete-batches-and-partial-events).

## Test outcome

| Template item | Actual result |
|---|---|
| Connector/schema/browser/runtime versions | Standalone probe: Playwright 1.64.1, Chromium 156.0.8078.4; no application connector/schema activation |
| Actual account test execution time | Initial portability at 18:44:28/18:44:33 UTC on October 20; subsequent coded scope/renewal runs recorded in owner-local reports and BUILD_STATUS |
| Successful source scope/record count | Selected 90-date visible-account scope collected repeatedly; counts and minimized private captures remain outside this repository |
| Expired session, challenge and access-denial checks | Copied client-cookie expiry and empty-state denial tested; headless SSO 403 observed; headed normal renewal succeeded. Natural server/SSO expiry and captcha recovery remain unverified |
| Wrong account, pagination, split shipments and schema-drift checks | Invented tests cover mismatches, gaps/duplicates/drift and separate shipment dates; real pagination and limit-failure preservation passed. Broader live split/lifecycle identity semantics remain pending |
| One-order pipeline/private UI proof | Not executed against Lenovo |
| Sanitizer preview/approval on real source | Not executed |
| Runner sandbox/egress/resource checks | Bounded sandbox launch, dropped capabilities, read-only/tmpfs resources, network-none container and restricted Unix-socket proxy verified; private/LAN/metadata and unapproved-host checks denied access |
| Private minimized evidence references | Owner-local sanitized probe report; encrypted session and private key outside this repository. No raw source artifact is included here |
| Ready for scheduling | **No.** Recurring terms/cadence, natural/multi-day authentication recovery, canonical field mapping and application ingestion remain unverified |

The October 20 tests establish bounded code-only collection, pagination, encrypted history, failure preservation, session portability and current-session normal renewal/persistence. The one-off real-expiry follow-up subsequently completed: copied date extension did not preserve access, original-state renewal was not recovered in that test, and a previously renewed capsule still worked. See the [sanitized summary](../evidence/2026-10-20-lenovo-expiry.json). These results do not establish Go pipeline publication, private-workspace UI ingestion, unattended session longevity or production readiness. Detailed technical evidence and skipped checks are in [BUILD_STATUS.md](../BUILD_STATUS.md#october-20-deterministic-lenovo-collector). No application database/frontend tests were executed for the standalone probe; later source-only checks are recorded separately.

Never insert passwords, cookies, auth headers, session state, private account names, raw order IDs, addresses, tracking tokens/URLs, screenshots of private orders or unredacted HARs into this tracked document. Store only minimized findings and restricted evidence references.
