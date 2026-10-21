"""Finite owner-operated pre-expiry renewal and post-deadline access test."""
import argparse
import datetime as dt
import fcntl
import hashlib
import json
import os
import pathlib
import signal
import stat
import subprocess
import sys
import time
import uuid

UTC = dt.timezone.utc
ROOT = pathlib.Path(__file__).resolve().parent


def timestamp(value):
    parsed = dt.datetime.fromisoformat(value.replace('Z', '+00:00'))
    if parsed.utcoffset() != dt.timedelta(0):
        raise ValueError('invalid_health')
    return parsed


def plan(health, now, maximum_minutes=240):
    clocks = health['clocks']
    if health.get('schema') != 1 or not clocks.get('complete'):
        raise ValueError('unknown_authentication_deadline')
    passport = timestamp(clocks['passport']['expiresAt'])
    portal = timestamp(clocks['portal']['expiresAt'])
    due = min(passport, portal) - dt.timedelta(minutes=10)
    verify = max(passport, portal) + dt.timedelta(seconds=90)
    if due <= now + dt.timedelta(seconds=30):
        raise ValueError('fresh_session_required')
    if verify >= now + dt.timedelta(minutes=maximum_minutes - 12):
        raise ValueError('test_window_too_short')
    return {'renewAt': due.isoformat(), 'verifyAt': verify.isoformat(),
            'originalPassportExpiry': passport.isoformat(), 'originalPortalExpiry': portal.isoformat()}


def private_read(path, limit=65536):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.getuid() or info.st_mode & 0o077 or not 0 < info.st_size <= limit:
            raise ValueError('unsafe_private_file')
        with os.fdopen(descriptor, 'rb', closefd=False) as handle:
            return handle.read(limit + 1)
    finally:
        os.close(descriptor)


def private_report(path, value):
    temporary = path.parent / ('.renewal-test-' + uuid.uuid4().hex)
    try:
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, 'w') as handle:
            json.dump(value, handle, indent=2)
            handle.flush()
            os.fsync(handle.fileno())
        temporary.replace(path)
    finally:
        temporary.unlink(missing_ok=True)


def read_health(private):
    try:
        health = json.loads(private_read(private / 'data/session-health.json'))
    except FileNotFoundError:
        raise ValueError('session_health_missing') from None
    digest = hashlib.sha256(private_read(private / 'state/session.enc', 16 * 1024 * 1024)).hexdigest()
    if health.get('stateDigest') != digest:
        raise ValueError('session_health_mismatch')
    return health


def run_once(private, mode, remaining):
    if remaining <= 0:
        raise ValueError('test_deadline_reached')
    child = subprocess.Popen([sys.executable, str(ROOT / 'run-isolated.py'), '--private-dir', str(private), '--mode', mode],
                             stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
    try:
        output, _ = child.communicate(timeout=min(720, remaining))
    finally:
        if child.poll() is None:
            child.terminate()
            try:
                child.wait(timeout=45)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait()
    rows = []
    for line in output.splitlines():
        try:
            row = json.loads(line)
            if row.get('outcome') in ('success', 'failed'):
                rows.append(row)
        except (ValueError, AttributeError):
            pass
    if child.returncode or not rows or rows[-1].get('outcome') != 'success':
        raise ValueError('collector_failed')
    return rows[-1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--private-dir', required=True, type=pathlib.Path)
    parser.add_argument('--max-minutes', type=int, default=240)
    parser.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    private = args.private_dir.absolute()
    if os.getuid() == 0 or not 30 <= args.max_minutes <= 360 or any(p.is_symlink() for p in [private, *private.parents]):
        raise ValueError('invalid_test_configuration')
    for directory in (private, private / 'state', private / 'data'):
        info = directory.stat()
        if not directory.is_dir() or info.st_uid != os.getuid() or info.st_mode & 0o077:
            raise ValueError('unsafe_private_directory')
    health = read_health(private)
    now = dt.datetime.now(UTC)
    schedule = plan(health, now, args.max_minutes)
    if args.dry_run:
        print(json.dumps({'outcome': 'planned', **schedule, 'supplierRequestsMade': False}))
        return 0
    lock_fd = os.open(private / '.renewal-test.lock', os.O_CREAT | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
    with os.fdopen(lock_fd, 'w') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError('renewal_test_already_running')
        result = {'schema': 1, 'testId': str(uuid.uuid4()), 'startedAt': now.isoformat(),
                  'deadline': (now + dt.timedelta(minutes=args.max_minutes)).isoformat(), **schedule,
                  'stage': 'waiting_before_expiry', 'recurringScheduleEnabled': False}
        destination = private / 'data/renewal-test-last.json'
        private_report(destination, result)
        print(json.dumps(result), flush=True)
        expected = health['stateDigest']

        def wait_until(target):
            while dt.datetime.now(UTC) < target:
                remaining = (timestamp(result['deadline']) - dt.datetime.now(UTC)).total_seconds()
                if remaining <= 0:
                    raise ValueError('test_deadline_reached')
                if read_health(private)['stateDigest'] != expected:
                    raise ValueError('session_changed_during_test')
                time.sleep(min(30, remaining, max(.1, (target - dt.datetime.now(UTC)).total_seconds())))

        try:
            wait_until(timestamp(schedule['renewAt']))
            result['stage'] = 'renewing_before_expiry'
            private_report(destination, result)
            renewed = run_once(private, 'renew', (timestamp(result['deadline']) - dt.datetime.now(UTC)).total_seconds())
            result['renewal'] = renewed.get('renewal')
            if result['renewal'] is None or result['renewal'].get('status') != 'full':
                raise ValueError('both_authentication_clocks_not_extended')
            health = read_health(private)
            expected = health['stateDigest']
            if timestamp(health['clocks']['limitingExpiryAt']) <= timestamp(schedule['verifyAt']) + dt.timedelta(minutes=10):
                raise ValueError('extension_too_short_for_independent_proof')
            result['stage'] = 'waiting_after_original_deadlines'
            private_report(destination, result)
            wait_until(timestamp(schedule['verifyAt']))
            result['stage'] = 'verifying_after_original_deadlines'
            private_report(destination, result)
            verified = run_once(private, 'collect', (timestamp(result['deadline']) - dt.datetime.now(UTC)).total_seconds())
            result.update(stage='complete', outcome='verified', postDeadlineRunId=verified['runId'])
            return_code = 0
        except KeyboardInterrupt:
            result.update(stage='stopped', outcome='not_verified', reason='cancelled')
            return_code = 130
        except Exception as error:
            reasons = {'collector_failed', 'both_authentication_clocks_not_extended', 'extension_too_short_for_independent_proof',
                       'test_deadline_reached', 'session_changed_during_test', 'session_health_mismatch'}
            result.update(stage='stopped', outcome='not_verified', reason=str(error) if str(error) in reasons else 'test_failed')
            return_code = 1
        result['finishedAt'] = dt.datetime.now(UTC).isoformat()
        private_report(destination, result)
        print(json.dumps(result), flush=True)
        return return_code


def interrupted(_signum, _frame):
    raise KeyboardInterrupt()


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGHUP, interrupted)
    try:
        raise SystemExit(main())
    except (Exception, KeyboardInterrupt) as error:
        known = {'unknown_authentication_deadline', 'fresh_session_required', 'test_window_too_short', 'session_health_mismatch', 'session_health_missing',
                 'invalid_test_configuration', 'unsafe_private_directory', 'renewal_test_already_running'}
        print(json.dumps({'outcome': 'not_started', 'reason': str(error) if str(error) in known else 'test_configuration_failed'}))
        raise SystemExit(1)
