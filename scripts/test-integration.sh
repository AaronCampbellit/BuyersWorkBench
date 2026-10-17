#!/usr/bin/env bash
# A fresh database container and loopback-only random port per invocation.
set -euo pipefail
cd "$(dirname "$0")/.."
task_test_dir=$(mktemp -d)
task_test_container="bwb-test-$(id -u)-$$"
task_started=false
cleanup() {
  if [[ "$task_started" == true ]] || docker container inspect "$task_test_container" >/dev/null 2>&1; then
    docker rm -f "$task_test_container" >/dev/null
  fi
  rm -rf -- "$task_test_dir"
}
trap cleanup EXIT
python3 - "$task_test_dir" <<'PY'
from pathlib import Path
import secrets, sys
folder = Path(sys.argv[1])
for name in ('admin-password', 'app-password'):
    path = folder / name
    path.write_text(secrets.token_hex(32) + '\n')
    path.chmod(0o444)
PY
docker run --detach --name "$task_test_container" \
  --publish 127.0.0.1::5432 \
  --env POSTGRES_DB=buyersworkbench --env POSTGRES_USER=bwb_admin \
  --env POSTGRES_PASSWORD_FILE=/run/secrets/admin_password \
  --env POSTGRES_INITDB_ARGS=--auth-host=scram-sha-256 \
  --mount "type=bind,source=$task_test_dir/admin-password,target=/run/secrets/admin_password,readonly" \
  --mount "type=bind,source=$task_test_dir/app-password,target=/run/secrets/app_password,readonly" \
  --mount "type=bind,source=$PWD/deploy/postgres-init.sh,target=/docker-entrypoint-initdb.d/01-bwb.sh,readonly" \
  --tmpfs /var/lib/postgresql/data:rw,nosuid \
  postgres:16-alpine@sha256:721873c34ceb9f8d8fc265984940dc982404c105f19ad51be9fdc5970a6080ea >/dev/null
task_started=true
for task_attempt in {1..30}; do
  # The host port is not open until initial scripts and the temporary server finish.
  task_test_port=$(docker port "$task_test_container" 5432/tcp)
  task_test_port=${task_test_port##*:}
  if docker exec "$task_test_container" pg_isready -h 127.0.0.1 -U bwb_admin -d buyersworkbench >/dev/null 2>&1; then
    break
  fi
  if [[ "$task_attempt" == 30 ]]; then
    printf '%s\n' 'Disposable PostgreSQL failed to become ready.' >&2
    exit 1
  fi
  sleep 1
done
python3 - "$task_test_dir" "$task_test_port" <<'PY'
from pathlib import Path
import os, subprocess, sys
folder, port = Path(sys.argv[1]), sys.argv[2]
env = os.environ.copy()
for name, role, variable in [('admin-password', 'bwb_admin', 'BWB_TEST_DATABASE_URL'),
                             ('app-password', 'bwb_app', 'BWB_TEST_APP_DATABASE_URL')]:
    password = (folder / name).read_text().strip()
    env[variable] = f'postgres://{role}:{password}@127.0.0.1:{port}/buyersworkbench?sslmode=disable'
env.pop('DATABASE_URL_FILE', None)
env['DATABASE_URL'] = env['BWB_TEST_DATABASE_URL']
subprocess.run(['go', 'run', './cmd/bwb', 'migrate'], env=env, check=True)
# Database packages run sequentially; each package's tests control their own rows.
sys.exit(subprocess.call(['go', 'test', '-p', '1', '-count=1', '-v', './...'], env=env))
PY
