# 0010: Self-contained public learning project

Date: October 17, 2026

## Decision

Publish the application source, fictional runtime fixtures, migrations, locked dependencies, reproducible test scripts and implementation documentation as a public learning project. The README describes actual capabilities, local operation, data boundaries and remaining live-source work.

Keep the original planning ZIP and its extracted contents unchanged in their local archive. They are optional reference material and excluded from public distribution. Builds and tests use the fictional fixtures checked into `internal/domain/fixtures/`; they do not read the archive. Public documentation links resolve within the public source tree.

Ignore the actual deployment manifest, credentials, database backups, supplier/session material and generated diagnostics. Publish `deploy/cloudflare.example.json` with placeholders so another operator can configure their own resources. The existing local manifest and its token-fetch workflow remain intact. Public source does not grant access to the hosted owner preview.

## Release procedure

Use the agreed existing owner preview without widening access. Build images, verify an encrypted pre-upgrade backup, stop API/worker processes, migrate with the administrative one-off tool, and start matching runtimes. Preserve existing accounts and purchasing data. Verify an encrypted post-upgrade restore in an isolated disposable database, runtime readiness and the production gateway separately from live supplier access.

Test the exported public source independently of ignored local files before pushing. Record executed checks and skipped live-source tests in [BUILD_STATUS.md](../BUILD_STATUS.md). Keep Lenovo order collection deferred: fictional ingestion and connection settings are not evidence of a real supplier integration.
