#!/usr/bin/env python3
"""Restore one encrypted backup into an isolated disposable PostgreSQL container."""
from pathlib import Path
import argparse
import json
import re
import subprocess
import sys
import time
import uuid

from restore_sql import sanitation_sql, rls_probe_sql

# The hyphenated sibling is deliberately loaded as local Python source only;
# there are no third-party Python dependencies and no supplier connector code.
import importlib.util
_spec = importlib.util.spec_from_file_location('bwb_local_backup', Path(__file__).with_name('backup-local.py'))
backup = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(backup)


def command(arguments, data=None, timeout=60):
    result = subprocess.run(arguments, input=data, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=timeout)
    if result.returncode:
        raise RuntimeError('Disposable restore operation failed; no source database was modified.')
    return result.stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--backup', help='An encrypted file inside this project\'s .local/backups; defaults to latest.')
    args = parser.parse_args()
    backup.private_directory(backup.LOCAL)
    backup.private_directory(backup.BACKUPS)
    if args.backup:
        path = Path(args.backup).absolute()
        if path.parent != backup.BACKUPS or not backup.NAME.fullmatch(path.name):
            raise RuntimeError('Restore tests accept only generated encrypted files in this project\'s .local/backups.')
    else:
        paths = sorted(p for p in backup.BACKUPS.iterdir() if backup.NAME.fullmatch(p.name))
        if not paths:
            raise RuntimeError('No project-local encrypted backup exists. Run backup-local.py first.')
        path = paths[-1]
    backup.restricted_file(path)
    backup.restricted_file(backup.KEY)
    backup.build_tool()
    # Decryption authenticates the full envelope before any restore is started.
    plaintext = command([str(backup.TOOL), 'backup-decrypt', '--key-file', str(backup.KEY),
                         '--input', str(path), '--stdout'], timeout=60)
    if not plaintext.startswith(b'PGDMP'):
        raise RuntimeError('Decrypted input is not a PostgreSQL custom-format dump.')
    compose = (backup.ROOT / 'compose.yaml').read_text()
    images = re.findall(r'image:\s*(postgres:16-alpine@sha256:[0-9a-f]{64})', compose)
    if len(images) != 1:
        raise RuntimeError('Cannot identify the project\'s pinned PostgreSQL image.')
    image = images[0]
    command(['docker', 'image', 'inspect', image])
    token = uuid.uuid4().hex
    name = 'bwb-restore-' + token[:16]
    created = False
    try:
        # No network, ports, source volumes, host credentials, app, or worker.
        # Restored database files live only in the disposable container's tmpfs.
        command(['docker', 'run', '--detach', '--pull', 'never', '--name', name,
                 '--label', 'buyersworkbench.restore-test=' + token,
                 '--network', 'none', '--tmpfs', '/var/lib/postgresql/data:rw,nosuid',
                 '--env', 'POSTGRES_DB=buyersworkbench', '--env', 'POSTGRES_USER=bwb_admin',
                 '--env', 'POSTGRES_HOST_AUTH_METHOD=trust', image])
        created = True
        for attempt in range(45):
            ready = subprocess.run(['docker', 'exec', name, 'pg_isready', '--username=bwb_admin',
                                    '--dbname=buyersworkbench'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if ready.returncode == 0:
                # The entrypoint's temporary server uses a Unix socket. Wait for
                # its final TCP server to ensure initialization has completed.
                tcp = subprocess.run(['docker', 'exec', name, 'pg_isready', '--host=127.0.0.1',
                                       '--username=bwb_admin', '--dbname=buyersworkbench'],
                                      stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                if tcp.returncode == 0:
                    break
            time.sleep(1)
        else:
            raise RuntimeError('Disposable PostgreSQL did not become ready.')
        psql = ['docker', 'exec', '-i', name, 'psql', '--no-psqlrc', '--username=bwb_admin',
                '--dbname=buyersworkbench', '--set=ON_ERROR_STOP=1', '--tuples-only', '--no-align', '--quiet']
        command(psql, b'CREATE ROLE bwb_app NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;\n')
        command(['docker', 'exec', '-i', name, 'pg_restore', '--exit-on-error', '--no-owner',
                 '--username=bwb_admin', '--dbname=buyersworkbench'], plaintext, timeout=120)
        plaintext = b''
        # Disable all restored collection schedules and revoke copied sessions.
        # No worker ever starts, and network=none also fences future connectors.
        safe = sanitation_sql()
        evidence = json.loads(command(psql, safe))
        if evidence['migrations'] < 1 or not all(evidence[k] for k in ('constraints_valid', 'sessions_revoked', 'runtime_role_constrained')):
            raise RuntimeError('Restored database did not satisfy integrity or isolation prerequisites.')
        has_domain = command(psql, b"SELECT to_regclass('public.wb_orders') IS NOT NULL;").strip() == b't'
        evidence['runtime_rls_probe'] = 'not applicable: backup predates domain tables'
        if has_domain:
            actor, foreign_actor, own, foreign = (str(uuid.uuid4()) for _ in range(4))
            probe = rls_probe_sql(actor, foreign_actor, own, foreign)
            isolation = json.loads(command(psql, probe))
            if isolation != {'own': 1, 'foreign': 0}:
                raise RuntimeError('Actual restored runtime-role workspace isolation failed.')
            evidence['runtime_rls_probe'] = 'passed: own row visible, foreign row denied'
        evidence.update({'restored': True, 'backup': path.name, 'source_database_unchanged': True,
                         'network': 'none', 'workers_started': False, 'restored_schedules_disabled': True,
                         'database_storage': 'disposable tmpfs', 'container_removed': True})
    finally:
        plaintext = b''
        if created:
            label = command(['docker', 'inspect', '--format', '{{index .Config.Labels "buyersworkbench.restore-test"}}', name]).strip().decode()
            if label != token:
                raise RuntimeError('Restore container identity changed; cleanup refused.')
            command(['docker', 'rm', '--force', name])
    print(json.dumps(evidence))


if __name__ == '__main__':
    try:
        main()
    except (OSError, RuntimeError, subprocess.SubprocessError, ValueError) as error:
        print('Restore test failed: ' + (str(error) if isinstance(error, RuntimeError) else 'isolated operation could not complete.'), file=sys.stderr)
        sys.exit(1)
