# BuyersWorkBench development

- Read `PROJECT.md` and `docs/BUILD_STATUS.md` for the implemented milestone and remaining work. API contracts and implementation choices are under `docs/`. When available, the original local-only planning archive is under `docs/planning/buyers-workbench-plan/`.
- Preserve the imported plan and original ZIP. Record implementation choices in `docs/decisions/` and current test evidence in `docs/BUILD_STATUS.md`.
- Keep one Go module for the API, worker, and local administration CLI. The frontend is React/TypeScript under `apps/web/`.
- `compose.yaml` is for local development. Keep its published interface on loopback. Changes to deployment, networks, or supplier access require the agreed target and scope.
- Never add default account credentials, public registration, private supplier data, or browser session material. The owner is provisioned locally using a restricted password file.
- Runtime processes never migrate the database. Run the one-off migration command before the API and worker. API and worker use the constrained `bwb_app` role; admin credentials are available only to local setup tools.
- A fictional workspace is not evidence of live Lenovo collection. Do not fabricate purchasing metrics, successful syncs, or exceptions; later scenarios must use the common ingestion pipeline.
- `make test` runs Go tests without requiring PostgreSQL. `make test-integration` creates a disposable PostgreSQL container with a random loopback port and removes only that container afterward. `make web-check` installs locked frontend dependencies and builds the app.
- Record skipped database, browser, and live-source checks separately. Reviewer access remains gated on the later authentication, isolation, sanitization, and HTTPS acceptance tests.
