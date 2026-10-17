#!/usr/bin/env python3
"""Restore SQL contracts; opt in to offline disposable PostgreSQL integration."""
from pathlib import Path
import hashlib
import json
import os
import re
import subprocess
import time
import unittest
import uuid

from restore_sql import sanitation_sql, rls_probe_sql

ROOT = Path(__file__).resolve().parents[1]


def execute(arguments, data=None, timeout=60):
    result = subprocess.run(arguments, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    if result.returncode:
        raise RuntimeError('Disposable SQL fixture failed: ' + result.stderr.decode(errors='replace'))
    return result.stdout


class RestoreSQLUnitTest(unittest.TestCase):
    def test_probe_refuses_untrusted_identifiers(self):
        ids = [str(uuid.uuid4()) for _ in range(4)]
        for i in range(4):
            invalid = ids.copy()
            invalid[i] = "x'); SELECT 1; --"
            with self.assertRaises(ValueError):
                rls_probe_sql(*invalid)


@unittest.skipUnless(os.environ.get('BWB_TEST_RESTORE_SQL') == '1',
                     'set BWB_TEST_RESTORE_SQL=1 for a new offline disposable PostgreSQL container')
class RestoreSQLIntegrationTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        compose = (ROOT / 'compose.yaml').read_text()
        images = re.findall(r'image:\s*(postgres:16-alpine@sha256:[0-9a-f]{64})', compose)
        if len(images) != 1:
            raise RuntimeError('Cannot identify pinned PostgreSQL fixture image')
        cls.token = uuid.uuid4().hex
        cls.container = 'bwb-restore-sql-test-' + cls.token[:16]
        cls.created = False
        cls.addClassCleanup(cls.cleanup)
        execute(['docker', 'run', '--detach', '--pull', 'never', '--name', cls.container,
                 '--label', 'buyersworkbench.restore-sql-test=' + cls.token,
                 '--network', 'none', '--tmpfs', '/var/lib/postgresql/data:rw,nosuid',
                 '--env', 'POSTGRES_DB=postgres', '--env', 'POSTGRES_USER=bwb_admin',
                 '--env', 'POSTGRES_HOST_AUTH_METHOD=trust', images[0]])
        cls.created = True
        for _ in range(45):
            ready = subprocess.run(['docker', 'exec', cls.container, 'pg_isready', '--host=127.0.0.1',
                                    '--username=bwb_admin', '--dbname=postgres'],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if ready.returncode == 0:
                break
            time.sleep(1)
        else:
            raise RuntimeError('Disposable SQL fixture did not become ready')
        cls.sql('postgres', b'CREATE ROLE bwb_app NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;')

    @classmethod
    def cleanup(cls):
        if cls.created:
            label = execute(['docker', 'inspect', '--format',
                             '{{index .Config.Labels "buyersworkbench.restore-sql-test"}}', cls.container]).strip().decode()
            if label != cls.token:
                raise RuntimeError('Disposable fixture identity changed; cleanup refused')
            execute(['docker', 'rm', '--force', cls.container])

    @classmethod
    def sql(cls, database, statement):
        return execute(['docker', 'exec', '-i', cls.container, 'psql', '--no-psqlrc', '--username=bwb_admin',
                        '--dbname=' + database, '--set=ON_ERROR_STOP=1', '--tuples-only', '--no-align', '--quiet'], statement)

    def verify_version(self, version):
        source, restored = 'fixture_source_' + str(version), 'fixture_restored_' + str(version)
        self.sql('postgres', f'CREATE DATABASE {source}; CREATE DATABASE {restored};'.encode())
        self.sql(source, b'CREATE TABLE bwb_schema_migrations(version bigint PRIMARY KEY,name text NOT NULL,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now());')
        for path in sorted((ROOT / 'migrations').glob('*.sql')):
            number = int(path.name.split('_', 1)[0])
            if number > version:
                continue
            body = path.read_bytes()
            checksum = hashlib.sha256(body).hexdigest()
            ledger = f"INSERT INTO bwb_schema_migrations(version,name,checksum) VALUES({number},'{path.name}','{checksum}');".encode()
            self.sql(source, b'BEGIN;\n' + body + b'\n' + ledger + b'\nCOMMIT;')
        actor, workspace, private, connection, pending, accepted, order = [str(uuid.uuid4()) for _ in range(7)]
        self.sql(source, f"""INSERT INTO users(id,display_name,is_owner) VALUES('{actor}','Fixture owner',true);
        INSERT INTO workspaces(id,owner_user_id,kind,name,seed_version,business_date)
          VALUES('{workspace}','{actor}','presentation','Fixture workspace','fixture-seed','2026-10-19');
        INSERT INTO workspaces(id,owner_user_id,kind,name) VALUES('{private}','{actor}','private','Fixture private');
        INSERT INTO memberships(workspace_id,user_id,role) VALUES('{workspace}','{actor}','owner'),('{private}','{actor}','owner');
        INSERT INTO sessions(token_hash,user_id,csrf_token_hash,expires_at)
          VALUES(decode(repeat('11',32),'hex'),'{actor}',decode(repeat('22',32),'hex'),now()+interval '1 hour');
        INSERT INTO wb_connections(id,workspace_id,source_records,fence,active_run_id,lease_expires_at,schedule_enabled,
          cadence_seconds,next_due_at,scheduled_by_user_id,status,last_success_at,initialized_generation)
          VALUES('{connection}','{workspace}','[]',4,'{pending}',now()+interval '2 minutes',true,
            300,now(),'{actor}','queued',now()-interval '1 day',1);
        INSERT INTO wb_runs(id,workspace_id,generation,connection_id,actor_user_id,fence,state,finished_at,record_count)
          VALUES('{accepted}','{workspace}',1,'{connection}','{actor}',3,'succeeded',now()-interval '1 day',1),
                ('{pending}','{workspace}',1,'{connection}','{actor}',4,'queued',NULL,0);
        CREATE TABLE river_job(id bigint PRIMARY KEY,state text NOT NULL,finalized_at timestamptz);
        INSERT INTO river_job(id,state,finalized_at) VALUES(1,'available',NULL),(2,'completed',now());
        """.encode())
        connection_column = ',connection_id' if version >= 10 else ''
        connection_value = f",'{connection}'" if version >= 10 else ''
        self.sql(source, f"""INSERT INTO wb_orders(id,workspace_id,generation,source_key,record,first_record,first_observed_at,last_observed_at{connection_column})
          VALUES('{order}','{workspace}',1,'fixture-line','{{"status":"open"}}','{{"status":"open"}}',now()-interval '1 day',now()-interval '1 day'{connection_value});
        INSERT INTO wb_requirements(workspace_id,generation,source_key,data{connection_column})
          VALUES('{workspace}',1,'fixture-line','{{"internal_po":"Retained requirement"}}'{connection_value});
        INSERT INTO wb_observations(workspace_id,generation,order_id,run_id,record,observed_at)
          VALUES('{workspace}',1,'{order}','{accepted}','{{"status":"open"}}',now()-interval '1 day');
        INSERT INTO wb_changes(workspace_id,generation,order_id,run_id,field,data)
          VALUES('{workspace}',1,'{order}','{accepted}','confirmation','{{"before":"unavailable","after":"confirmed"}}');
        """.encode())
        placeholder = None
        if version >= 10:
            placeholder = str(uuid.uuid4())
            self.sql(source, f"""INSERT INTO wb_connections(id,workspace_id,name,source_records,is_demo_default,connector,origin,method,status)
              VALUES('{placeholder}','{private}','Pending portal','[]',false,'unconfigured','live_private','browser','not_configured');
            """.encode())
        snapshot = b"""SELECT json_build_object('orders',(SELECT jsonb_agg(o ORDER BY id) FROM wb_orders o),
          'requirements',(SELECT jsonb_agg(r ORDER BY source_key) FROM wb_requirements r),
          'observations',(SELECT jsonb_agg(o ORDER BY id) FROM wb_observations o),
          'events',(SELECT jsonb_agg(e ORDER BY id) FROM wb_changes e))::text;"""
        before = self.sql(source, snapshot)
        # Exercise the same custom-format dump/restore boundary as the real tool,
        # using only this container's synthetic fixture databases.
        dump = execute(['docker', 'exec', self.container, 'pg_dump', '--username=bwb_admin',
                        '--dbname=' + source, '--format=custom', '--no-owner'])
        self.assertTrue(dump.startswith(b'PGDMP'))
        execute(['docker', 'exec', '-i', self.container, 'pg_restore', '--username=bwb_admin',
                 '--dbname=' + restored, '--exit-on-error', '--no-owner'], dump)
        for _ in range(2):
            evidence = json.loads(self.sql(restored, sanitation_sql()))
            self.assertEqual(evidence['migrations'], version)
            self.assertTrue(all(evidence[k] for k in ('constraints_valid', 'sessions_revoked', 'runtime_role_constrained')))
            self.assertEqual(self.sql(restored, snapshot), before, 'sanitation changed accepted source evidence')
        paused_column = 'AND paused' if version >= 10 else ''
        self.assertEqual(self.sql(restored, f"""SELECT NOT schedule_enabled AND next_due_at IS NULL AND active_run_id IS NULL
          AND lease_expires_at IS NULL AND status='paused' AND fence>4 AND initialized_generation=1
          AND last_success_at IS NOT NULL {paused_column} FROM wb_connections WHERE id='{connection}';""".encode()).strip(), b't')
        self.assertEqual(self.sql(restored, f"SELECT state='cancelled' AND error_code='restore_sandbox' AND finished_at IS NOT NULL FROM wb_runs WHERE id='{pending}';".encode()).strip(), b't')
        self.assertEqual(self.sql(restored, b"SELECT NOT EXISTS(SELECT 1 FROM river_job WHERE state NOT IN ('completed','cancelled','discarded'));").strip(), b't')
        if placeholder:
            self.assertEqual(self.sql(restored, f"SELECT paused AND status='not_configured' AND last_success_at IS NULL AND NOT schedule_enabled AND active_run_id IS NULL FROM wb_connections WHERE id='{placeholder}';".encode()).strip(), b't')
        if version >= 9:
            self.assertEqual(self.sql(restored, b"SELECT NOT has_table_privilege('bwb_app','wb_changes','UPDATE') AND NOT has_table_privilege('bwb_app','wb_changes','DELETE');").strip(), b't')
        if version >= 12:
            self.assertEqual(self.sql(restored, b"SELECT NOT has_table_privilege('bwb_app','wb_observations','UPDATE') AND NOT has_table_privilege('bwb_app','wb_observations','DELETE');").strip(), b't')
        isolation = json.loads(self.sql(restored, rls_probe_sql(*(str(uuid.uuid4()) for _ in range(4)))))
        self.assertEqual(isolation, {'own': 1, 'foreign': 0})
        self.assertEqual(self.sql(source, snapshot), before, 'sandbox operations changed source fixture database')
        self.assertEqual(self.sql(source, f"SELECT schedule_enabled AND state='queued' AND revoked_at IS NULL FROM wb_connections c JOIN wb_runs r ON r.id=c.active_run_id JOIN sessions s ON s.user_id=r.actor_user_id WHERE c.id='{connection}';".encode()).strip(), b't')

    def test_version_eight_dump_restore(self):
        self.verify_version(8)

    def test_version_nine_dump_restore(self):
        self.verify_version(9)

    def test_latest_dump_restore_with_private_placeholder(self):
        latest = max(int(path.name.split('_', 1)[0]) for path in (ROOT / 'migrations').glob('*.sql'))
        self.verify_version(latest)


if __name__ == '__main__':
    unittest.main()
