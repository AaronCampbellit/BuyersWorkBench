# Operational inputs and gates

Updated October 21, 2026. Bounded owner-assisted Lenovo Pro US discovery and Windows-to-VM portability, coded 90-date collection and current-session renewal are verified in [LENOVO_DISCOVERY.md](LENOVO_DISCOVERY.md). The encrypted probe state is outside this repository and is not provisioned into an application connection. Application ingestion and recurring Lenovo collection remain pending. The real-deadline experiment is complete with limited results; continue from [LENOVO_HANDOFF.md](../LENOVO_HANDOFF.md). Current application deployment and separate probe evidence are recorded in [BUILD_STATUS.md](../BUILD_STATUS.md).

| Source item | Current evidence | Gate |
|---|---|---|
| U01 Lenovo URL and account experience | Lenovo Pro US for Small Business order list/detail identified in owner-selected tab | Identified for bounded discovery; ongoing account binding and other account experiences remain separate |
| U02 Permitted automation and scope | Owner selected manual code-driven collection of all visible account orders in the last 90 calendar dates; supplier terms and recurring cadence pending | Required before enabling an application collector or schedule |
| U03 Available fields and source semantics | Allowlisted order/line/component/shipment parsing, lossless IDs, explicit unknowns and real multi-page coverage verified | Lifecycle identity stability, carrier-date/UOM canonical mapping and Go ingestion still require verification |
| U04 Login, MFA, and session behavior | Code-driven renewal/restart and three final forced-cookie-expiry trials passed. After-deadline cookie-date edits and original-state SSO did not recover access; an already renewed capsule still worked | Dependable natural/multi-day recovery, fresh login/MFA and revocation remain open before unattended operation |
| U05 Deployment host and resources | An owner-only Compose profile runs alongside local development; actual host details remain in local administration records | Broader hosting capacity, off-host backup and disaster recovery remain open; unrelated workloads are preserved |
| U06 Hostname and HTTPS route | Dedicated Cloudflare Tunnel with an owner-only Access policy; configure the chosen hostname in local `.env` | Current HTTPS evidence is in BUILD_STATUS; reviewer access remains gated on acceptance tests and explicit invitations |
| U07 Repository and frontend | Go/React source, migrations, test tooling and operational examples are included in this repository | Fresh installations require local setup and explicit owner provisioning |
| U08 Reviewers and expiry | No actual invitees supplied; no external accounts created | Reviewer CLI, expiry, disable, first-use change and isolation are implemented/tested; actual invitation and Access-policy changes remain owner decisions |
| U09 Approved shareable Lenovo fields | Not supplied; real minimized captures remain encrypted outside the checkout | Required before sanitizer approval or sharing |

The existing stack has the requested owner account. A fresh installation uses `make owner` and a hidden local password prompt. Repeating the same owner email preserves credentials. QA accounts belong only to disposable verification databases.

The fictional workspace uses the pinned seed-1.0 dataset and an October 19, 2026 business date. The collector stamps actual execution time; source revisions pass through validation, observation comparison, conditions and notification publication. The fixed fictional business clock does not establish live Lenovo evidence.

Complete the account, authentication, source-field and test sections in [LENOVO_DISCOVERY.md](LENOVO_DISCOVERY.md) before application integration and recurring live collection. Record the approved host, hostname, resource limits, backup destination and sharing scope in private deployment records; use the public [tunnel](../TUNNEL.md), [operations](../OPERATIONS.md) and [backup](../BACKUP.md) runbooks for the reproducible procedure. The original reference package is preserved locally outside the public repository.

See the [owner tunnel runbook](../TUNNEL.md) for route configuration, the example resource manifest, restart/stop commands, credential handling and remaining release limits.

Encrypted local backups and a disposable restore rehearsal are implemented. An off-host destination, independent key escrow and automatic backup cadence are not configured. The bounded VM probe's sandbox and egress checks are recorded separately; production browser-runner integration and its full acceptance remain pending.
