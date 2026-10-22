# Portable Scrapeyy session import

Date: October 22, 2026. Status: implemented for the standalone collector;
real owner-provided Firefox export authenticated on the VM. Renewal remains pending.

The owner requested a general Scrapeyy Borrow session feature with optional
PIN/passphrase encryption, reusable across VMs/projects. Its v1 export is
destination-independent: browser cookie attributes and explicit origin storage
are either plaintext or PBKDF2-SHA256/AES-256-GCM encrypted. The extension uses
600,000 iterations, a fresh 16-byte salt and 12-byte IV, a 128-bit tag and
`scrapeyy-session-encrypted:1` authenticated context. Secrets are UTF-8 without
trimming/normalization. Optional encryption is an explicit user choice.

The standalone Lenovo importer accepts that public format. It bounds KDF work
and file sizes before decryption, restricts origins/cookie applicability, retains
host-only scope/expiry and rejects unsupported partitions. It records the
configured expected customer; the bundle itself cannot prove customer identity.
It writes an encrypted candidate under the existing collection lock using each
destination VM's own RSA key. The existing isolated full account/collection/
history verification remains the only promotion path. It makes no supplier
request, changes no active session or history and installs no recurring job.

PIN entry uses a hidden terminal prompt, or an explicitly selected stdin pipe.
There is no secret argument/environment option. The source file is read with
owner/mode/size/link checks. Errors are fixed codes and never expose provider
data, PINs or raw parser exceptions. Source files are not automatically deleted.

Synthetic tests cover extension-produced golden files, WebCrypto interoperability,
wrong secret and tampering, KDF bounds, origin/cookie/storage rejection, original
expiry, private files and preservation of active/candidate/history on failure.
The source browser is never logged out or mutated by this importer. Copying
authentication cannot renew it; natural-expiry success remains unverified.

The first real Firefox export represented unpartitioned cookies with
`partitionKey: null`. The receiver now accepts this representation; non-null
partition objects are still unsupported. The exporter normalizes null to an
omitted field for new files. That export contained only the storefront origin,
including applicable parent-domain portal cookies. A disposable VM probe with
empty account storage authenticated, so the normal importer now supports this
case explicitly. Account storage is left empty, never copied from another
origin; full isolated identity/scope/history verification remains mandatory.
Import metadata reports missing account storage and incomplete renewal cookies.
Passport and business-SSO state was absent, so this is session portability
evidence, not normal renewal or overnight persistence evidence.

Application Go/vault/database integration, deployment and scheduling remain
outside this change. See the [operator instructions](../../services/browser-runner/README.md#import-a-portable-scrapeyy-session).
