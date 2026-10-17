# Owner-only HTTPS deployment

The optional Cloudflare Tunnel profile serves built frontend assets and the production API behind Cloudflare Access. It shares the project's PostgreSQL database with local development. The local development URL remains `http://127.0.0.1:4180`; choose an HTTPS hostname under a domain you control for the public route.

## Sign in

Cloudflare Access authenticates the explicitly allowed owner through the configured identity provider. Then sign in to BuyersWorkBench with the separately provisioned local application account. Tunnel setup does not change the application password. Keep the Access policy restricted to the owner until the external-sharing acceptance gates are satisfied.

The reference deployment uses an owner-only policy. Publishing this repository does not grant access to that deployment, create accounts or enable reviewer invitations. Human identity-provider verification is completed by the owner at sign-in; deployment checks need no mailbox access.

## Deployed route

```text
HTTPS hostname
  → Cloudflare Access owner policy
  → dedicated application tunnel
  → cloudflared validates this application's Access JWT
  → public-web Caddy gateway with built React assets
  → public-api using the exact HTTPS origin and the existing PostgreSQL database
```

The tunnel, gateway, and public API publish no host ports. Cloudflared has an outbound network and a private origin network; it receives only its tunnel token. The gateway and API communicate over a private backend network. Database traffic remains internal.

The public frontend image contains built assets rather than the Vite development server. The gateway rejects development source routes and missing assets, and routes API errors to the API without an HTML fallback. It sets a frontend CSP and no-store caching. The public API runs in production mode with Secure, HttpOnly, SameSite Strict cookies scoped to `/api`, the exact HTTPS origin, and no development aliases.

Public services run without root privileges, with read-only filesystems, dropped capabilities, bounded resource limits, rotated logs, and restart policies. Caddy terminates internal HTTP on port 8080; Cloudflare supplies the public HTTPS endpoint. The otherwise unused Caddy low-port file capability is removed in the build so the process can start with all runtime capabilities dropped.

Keep actual Cloudflare resource identifiers in the ignored local `deploy/cloudflare.json`, using [the example manifest](../deploy/cloudflare.example.json) as a shape reference. Provision a dedicated tunnel, a proxied DNS record, an Access application and an explicit owner allow policy for your chosen hostname. Configure tunnel ingress to `http://public-web:8080`, enable Access JWT validation with your application's team and audience, and end ingress with an HTTP 404 fallback. The example contains placeholders and does not create these resources. Preserve unrelated tunnels and services; this application requires no SSH, firewall or existing host proxy changes.

Before first tunnel startup, run the [local setup and owner provisioning](../README.md), configure `BWB_PUBLIC_HOST` and `BWB_PUBLIC_ORIGIN` in the ignored `.env`, and set `BWB_HOST_UID`/`BWB_HOST_GID` to the local owner of the connector token. Supply the existing tunnel token in `.local/cloudflared-token` with mode 0400 under a mode 0700 `.local` directory, or configure the token-fetch helper below. Store no token in the manifest. Run `make tunnel-build` and `make tunnel-up`, then verify gateway/API health and the anonymous Access redirect. This repo does not provision Cloudflare resources automatically.

## Operate the tunnel

From the repository root:

| Command | Effect |
|---|---|
| `make tunnel-build` | Build the backend and production frontend images |
| `make tunnel-up` | Start the public API, static gateway, and connector with their database and one-off migration dependencies; use the coordinated update procedure below for an existing stack |
| `make tunnel-status` | Inspect the merged development and tunnel services |
| `make tunnel-logs` | Read bounded public API, gateway, and connector logs |
| `make tunnel-down` | Stop only the tunnel, public gateway, and public API; preserve the local development services and database |
| `make tunnel-refresh-token` | Fetch the existing tunnel token using approved local credentials, then force-recreate the connector so its secret mount reads the replacement file |

Public route settings are in the Git-ignored `.env`: `BWB_PUBLIC_HOST`, `BWB_PUBLIC_ORIGIN`, and the connector's host UID/GID. The tunnel token is mode 0400 under the mode 0700 `.local` directory, excluded from source control and Docker builds. The fetch helper reads `CLOUDFLARE_API_TOKEN` or the owner's mode 0600 `~/.config/cloudflare/fleet-api-token` file without printing it. Use a credential authorized only for the necessary tunnel operation. Do not place secret values in arguments, documentation or logs. Token refresh retrieves the existing token; rotation/revocation is a separate Cloudflare administration action.

For an existing deployment, follow the [coordinated update procedure](OPERATIONS.md#update-the-current-stack): back up, build, stop both API variants and the worker, run the one-off migration command, then recreate the matching runtimes. Runtime processes never migrate the database. The current source has twelve application migrations; a prior eight-migration preview requires migrations 0009–0012 before updated API/worker startup.

Run `make tunnel-down` before `make down` when stopping the complete application. The public profile shares the database with local development, so stopping the base database also interrupts the public application. Stopping the connector does not delete its Cloudflare resources or database data. Permanent DNS, Access, or tunnel removal should target only the identifiers in the deployment manifest.

## Scope and remaining gates

The application now includes the common collection pipeline, named connections, independent fictional feeds and schedules, change events, buyer workflows, notification preferences, reviewer isolation, sanitizer approval, reset fencing and encrypted local backup/restore tooling. See [current release evidence](BUILD_STATUS.md) for checks executed against the deployed revision; historical availability alone does not verify a newer build.

Real Lenovo collection, authenticated supplier sessions, isolated browser-runner operation, inbound webhook processing and external message delivery remain future work. Private-method settings cannot execute a collector. Backups are manual and local; off-host recovery, independent key custody and automated backup cadence remain unconfigured. Keep the owner-only Access policy until authentication, isolation, sanitization, HTTPS and the intended sharing scope have been reviewed for actual invitees.

Cloudflare references: [Tunnel setup and hostname routing](https://developers.cloudflare.com/tunnel/get-started/), [token-file parameter](https://developers.cloudflare.com/tunnel/reference/run-parameters/#token-file), and [Access service credentials](https://developers.cloudflare.com/cloudflare-one/access-controls/service-credentials/service-tokens/).
