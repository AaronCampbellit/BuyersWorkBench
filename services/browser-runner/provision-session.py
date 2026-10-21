"""Capture a candidate from the local Chrome MCP bridge under the collector lock."""
import argparse
import fcntl
import json
import os
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--private-dir', type=pathlib.Path, required=True)
    parser.add_argument('--mcp-url', default='http://localhost:8931/mcp')
    args = parser.parse_args()
    private = args.private_dir.absolute()
    if os.getuid() == 0 or any(p.is_symlink() for p in [private, *private.parents]):
        raise ValueError('unsafe_private_directory')
    info = private.stat()
    if not private.is_dir() or info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise ValueError('unsafe_private_directory')
    descriptor = os.open(private / '.collection.lock', os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'w') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError('collection_already_running')
        env = os.environ.copy()
        env['BWB_PROVISION_LOCK_HELD'] = '1'
        return subprocess.call(['node', str(ROOT / 'src/provision.ts'), '--private-dir', str(private), '--mcp-url', args.mcp_url],
                               env=env, stderr=subprocess.DEVNULL)


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception as error:
        reasons = {'unsafe_private_directory', 'collection_already_running'}
        print(json.dumps({'outcome': 'failed', 'code': str(error) if str(error) in reasons else 'provision_configuration_failed', 'activeSessionReplaced': False}))
        raise SystemExit(1)
