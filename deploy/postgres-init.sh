#!/bin/sh
set -eu
# Docker runs initialization scripts only for a new project-owned volume.
# Feed credentials over stdin; never put them in command arguments or logs.
task_app_password=$(cat /run/secrets/app_password)
case "$task_app_password" in *[!a-f0-9]*) exit 1;; esac
{
  printf "CREATE ROLE bwb_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD '%s';\n" "$task_app_password"
  printf 'REVOKE ALL ON DATABASE buyersworkbench FROM PUBLIC;\n'
  printf 'GRANT CONNECT ON DATABASE buyersworkbench TO bwb_app;\n'
  printf 'REVOKE CREATE ON SCHEMA public FROM PUBLIC;\n'
} | psql --no-psqlrc --set ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --quiet
unset task_app_password
