#!/usr/bin/env python3
"""Create project-local database credentials without printing their values."""
from pathlib import Path
import os
import secrets

root = Path(__file__).resolve().parents[1]
local = root / '.local'
local.mkdir(mode=0o700, exist_ok=True)
local.chmod(0o700)
paths = [local / name for name in ('admin-password', 'app-password', 'admin-dsn', 'app-dsn')]
if any(p.exists() for p in paths):
    if not all(p.is_file() and not p.is_symlink() for p in paths):
        raise SystemExit('Incomplete credential files. Restore the existing .local files; credentials were not replaced.')
    print('Existing local database credential files preserved.')
else:
    admin, app = secrets.token_hex(32), secrets.token_hex(32)
    contents = [admin, app,
                f'postgres://bwb_admin:{admin}@postgres:5432/buyersworkbench?sslmode=disable',
                f'postgres://bwb_app:{app}@postgres:5432/buyersworkbench?sslmode=disable']
    for path, content in zip(paths, contents, strict=True):
        # Directory mode 0700 restricts host access. Containers read individual
        # read-only mounted secret files as their own unprivileged UIDs.
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o444)
        with os.fdopen(fd, 'w') as handle:
            handle.write(content + '\n')
    print('Created local database credential files in restricted .local directory.')
