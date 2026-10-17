#!/usr/bin/env python3
"""Encrypt a local Compose PostgreSQL dump without a plaintext dump on disk."""
from pathlib import Path
import datetime as dt
import json
import os
import re
import stat
import subprocess
import sys
import uuid

ROOT = Path(__file__).resolve().parents[1]
LOCAL = ROOT / '.local'
BACKUPS = LOCAL / 'backups'
KEY = LOCAL / 'backup-key'
TOOL = LOCAL / 'bwb-backup-tool'
NAME = re.compile(r'^bwb-(\d{8})T\d{6}Z-[0-9a-f]{8}\.bwb$')


def private_directory(path):
    if not path.exists():
        path.mkdir(mode=0o700)
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o077:
        raise RuntimeError('Backup directories must be ordinary private directories with permissions 0700.')


def restricted_file(path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077:
        raise RuntimeError('Encryption keys and backups require regular files with permissions 0600 or 0400.')


def build_tool():
    private_directory(LOCAL)
    if TOOL.exists() or TOOL.is_symlink():
        if not stat.S_ISREG(TOOL.lstat().st_mode):
            raise RuntimeError('The local backup-tool output must be a regular file.')
    result = subprocess.run(['go', 'build', '-o', str(TOOL), './cmd/bwb'], cwd=ROOT,
                            stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=120)
    if result.returncode:
        raise RuntimeError('Cannot build the local backup tool; run go build ./cmd/bwb for diagnostics.')


def ensure_key():
    if KEY.exists() or KEY.is_symlink():
        restricted_file(KEY)
        if KEY.stat().st_size != 32:
            raise RuntimeError('The existing backup key is invalid; it was not replaced.')
    else:
        result = subprocess.run([str(TOOL), 'backup-key', '--key-file', str(KEY)],
                                stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=15)
        if result.returncode:
            raise RuntimeError('Cannot create the encryption key; no backup was written.')


def retain_daily():
    # Keep the newest snapshot for each of the fourteen most recent UTC days.
    # Delete only files with this script's exact generated name and private mode.
    days = {}
    for path in BACKUPS.iterdir():
        match = NAME.fullmatch(path.name)
        if not match:
            continue
        restricted_file(path)
        days.setdefault(match.group(1), []).append(path)
    retained = []
    for day in sorted(days, reverse=True):
        files = sorted(days[day], key=lambda p: p.name, reverse=True)
        if len(retained) < 14:
            retained.append(files.pop(0))
        for path in files:
            path.unlink()
    return len(retained)


def main():
    if len(sys.argv) != 1:
        raise RuntimeError('Usage: python3 scripts/backup-local.py (the project-local Compose database only).')
    private_directory(LOCAL)
    private_directory(BACKUPS)
    build_tool()
    ensure_key()
    password_path = LOCAL / 'admin-password'
    info = password_path.lstat()
    # Existing Compose credentials are 0444 inside a 0700 parent for secret mounts.
    if not stat.S_ISREG(info.st_mode):
        raise RuntimeError('The local admin credential must be a regular file.')
    password = password_path.read_bytes().strip()
    if not re.fullmatch(rb'[0-9a-f]{64}', password):
        raise RuntimeError('The existing local database credential has an unsupported format.')
    stamp = dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    target = BACKUPS / f'bwb-{stamp}-{uuid.uuid4().hex[:8]}.bwb'
    partial = BACKUPS / (target.name + '.partial')
    dump = encrypt = None
    try:
        # The password crosses stdin only. It is never a host command argument,
        # host environment value, log line, or part of the encrypted dump name.
        command = 'IFS= read -r task_backup_password; export PGPASSWORD="$task_backup_password"; exec pg_dump --host=127.0.0.1 --username=bwb_admin --dbname=buyersworkbench --format=custom --no-owner'
        dump = subprocess.Popen(['docker', 'compose', '-f', str(ROOT / 'compose.yaml'), 'exec', '-T',
                                 'postgres', 'sh', '-c', command], cwd=ROOT,
                                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        encrypt = subprocess.Popen([str(TOOL), 'backup-encrypt', '--key-file', str(KEY), '--output', str(partial)],
                                   stdin=dump.stdout, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        dump.stdout.close()
        dump.stdin.write(password + b'\n')
        dump.stdin.close()
        dump_code = dump.wait(timeout=120)
        metadata, _ = encrypt.communicate(timeout=30)
        if dump_code or encrypt.returncode:
            raise RuntimeError('Database dump or encryption failed; no completed backup was retained.')
        # An exclusive hard link publishes only a complete authenticated envelope.
        os.link(partial, target)
        partial.unlink()
        snapshots = retain_daily()
        result = json.loads(metadata)
        print(json.dumps({'backup': str(target), 'encrypted': result['encrypted'],
                          'bytes': result['bytes'], 'retained_daily_snapshots': snapshots,
                          'key_stored_separately': True}))
    finally:
        for process in (dump, encrypt):
            if process is not None and process.poll() is None:
                process.kill()
                process.wait()
        partial.unlink(missing_ok=True)


if __name__ == '__main__':
    try:
        main()
    except (OSError, RuntimeError, subprocess.SubprocessError, ValueError) as error:
        # Do not include subprocess output or source records in errors.
        print('Backup failed: ' + (str(error) if isinstance(error, RuntimeError) else 'local operation could not complete.'), file=sys.stderr)
        sys.exit(1)
