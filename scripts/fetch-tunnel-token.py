#!/usr/bin/env python3
"""Fetch this project's existing tunnel token into a restricted local file."""
from pathlib import Path
import json
import os
import stat
import urllib.error
import urllib.request

root = Path(__file__).resolve().parents[1]
config = json.loads((root / 'deploy/cloudflare.json').read_text())
token = os.environ.get('CLOUDFLARE_API_TOKEN')
if not token:
    credential = Path.home() / '.config/cloudflare/fleet-api-token'
    if not credential.is_file() or stat.S_IMODE(credential.stat().st_mode) != 0o600:
        raise SystemExit('Provision the approved Cloudflare credential locally with mode 0600.')
    token = credential.read_text().strip()
url = f"https://api.cloudflare.com/client/v4/accounts/{config['account_id']}/cfd_tunnel/{config['tunnel_id']}/token"
request = urllib.request.Request(url, headers={'Authorization': 'Bearer ' + token})
try:
    with urllib.request.urlopen(request, timeout=30) as response:
        result = json.load(response)
except urllib.error.HTTPError as error:
    raise SystemExit(f'Tunnel-token retrieval failed with HTTP {error.code}; no credentials printed.')
if not result.get('success') or not isinstance(result.get('result'), str) or not result['result']:
    raise SystemExit('Tunnel-token retrieval failed; no credentials printed.')
local = root / '.local'
if not local.is_dir() or stat.S_IMODE(local.stat().st_mode) != 0o700:
    raise SystemExit('Run local setup first; .local must be a restricted mode 0700 directory.')
temporary = local / 'cloudflared-token.pending'
descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o400)
try:
    with os.fdopen(descriptor, 'w') as handle:
        handle.write(result['result'] + '\n')
    os.replace(temporary, local / 'cloudflared-token')
finally:
    temporary.unlink(missing_ok=True)
print('Saved the existing tunnel token in restricted .local/cloudflared-token; no secret printed.')
