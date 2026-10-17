# Local supplier credential and session vault

Supplier credentials and future portal session documents can be stored in a local encrypted vault. The local administration CLI creates keys, stores or replaces a connection's opaque value, reports metadata, and deletes a value. It does not contact suppliers or enable a collector. Live Lenovo discovery, account authorization, session creation and the isolated browser runner remain separate work.

The vault is supplier-independent. Its caller must supply the connection UUID from the intended private workspace. The CLI does not access PostgreSQL and cannot verify that a connection exists or belongs to a particular workspace. The eventual runtime integration must enforce that trusted binding before resolving a reference. There is no HTTP vault endpoint, plaintext CLI output command, frontend credential form, automatic key discovery or environment fallback.

## Local setup and use

Choose the host and restricted local location before provisioning actual material. Store keys and value input files outside this repository and outside source archives. No runtime mounts or deployment configuration are added by this feature.

Create an operator-owned parent directory with permissions 0700, then create a key using exclusive creation. The example paths are placeholders for an operator-selected location, and the UUID must be replaced with the intended connection's ID:

```sh
go run ./cmd/bwb supplier-secret-key \
  --key-file /path/to/private-supplier-secrets/vault.key

go run ./cmd/bwb supplier-secret-set \
  --connection CONNECTION_UUID \
  --key-file /path/to/private-supplier-secrets/vault.key \
  --vault-dir /path/to/private-supplier-secrets/vault \
  --value-file /path/to/private-supplier-secrets/restricted-input

go run ./cmd/bwb supplier-secret-status \
  --connection CONNECTION_UUID \
  --key-file /path/to/private-supplier-secrets/vault.key \
  --vault-dir /path/to/private-supplier-secrets/vault

go run ./cmd/bwb supplier-secret-delete \
  --connection CONNECTION_UUID \
  --key-file /path/to/private-supplier-secrets/vault.key \
  --vault-dir /path/to/private-supplier-secrets/vault
```

The key contains exactly 32 randomly generated **raw bytes**, with no newline. Hex or base64 text is not accepted. `supplier-secret-key` refuses to overwrite an existing path. The parent directory must already exist or have an existing parent; the tool creates only its final directory. Key files and value input files require mode 0600 or 0400. Input values are 1 byte to 1 MiB and are preserved byte for byte, including line endings. An operator can use their chosen restricted editor or credential provisioning process to supply the input file; secret values are never command arguments.

The vault directory requires mode 0700; newly written vault files use 0600. Unsafe existing permissions and symbolic path components are refused. Permissions are not repaired silently. The final vault directory is created if absent, including for a status or delete operation. The selected key must authenticate the vault before any command can proceed.

Set and status return only a connection ID, opaque `vault:v1:UUID` reference, byte count and local file modification time. Missing status returns `configured: false`; repeated deletion succeeds. A reference is not an authorization token. Preserve only that reference in connection configuration, never the credential or portal storage document. Database job arguments, snapshots, events, audits and logs must contain neither credential contents nor browser session material. Nothing in the current runtime or frontend resolves these references.

Repeating set atomically replaces that connection's value with fresh randomized ciphertext, after authenticating its previous value. Validation and encryption failure preserve the previous entry. Atomic replacement prevents readers from observing a partially written file; a storage error after the final rename can still mean the replacement succeeded, so inspect metadata before retrying. Concurrent local operators must coordinate; the last completed update wins. A delete revokes only the local stored copy for that UUID. It does not invalidate an already issued supplier-side token, clear a runner's already decrypted memory, or contact a supplier.

## Encryption, recovery and runtime contract

The versioned envelope uses AES-256-GCM with a new cryptographic nonce for every write. Authenticated data binds the format version and canonical connection UUID, preventing a copied ciphertext from being accepted for another connection. An authenticated vault marker detects wrong keys when reopening a vault, including an otherwise empty vault. A missing marker in a populated vault is treated as damage. Tampered entries, wrong keys and unsafe files fail closed; errors omit file paths, underlying OS errors and secret contents.

Keep an offline backup of the raw key separately from the encrypted directory. Back up the entire encrypted directory, including its hidden `.vault-key-check` file; PostgreSQL backups do not include the vault or key. Restore both on the selected host with the required permissions. Possession of both the key and encrypted directory permits reading its contents; possession of only the database's opaque references does not. Losing the key means losing access to every value encrypted under it. There is no recovery key or default password.

Replacing a connection value rotates that value; it does not rotate the vault encryption key. The CLI deliberately refuses in-place encryption-key replacement. A future key migration must authenticate every existing entry, write and verify a separate vault using a new key, and switch the runtime location coherently. Until that exists, an operator can provision a new separate vault from authorized source credentials. Do not overwrite a key while retaining its old ciphertext directory.

`internal/secretvault.Open(vaultDir, keyFile)` opens the vault; `Put`, `Get`, `Status` and `Delete` accept the connection UUID. `Get` is a runtime package API and has no CLI counterpart. The consuming connector must enforce private-workspace ownership and source access policy before reading the value, interpret its own credential/session schema, and handle expiry, refresh and supplier-side revocation. It must not propagate input or provider errors into logs. Browser sessions must remain confined to the eventual isolated runner and must never be returned to the frontend.

Callers own decrypted `Get` buffers and should call `secretvault.Clear` promptly. The CLI clears input buffers after use, and key input buffers are cleared after cipher initialization. This reduces buffer lifetime; Go's memory manager, cipher key schedules, OS paging, process dumps and copies can retain sensitive material. `Close` releases the pinned directory and cipher reference; it does not guarantee memory erasure. Local host ownership, process isolation, backup handling and supplier session controls still apply.

Unit and race tests cover authentication/tampering, UUID binding, wrong keys, permissions and symbolic paths, bounded input, atomic replacements and concurrent reads, exclusive key creation, connection-specific revocation and redacted CLI output. Tests use invented opaque strings and create no supplier session or external connection.
