# Lenovo discovery record

Updated October 17, 2026. This record contains the discovery checklist and actual findings needed before implementing an account-specific connector. The original reference template remains preserved locally outside the public repository.

**Status: ACCOUNT UNVERIFIED / NOT INSPECTED.** Public documentation only was reviewed. No private portal, account settings, login, order, endpoint execution or browser session was inspected. On October 16 the owner deferred live discovery; the new collection strategy does not supply the exact portal/account/region, permitted automation scope or approved cadence needed to perform it. No live Lenovo connector, credential or schedule is enabled.

## Authorization and source

| Item | Actual finding |
|---|---|
| Authorized operator and date | Not established for account discovery; public documentation research performed October 17, 2026 |
| Clean account/orders URL | Not supplied; do not infer it from public integration documentation |
| Account product/region | Unknown; Lenovo consumer, business, Enterprise Pro and supply-chain partner roles are not interchangeable |
| Approved record/status/date scope | Not supplied |
| Automation permission/restrictions and approved cadence | Not established; real scheduling remains disabled |
| Forbidden actions | Collection design excludes purchase submission, cancellation, returns/refunds, payment/address/account changes, supplier messaging and automatic tracking-link following; account-specific constraints remain unverified |
| Shareable source fields | Not approved; U09 remains open |

See [OPEN_GATES.md](OPEN_GATES.md), especially U01–U04/U09. This record does not close those gates.

## Public API and webhook research

Reviewed official public Lenovo pages on October 17, 2026; no credentials or private data were involved:

| Source | Limited public finding | Account result |
|---|---|---|
| [Enterprise Pro benefits](https://www.lenovo.com/us/en/enterprisepro/benefits/) | Advertises enterprise procurement integrations, API/cXML/X12/EDIFACT formats, shipment alerts and fulfillment messages | Eligibility and specific subscription/read capabilities unknown |
| [Supply Chain Open Platform quick start](https://scc.lenovo.com/opendocs/en/common/guide/2_1_quick_start.html) | API/FTP integration is coordinated with Lenovo and limited by interface permissions | Applicability to this purchasing account unknown |
| [API access process](https://scc.lenovo.com/opendocs/en/common/guide/2_2_integration-api.html) | Documents partner client-credential onboarding and assigned interface permissions | No client provisioned or token request tested |
| [Data push API](https://scc.lenovo.com/opendocs/en/common/data-push/4_4_data_push_api.html) | Documents partner receiving addresses and authentication choices for push integration | No enabled topic, buyer-account coverage, signature or replay contract established |
| [Logistics API Portal](https://gtms.lenovo.com/api-portal/) | Documents coordinated logistics partner APIs and Lenovo-to-partner delivery | No evidence that this is the owner's purchasing integration |

Some official Lenovo APIs and push integrations exist. This does not establish a supported general purchasing webhook for this account, and this review does not establish that one is absent. Public documentation is a lead for authorized account-specific discovery, not permission to call the listed gateways. No network-observed internal API has been inspected or selected.

Pending discovery:

1. Confirm exact account URL, product/region and read scope with the owner.
2. Establish permitted automation, cadence and deployment/runner target.
3. Review account integration/export settings and applicable official program documentation.
4. If eligible, obtain supplier-provisioned client scopes, read/event coverage, subscription test path, limits, sender authentication, retries and ordering details. Any supplier contact requires an explicit instruction to send it.
5. If needed and authorized, inspect normal read traffic/DOM with minimized private evidence; distinguish a website's internal requests from a supported API or webhook.
6. Select the verified method and complete the one-order proof and failure tests below before scheduling.

## Authentication

| Template item | Actual finding |
|---|---|
| Normal sign-in/MFA path | Not inspected |
| Dedicated profile/environment | Not selected; must be isolated from personal browsing and other supplier accounts |
| Session handling and secure storage reference | Not selected; no supplier secret/session has been imported |
| Portability tested | No |
| Headless/host mode tested | No |
| Reauthentication/revocation procedure | Not established |
| Attended fallback | Not selected; remains a possible authorized approach |

Application login does not authenticate Lenovo. Normal owner-assisted MFA and connection/account binding must be tested; challenges must stop unattended collection. No public CDP/VNC or reviewer session upload is part of the design.

## Collection evidence

| Template item | Actual finding |
|---|---|
| Chosen method | None; supported API/push/export, browser response/DOM or attended capture remain candidates |
| Normal read operations observed | None |
| Pagination/detail/split shipments | Not inspected |
| Account binding validation | Not tested |
| Source field → canonical field map | Not established |
| Units/date meanings and confirmation semantics | Unknown; estimated ship, arrival and delivered must be mapped separately |
| Unavailable/ambiguous fields | Unknown; do not substitute fictional values |
| Complete bounded scope and expected record counts | Not established |
| Source-specific read allowlist/excluded paths | Not established |
| Sanitizer allowlist/owner approval | Not established for real Lenovo data |

The initial proof must preserve source identity, exact quantities/UOM, explicit unknowns, date labels/kinds, provenance and observation time. A shipment notification or one-order email is partial evidence, not proof of a complete account snapshot. Follow [COLLECTION_STRATEGY.md](../COLLECTION_STRATEGY.md#complete-batches-and-partial-events).

## Test outcome

| Template item | Actual result |
|---|---|
| Connector/schema/browser/runtime versions | No Lenovo connector/browser runtime selected or executed; fictional adapter evidence is separate |
| Actual account test execution time | None |
| Successful source scope/record count | Not measured; no real order collected |
| Expired session, challenge and access-denial checks | Not executed |
| Wrong account, pagination, split shipments and schema-drift checks | Not executed |
| One-order pipeline/private UI proof | Not executed against Lenovo |
| Sanitizer preview/approval on real source | Not executed |
| Runner sandbox/egress/resource checks | Not executed for a supplier browser runner |
| Private minimized evidence references | None |
| Ready for scheduling | **No.** Exact source, permissions, auth, field mapping, completeness, isolation and recovery remain unverified |

Public documentation review is the only executed Lenovo research in this record. Database tests, fictional adapter tests, application browser tests and existing owner HTTPS availability cannot establish live collection success. Record future account tests separately with actual scope, time, runtime and outcome; retain explicit skipped checks.

Never insert passwords, cookies, auth headers, session state, private account names, raw order IDs, addresses, tracking tokens/URLs, screenshots of private orders or unredacted HARs into this tracked document. Store only minimized findings and restricted evidence references.
