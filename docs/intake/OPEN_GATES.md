# Operational inputs and gates

Updated October 17, 2026. Live Lenovo order collection is deferred while the supplier-independent foundation is developed. Public official documentation research is recorded in [LENOVO_DISCOVERY.md](LENOVO_DISCOVERY.md); no private account was inspected. Multiple connections, fictional source staging, local encrypted vault tooling, routing/preferences and explicit retention are implemented. No supplier credential was provisioned or required. Current deployment evidence is recorded in [BUILD_STATUS.md](../BUILD_STATUS.md).

| Source item | Current evidence | Gate |
|---|---|---|
| U01 Lenovo URL and account experience | Exact portal/account product/region pending; public integration programs reviewed, private account not inspected | Required before account-specific connector discovery |
| U02 Permitted automation and scope | Exact read scope, method and cadence pending | Required before automated supplier access; no real connector or schedule exists |
| U03 Available fields and source semantics | Not inspected | Required before real field mapping and completeness claims |
| U04 Login, MFA, and session behavior | Not inspected | Required before private runner authentication or scheduling |
| U05 Deployment host and resources | An owner-only Compose profile runs alongside local development; actual host details remain in local administration records | Broader hosting capacity, off-host backup and disaster recovery remain open; unrelated workloads are preserved |
| U06 Hostname and HTTPS route | Dedicated Cloudflare Tunnel with an owner-only Access policy; configure the chosen hostname in local `.env` | Current HTTPS evidence is in BUILD_STATUS; reviewer access remains gated on acceptance tests and explicit invitations |
| U07 Repository and frontend | Go/React source, migrations, test tooling and operational examples are included in this repository | Fresh installations require local setup and explicit owner provisioning |
| U08 Reviewers and expiry | No actual invitees supplied; no external accounts created | Reviewer CLI, expiry, disable, first-use change and isolation are implemented/tested; actual invitation and Access-policy changes remain owner decisions |
| U09 Approved shareable Lenovo fields | Not supplied; no real data collected | Required before sanitizer approval or sharing |

The existing stack has the requested owner account. A fresh installation uses `make owner` and a hidden local password prompt. Repeating the same owner email preserves credentials. QA accounts belong only to disposable verification databases.

The fictional workspace uses the pinned seed-1.0 dataset and an October 19, 2026 business date. The collector stamps actual execution time; source revisions pass through validation, observation comparison, conditions and notification publication. The fixed fictional business clock does not establish live Lenovo evidence.

Complete the account, authentication, source-field and test sections in [LENOVO_DISCOVERY.md](LENOVO_DISCOVERY.md) before any live collection. Record the approved host, hostname, resource limits, backup destination and sharing scope in private deployment records; use the public [tunnel](../TUNNEL.md), [operations](../OPERATIONS.md) and [backup](../BACKUP.md) runbooks for the reproducible procedure. The original reference package is preserved locally outside the public repository.

See the [owner tunnel runbook](../TUNNEL.md) for route configuration, the example resource manifest, restart/stop commands, credential handling and remaining release limits.

Encrypted local backups and a disposable restore rehearsal are implemented. An off-host destination, independent key escrow and automatic backup cadence are not configured. The future browser-runner sandbox/egress gate remains deferred with live Lenovo work.
