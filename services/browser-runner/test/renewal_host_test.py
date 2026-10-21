import datetime as dt
import importlib.util
import hashlib
import json
import os
import pathlib
import tempfile
import unittest

path = pathlib.Path(__file__).resolve().parents[1] / 'run-renewal-test.py'
spec = importlib.util.spec_from_file_location('renewal_host', path)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class RenewalHostTest(unittest.TestCase):
    def health(self):
        return {'schema': 1, 'clocks': {'complete': True,
                'passport': {'expiresAt': '2026-10-21T15:00:00Z'}, 'portal': {'expiresAt': '2026-10-21T15:30:00Z'}}}

    def test_renews_before_earliest_and_proves_after_both_original_deadlines(self):
        result = module.plan(self.health(), dt.datetime(2026, 10, 21, 12, tzinfo=dt.timezone.utc))
        self.assertEqual(result['renewAt'], '2026-10-21T14:50:00+00:00')
        self.assertEqual(result['verifyAt'], '2026-10-21T15:31:30+00:00')

    def test_expired_unknown_or_excessive_wait_never_start(self):
        with self.assertRaisesRegex(ValueError, 'fresh_session_required'):
            module.plan(self.health(), dt.datetime(2026, 10, 21, 15, tzinfo=dt.timezone.utc))
        health = self.health()
        health['clocks']['complete'] = False
        with self.assertRaisesRegex(ValueError, 'unknown_authentication_deadline'):
            module.plan(health, dt.datetime(2026, 10, 21, 12, tzinfo=dt.timezone.utc))
        with self.assertRaisesRegex(ValueError, 'test_window_too_short'):
            module.plan(self.health(), dt.datetime(2026, 10, 21, 12, tzinfo=dt.timezone.utc), 60)

    def test_private_progress_reports_replace_atomically_with_restricted_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            destination = root / 'progress.json'
            module.private_report(destination, {'stage': 'waiting_before_expiry'})
            self.assertEqual(destination.stat().st_mode & 0o777, 0o600)
            module.private_report(destination, {'stage': 'complete', 'outcome': 'verified'})
            self.assertEqual(json.loads(module.private_read(destination))['outcome'], 'verified')
            self.assertEqual(list(root.iterdir()), [destination])

    def test_health_is_bound_to_current_encrypted_state_and_rejects_unsafe_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / 'state').mkdir(mode=0o700)
            (root / 'data').mkdir(mode=0o700)
            with self.assertRaisesRegex(ValueError, 'session_health_missing'):
                module.read_health(root)
            session = root / 'state/session.enc'
            session.write_bytes(b'invented encrypted state')
            session.chmod(0o600)
            health = self.health()
            health['stateDigest'] = hashlib.sha256(session.read_bytes()).hexdigest()
            module.private_report(root / 'data/session-health.json', health)
            self.assertEqual(module.read_health(root), health)
            session.write_bytes(b'another invented encrypted state')
            with self.assertRaisesRegex(ValueError, 'session_health_mismatch'):
                module.read_health(root)
            session.chmod(0o644)
            with self.assertRaisesRegex(ValueError, 'unsafe_private_file'):
                module.private_read(session)
            session.chmod(0o600)
            os.link(session, root / 'linked.enc')
            with self.assertRaisesRegex(ValueError, 'unsafe_private_file'):
                module.private_read(session)
