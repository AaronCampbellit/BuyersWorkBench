# 0012: Bounded Lenovo session renewal experiments

Date: October 20, 2026. Scope: owner-authorized standalone experiments; no application collector activation.

Reusing the copied session permits immediate reads, but does not establish how an operated runner renews authentication. The owner authorized tests of normal renewal and cookie date changes. Each comparison used an isolated VM context; the source Windows profile's cookies were not changed. The expanded encrypted capsule included Lenovo passport and business SSO cookies, alongside the approved account origin's storage.

Ordinary reads left the original portal cookie deadline unchanged. Extending copied cookie retention dates succeeded before that deadline, which does not establish a server-side extension. Expired portal cookies and an empty-state control initially could not read the selected order.

The default headless browser could read orders using a valid capsule, but the passport SSO script received 403 HTML during renewal. Chromium's normal headed mode under the container image's existing Xvfb completed the website's own SSO synchronization and token renewal. It issued fresh portal cookies and read the same source order without entering a password or MFA. The successful test did not replace authentication responses, disable browser access controls or fabricate a token.

The renewed state was encrypted and saved atomically outside the repository. A new headless browser process restored it and matched source order/ecommerce/customer identities. A candidate runner can therefore use a virtual display for normal renewal and an encrypted saved session between runs. This is an experimentally verified implementation option, not an enabled application transport or a claim of indefinite authentication.

The finite follow-up after the original portal-cookie deadline completed October 20. Extending copied browser retention dates without SSO did not recover the order. Neither the original capsule nor a headed normal SSO attempt with its copied state recovered it; that SSO attempt observed no renewal response. A previously normally renewed saved capsule still matched the same order. This result does not identify the failed SSO cause, isolate server token expiry or establish the longer-lived business-SSO lifetime. See the [sanitized evidence](../evidence/2026-10-20-lenovo-expiry.json).

Dependable natural/multi-day renewal, revocation and normal attended recovery remain open. Cookie-date edits are not the runner's renewal mechanism. The standalone implementation now classifies failures and preserves last-good captures; trusted application connection/vault binding, cadence, complete canonical mapping and application ingestion remain pending. Continue from [LENOVO_HANDOFF.md](../LENOVO_HANDOFF.md).
