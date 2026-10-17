#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ ! -t 0 ]]; then
  printf '%s\n' 'Run make owner in an interactive terminal. For automation use the documented password-file CLI.' >&2
  exit 1
fi
read -r -p 'Owner email: ' task_owner_email
read -r -p 'Display name: ' task_owner_name
read -r -s -p 'Owner password (at least 14 characters): ' task_owner_password
printf '\n'
read -r -s -p 'Confirm password: ' task_owner_confirm
printf '\n'
if [[ "$task_owner_password" != "$task_owner_confirm" ]]; then
  printf '%s\n' 'Passwords did not match.' >&2
  exit 1
fi
task_secret_dir=$(mktemp -d)
trap 'rm -rf -- "$task_secret_dir"' EXIT
printf '%s' "$task_owner_password" > "$task_secret_dir/password"
unset task_owner_password task_owner_confirm
chmod 0400 "$task_secret_dir/password"
docker compose --profile tools run --rm --no-deps \
  --user "$(id -u):$(id -g)" \
  --volume "$task_secret_dir/password:/run/owner-password:ro" \
  admin bootstrap-owner --email "$task_owner_email" --name "$task_owner_name" \
  --password-file /run/owner-password
