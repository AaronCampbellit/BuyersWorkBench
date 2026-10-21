# 0011: Bounded Lenovo session portability probe

Date: October 20, 2026. Scope: owner-authorized discovery and a standalone session-transfer experiment; no production collector activation.

The owner supplied a logged-in Lenovo Pro US tab and requested a VM Playwright test. We used the existing browser connection to read the selected order, export only relevant Lenovo cookies and the current account origin's local/session storage, and encrypt the export before it left the Windows runtime. A private VM key decrypted the capsule only inside the probe process. Plaintext session files, passkey exports, raw order artifacts and application credential provisioning were excluded.

The VM probe used pinned Playwright 1.64.1 and the matching official container image. Non-root Chromium sandboxing, a read-only filesystem, tmpfs browser files, resource bounds, dropped capabilities and a network-none container constrained the browser. Its only outbound path was a mounted Unix-socket proxy with exact observed hosts, port 443 and public-IP validation. This avoids an open browser debugging port or a broadly connected browser container. The seccomp profile additionally allowed namespace-local `chroot` so Chromium's sandbox could start with capabilities dropped.

Two fresh browser processes restored the same capsule, observed successful website order-detail JSON and matched source order/ecommerce/customer identities. This establishes immediate portability for the selected order. It does not implement the common collection contract, publish private workspace facts or establish long-term authentication, recurring access permission, cadence or account-wide completeness. The existing application restrictions remain in place.

Next work must bind an operated runner to the trusted private connection, complete stable line/shipment identities and date semantics, test session expiry/recovery, verify complete scope and failure preservation, and pass the one-order application ingestion proof. Exact executed evidence and remaining skips are in [BUILD_STATUS.md](../BUILD_STATUS.md) and [LENOVO_DISCOVERY.md](../intake/LENOVO_DISCOVERY.md).
