# Local foundation baseline

Date: October 16, 2026. Scope: M0 local development.

The imported plan contained no application or design assets. The initial implementation keeps its proposed React/TypeScript, Go, PostgreSQL, River, and Docker Compose baseline. The Node/TypeScript browser runner remains unimplemented until M1 source discovery; no browser service or supplier session is started by M0.

The initial development environment is Linux x86_64 with 8 CPUs and approximately 15 GiB RAM. Its existing Go 1.26.5, Node 24.19.0, npm 11.17.0, Docker 29.9.0, Compose 5.6.0 and Playwright CLI are reused. Actual host identity stays in local administration records. These are recorded development versions; deployment uses the pinned container images in source.

## Version choices

- Go module directive 1.26.0, toolchain 1.26.5; pgx 5.11.0, River 0.49.0, x/crypto 0.58.0. Exact transitive dependencies are in `go.mod` and `go.sum`.
- React and React DOM 19.3.1, TypeScript 7.0.2, Vite 8.3.4, React plugin 6.1.2. Exact package versions and integrity hashes are in `apps/web/package-lock.json`.
- PostgreSQL 16.15, using the already cached official PostgreSQL 16 Alpine image pinned by digest. The development services use pinned Go, Node, and Alpine image digests as well.

Go library and npm versions were checked against their registries during setup. Version choice establishes a reproducible foundation; compatibility is assessed by the executed build and integration checks. Vite's supported Node baseline is documented in its [getting-started guide](https://vite.dev/guide/).

## Runtime and data boundaries

Only Vite's local development entry point is published, on `127.0.0.1:4180`. Vite forwards same-origin API requests to the internal API. The database has no published port. The API's explicit development mode allows HTTP session cookies only for a configured loopback origin; HTTPS is required outside that mode. This is not release hosting.

An owner-controlled CLI runs schema migrations and account bootstrap with the migration credential. The API and worker receive only the constrained runtime credential. Database credentials are generated into a Git-ignored directory with mode 0700, mounted individually read-only, and excluded from Docker build contexts. Secret file contents are never printed by setup. Container-readable database files are mode 0444 within that restricted directory; owner password files remain 0400 or 0600 and are removed after bootstrap.

Application SQL migrations are embedded, checksum-verified, serialized, and transactional. River migrations run separately through its maintained migration API, before workers, as described in the [River migration documentation](https://riverqueue.com/docs/migrations). Runtime roles receive explicit table privileges following PostgreSQL's [privilege model](https://www.postgresql.org/docs/16/ddl-priv.html). Workspace RLS and the full reviewer isolation release gate remain M2 work.

The owner has a stable internal user ID and a separate local identity. Bootstrap creates an empty private workspace and a fictional presentation workspace with a versioned seed and explicit simulated business date. No order records, supplier observations, or purchasing exceptions are fabricated. A dedicated River probe verifies queue execution and durable writes; it is not supplier ingestion.

## Scope boundaries

Basic local sessions support exercising the foundation through the browser. This does not complete M2 account lifecycle, reviewer provisioning, RLS, or the full session and isolation acceptance plan. M1 will add normalized connector ingestion. Deployment, Lenovo login, permitted automation scope, shareable fields, and reviewer details remain unresolved operational gates.
