# Local encrypted backups and isolated restore checks

Run these commands from the BuyersWorkBench project directory:

```sh
python3 scripts/backup-local.py
python3 scripts/restore-test.py
```

The backup command reads only the project's Compose PostgreSQL service. It pipes
a PostgreSQL custom-format dump directly into the local Go encryption tool;
there is no plaintext dump file. The password crosses the Docker exec process's
stdin and is not included in host arguments, host environment variables, output,
or backup filenames. The dump includes database records, migration history,
constraints, policies, and role grants. Database credentials and PostgreSQL role
passwords are separate from this database dump.

The encrypted output uses AES-256-GCM with a fresh random nonce and an
authenticated version header. Backups are bounded to 256 MiB of plaintext for
this small demo; a larger database requires a reviewed streaming-backup design.
The default private locations are:

| Item | Location | Permissions |
|---|---|---|
| Encryption key | `.local/backup-key` | 0600; 32 random bytes |
| Encrypted snapshots | `.local/backups/bwb-<UTC timestamp>-<random suffix>.bwb` | 0600 |
| Parent directories | `.local/` and `.local/backups/` | 0700 |

The key is created only if absent. An invalid existing key is rejected and never
replaced. Key and ciphertext files are separate; existing files and symbolic
links are never overwritten. Backup retention keeps the latest successful
snapshot per UTC date for the fourteen most recent dates with snapshots. Only
the script's exact generated filenames are eligible for deletion; unrelated
files are preserved. The script publishes a completed backup only after both
`pg_dump` and encryption succeed.

These commands are manual. No scheduled backup, off-host copy, or key escrow is
configured. Preserve the encryption key separately from the backups before
relying on recovery after a host failure. Losing the key makes these backups
unrecoverable. The project's existing PostgreSQL app/admin passwords must also
be recovered or securely reprovisioned when recreating a real service.

The restore check accepts only generated files in this project's private backup
directory. It defaults to the latest snapshot, or accepts an explicit file:

```sh
python3 scripts/restore-test.py --backup /absolute/project/path/.local/backups/bwb-<timestamp>-<suffix>.bwb
```

It authenticates and decrypts the dump in memory, starts the already cached
pinned PostgreSQL image in a newly named container, and restores through stdin.
The container has `--network none`, no published ports, no source data volume,
and a tmpfs database directory. Its local-only `trust` initialization exists only
inside that network-isolated disposable container; no application or worker is
started. The test creates a constrained `bwb_app` role before restoring the
database's grants, disables restored schedules, cancels copied pending jobs,
and revokes copied sessions. It checks migration history, constraints, runtime
role attributes, and—when the backup contains domain tables—actual own/foreign
workspace visibility as `bwb_app`.

Only the newly created container with the matching random ownership label is
removed afterward. No source database, service, volume, network, user account,
or supplier session is changed by a restore test. JSON output records the
observed checks and any inapplicable domain test. A successful isolated check is
restore evidence, not an in-place disaster-recovery or live supplier test.

The low-level CLI also exposes `backup-key`, `backup-encrypt`, and
`backup-decrypt`; use the scripts for ordinary operation. Decryption requires
an explicit `--stdout` restore pipe and refuses to print plaintext to a terminal.
Memory buffers necessarily contain the unencrypted dump during backup/restore;
neither implementation claims guaranteed memory erasure.
