"""One deterministic, bounded collection in a sandboxed network-none container."""
import argparse
import fcntl
import json
import os
import pathlib
import signal
import subprocess
import sys
import time
import uuid

ROOT = pathlib.Path(__file__).resolve().parent
IMAGE = 'mcr.microsoft.com/playwright:v1.64.1-noble@sha256:7852dc5a7d72f5fe458a597d690f6c42fbc8e9e26ddfa95f8e6ef552558d9bbf'


def safe_directory(path):
    if not path.exists():
        path.mkdir(mode=0o700)
    info = path.lstat()
    if not path.is_dir() or path.is_symlink() or info.st_mode & 0o077 or info.st_uid != os.getuid():
        raise ValueError('unsafe_private_directory')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--private-dir', required=True, type=pathlib.Path)
    parser.add_argument('--mode', choices=('collect', 'renew', 'verify-session'), default='collect')
    args = parser.parse_args()
    private = args.private_dir.absolute()
    # Refuse symbolic ancestors before resolving or mounting private state.
    if os.getuid() == 0 or any(p.is_symlink() for p in [private, *private.parents]):
        raise ValueError('unsafe_private_directory')
    safe_directory(private)
    credentials, state, data, egress = [private / name for name in ('credentials', 'state', 'data', 'egress')]
    for path in (credentials, state, data, egress):
        safe_directory(path)
    if not (ROOT / 'node_modules' / 'playwright' / 'package.json').is_file():
        raise ValueError('dependencies_missing_run_npm_ci')
    descriptor = os.open(private / '.collection.lock', os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    lock = os.fdopen(descriptor, 'w')
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        raise ValueError('collection_already_running')
    name = 'bwb-lenovo-collector-' + uuid.uuid4().hex[:12]
    proxy = None
    container = None
    socket = egress / 'proxy.sock'
    proxy_ready = False
    try:
        # The short relative socket name avoids Linux's Unix-socket path limit.
        proxy = subprocess.Popen([sys.executable, '-u', str(ROOT / 'egress-proxy.py'), 'proxy.sock'],
                                 cwd=egress, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
        for _ in range(100):
            if proxy.poll() is not None:
                raise ValueError('egress_unavailable')
            if socket.exists():
                proxy_ready = True
                break
            time.sleep(.05)
        if not proxy_ready:
            raise ValueError('egress_unavailable')
        # Consume only the proxy's fixed ready line; never expose request data.
        proxy.stdout.readline()
        command = ['docker', 'run', '--rm', '--init', '--name', name,
                   '--network', 'none', '--user', f'{os.getuid()}:{os.getgid()}',
                   '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
                   '--security-opt', 'seccomp=' + str(ROOT / 'seccomp-profile.json'),
                   '--read-only', '--tmpfs', '/tmp:rw,nosuid,nodev,size=512m,mode=1777',
                   '--shm-size', '512m', '--pids-limit', '256', '--memory', '2g', '--cpus', '2',
                   '--env', 'XDG_CONFIG_HOME=/tmp/pw-config', '--env', 'XDG_CACHE_HOME=/tmp/pw-cache',
                   '--mount', f'type=bind,src={ROOT},dst=/runner,readonly',
                   '--mount', f'type=bind,src={credentials},dst=/credentials,readonly',
                   '--mount', f'type=bind,src={state},dst=/state',
                   '--mount', f'type=bind,src={data},dst=/data',
                   '--mount', f'type=bind,src={egress},dst=/egress,readonly',
                   '--workdir', '/runner', IMAGE, 'xvfb-run', '-a', 'node', 'src/cli.ts', args.mode]
        container = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
        try:
            stdout, _ = container.communicate(timeout=660)
        except subprocess.TimeoutExpired:
            raise ValueError('runner_deadline_exceeded')
        summaries = []
        for line in stdout.splitlines():
            try:
                value = json.loads(line)
                if isinstance(value, dict) and value.get('outcome') in ('success', 'failed'):
                    summaries.append(value)
            except ValueError:
                pass
        for value in summaries:
            print(json.dumps(value), flush=True)
        if not summaries:
            print(json.dumps({'outcome': 'failed', 'code': 'runner_unavailable'}), flush=True)
        return container.returncode or (0 if summaries else 1)
    finally:
        subprocess.run(['docker', 'rm', '-f', name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if container and container.poll() is None:
            container.terminate()
            try:
                container.wait(timeout=10)
            except subprocess.TimeoutExpired:
                container.kill()
        if proxy:
            proxy.terminate()
            try:
                proxy.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proxy.kill()
                proxy.wait()
        if proxy_ready:
            socket.unlink(missing_ok=True)
        lock.close()


def interrupted(_signum, _frame):
    raise KeyboardInterrupt()


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGHUP, interrupted)
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        print(json.dumps({'outcome': 'failed', 'code': 'collection_cancelled'}))
        raise SystemExit(130)
    except ValueError as error:
        allowed = {'unsafe_private_directory', 'dependencies_missing_run_npm_ci',
                   'collection_already_running', 'egress_unavailable', 'runner_deadline_exceeded'}
        code = str(error) if str(error) in allowed else 'runner_setup_error'
        print(json.dumps({'outcome': 'failed', 'code': code}))
        raise SystemExit(1)
    except Exception:
        # No provider, command, OS exception or private path in operational logs.
        print(json.dumps({'outcome': 'failed', 'code': 'runner_setup_error'}))
        raise SystemExit(1)
