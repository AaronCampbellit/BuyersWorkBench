"""Stage a portable Scrapeyy session for the existing isolated Lenovo verifier."""
import argparse
import fcntl
import getpass
import json
import os
import pathlib
import stat
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent
MAXIMUM = 28 * 1024 * 1024


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--private-dir', type=pathlib.Path, required=True)
    parser.add_argument('--input', type=pathlib.Path, required=True)
    parser.add_argument('--passphrase-stdin', action='store_true', help='Read the exact UTF-8 secret from stdin instead of prompting; no trailing newline is removed.')
    args = parser.parse_args()
    private = args.private_dir.absolute()
    incoming = args.input.absolute()
    checkout = ROOT.parent.parent
    for path in [private, incoming]:
        if os.getuid() == 0 or path == checkout or checkout in path.parents or any(p.is_symlink() for p in [path, *path.parents]):
            raise ValueError('unsafe_private_path')
    info = private.stat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise ValueError('unsafe_private_path')
    descriptor = os.open(incoming, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'rb') as handle:
        info = os.fstat(handle.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.getuid() or info.st_mode & 0o077 or not 0 < info.st_size <= MAXIMUM:
            raise ValueError('unsafe_input_file')
        raw = handle.read(MAXIMUM + 1)
        if len(raw) > MAXIMUM:
            raise ValueError('unsafe_input_file')
        envelope = json.loads(raw)
        del raw
        encrypted = isinstance(envelope, dict) and envelope.get('format') == 'scrapeyy-session-encrypted'
        del envelope
    secret = b''
    if encrypted:
        if args.passphrase_stdin:
            secret = sys.stdin.buffer.read(4097)
        elif sys.stdin.isatty():
            secret = getpass.getpass('Scrapeyy PIN or passphrase: ').encode('utf-8')
        else:
            raise ValueError('passphrase_required')
        if not 0 < len(secret) <= 4096:
            raise ValueError('invalid_passphrase_length')
    descriptor = os.open(private / '.collection.lock', os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'w') as lock:
        info = os.fstat(lock.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.getuid() or info.st_mode & 0o077:
            raise ValueError('unsafe_private_path')
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError('collection_already_running')
        env = os.environ.copy()
        env['BWB_PROVISION_LOCK_HELD'] = '1'
        return subprocess.run(['node', str(ROOT / 'src/import-scrapeyy.ts'), '--private-dir', str(private), '--input', str(incoming)],
                              env=env, input=secret, stderr=subprocess.DEVNULL, timeout=60).returncode


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception as error:
        reasons = {'unsafe_private_path', 'unsafe_input_file', 'passphrase_required', 'invalid_passphrase_length', 'collection_already_running'}
        print(json.dumps({'outcome': 'failed', 'code': str(error) if str(error) in reasons else 'import_failed', 'activeSessionReplaced': False}))
        raise SystemExit(1)
