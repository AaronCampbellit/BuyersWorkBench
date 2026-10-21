# 0015: Attended provisioning and proactive Lenovo renewal

Date: October 21, 2026

The saved session failed after an overnight gap. No recurring renewal had been configured. Reactive SSO was reached, but HTTP 200 responses did not produce usable portal authentication. Ordinary reads had not extended the original cookie dates. A repeating collection timer alone is therefore not evidence that authentication will remain valid.

## Decision

Keep deterministic collection in the isolated VM. Add explicit `collect`, `renew` and `verify-session` modes. During collection, invoke normal website SSO when the earliest known Passport or portal expiry is within ten minutes. `renew` forces that same normal flow while reads still work. Preserve the existing reviewed requests, bounded retries, non-root sandbox, network-none container and exact-host egress policy. Do not edit cookie dates, enter credentials, submit challenges or contact new supplier hosts.

Measure Passport (`LPSWUST`, `LPSWUTGT`) and portal (`gateKeeper-token`, `online-token`) clocks independently. Long-retention and business-SSO cookie dates do not replace either clock. Classify a successful run's observed renewal as full, portal-only, Passport-only, unchanged, unknown or not attempted. Full requires both clocks to advance plus complete authenticated collection. Cookie metadata remains a planning signal; server-side validity requires subsequent real reads. Diagnostics expose only fixed error codes and invocation/response counts.

Persist `session-health.json` after successful collection, bound by SHA-256 to the encrypted saved session. A finite host test uses that metadata to renew once ten minutes before the earlier original deadline and collect once ninety seconds after the later original deadline. It stops if either clock fails to advance, the extension is too short for an independent proof, another process changes state, cancellation occurs or the bounded deadline expires. Default duration is four hours, capped at six. This does not install a recurring collector or keep-alive service. A passing interval would not establish multi-day reliability.

For attended recovery, the owner signs in normally in their selected Lenovo order tab. A separate host provisioning command connects through a loopback-only extension-backed MCP server/SSH tunnel, reloads that selected source page and verifies its order response against the independently configured account. It captures only Lenovo-scoped cookies and account-origin local/session storage. AES-GCM encryption and RSA-OAEP wrapping run on the source device; only ciphertext crosses the bridge and the private key stays on the VM. No whole Chrome profile, raw state file or private response diagnostic is saved.

The provisioning wrapper shares the collector's lock and writes only an encrypted candidate. A separate isolated `verify-session` run promotes it after the full account, scope, pagination and history checks succeed. Wrong-account/source failures preserve active credentials, the previous candidate and accepted facts. A valid candidate does not by itself establish VM portability. Interactive password/MFA entry remains an operator action; the Go vault is not integrated.

## Verification boundary

Invented-fixture tests cover source-side encryption and serialization, wrong-tab/account rejection, loopback MCP transport, staged provisioning with active-state/history preservation, renewal timing/classification and collection ordering. Host tests cover both original deadlines, bounded planning, atomic private progress and state/health binding. The expired real saved state still returns `authentication_required` under explicit renewal without changing the active encrypted state or accepted pointer.

Fresh-source capture, natural before-expiry renewal and a post-original-deadline read must be recorded in BUILD_STATUS when actually completed. Until then, do not claim unattended renewal or enable an indefinite schedule. The [runner instructions](../../services/browser-runner/README.md) contain the reproducible attended and finite-test commands.
