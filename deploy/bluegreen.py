#!/usr/bin/env python3
"""Deploy the verified image, preserving the old slot until routing is verified."""
import fcntl
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile
import time
import urllib.request

UPSTREAM = Path('/etc/nginx/conf.d/wastenot-upstream.conf')
PUBLIC = 'https://pred-autoscaler.bccdev.id/wastenot/api/health/ready'


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=90).stdout.strip()


def settings(path):
    info = path.stat()
    if info.st_uid != 0 or info.st_mode & 0o077:
        raise ValueError('Deployment environment must be root-owned and mode 0600')
    values = {}
    for line in path.read_text().splitlines():
        if not line.strip() or line.lstrip().startswith('#'):
            continue
        key, value = line.split('=', 1)
        parsed = shlex.split(value, comments=False)
        if not re.fullmatch('[A-Z][A-Z0-9_]*', key) or len(parsed) > 1:
            raise ValueError('Invalid deployment environment entry')
        values[key] = parsed[0] if parsed else ''
    for key in ('DATABASE_URL_APP', 'DATABASE_URL_MIGRATE', 'JWT_SECRET_BASE64'):
        if not values.get(key):
            raise ValueError('Incomplete deployment environment')
    return values


def ready(url, release, *, attempts=30):
    for _ in range(attempts):
        try:
            request = urllib.request.Request(url, headers={'Host': 'pred-autoscaler.bccdev.id'})
            with urllib.request.urlopen(request, timeout=3) as response:
                if (response.status == 200 and json.load(response).get('ready') is True
                        and (release is None or response.headers.get('X-WasteNot-Release') == release)):
                    return True
        except (OSError, ValueError):
            pass
        time.sleep(1)
    return False


def write_upstream(raw):
    temporary = UPSTREAM.with_suffix('.pending')
    temporary.write_bytes(raw)
    temporary.chmod(0o644)
    temporary.replace(UPSTREAM)


def reload_nginx():
    run('nginx', '-t')
    run('systemctl', 'reload', 'nginx')


def reconcile(active_port):
    # A killed deploy can leave the on-disk upstream ahead of the running Nginx workers.
    if not ready(f'http://127.0.0.1:{active_port}/health/ready', None, attempts=3):
        raise ValueError('Persisted active slot is not ready; inspect routing before deployment')
    reload_nginx()
    if not ready('http://127.0.0.1/wastenot/api/health/ready', None, attempts=3):
        raise ValueError('Existing upstream cannot be reconciled safely')


def deploy(image, env_file):
    if not re.fullmatch(r'wastenot-backend:[a-f0-9]{40}', image):
        raise ValueError('Use a full source commit image tag')
    release = image.split(':', 1)[1]
    values = settings(env_file)
    original = UPSTREAM.read_bytes()
    ports = re.findall(rb'server\s+127\.0\.0\.1:(1808[12])\s*;', original)
    if len(ports) != 1:
        raise ValueError('Expected exactly one existing blue-green upstream')
    active_port = int(ports[0])
    current, target, port = ('blue', 'green', 18082) if active_port == 18081 else ('green', 'blue', 18081)
    name = 'wastenot-api-' + target
    current_name = 'wastenot-api-' + current
    image_id = run('docker', 'image', 'inspect', '--format', '{{.Id}}', image)
    # Environment files keep credentials out of process arguments and deploy logs.
    with tempfile.TemporaryDirectory(prefix='wastenot-env-') as directory:
        migrate_env = Path(directory) / 'migrate.env'
        migrate_env.write_text('DATABASE_URL=' + values['DATABASE_URL_MIGRATE'] + '\n')
        migrate_env.chmod(0o600)
        run('docker', 'run', '--rm', '--network', 'host', '--read-only', '--cap-drop', 'ALL',
            '--security-opt', 'no-new-privileges', '--env-file', str(migrate_env),
            '--entrypoint', '/migrate', image)
        runtime = {key: value for key, value in values.items() if key.startswith('AI_')}
        runtime.update(DATABASE_URL=values['DATABASE_URL_APP'], JWT_SECRET_BASE64=values['JWT_SECRET_BASE64'],
                       HTTP_ADDR=f'127.0.0.1:{port}', WASTENOT_RELEASE=release)
        runtime.setdefault('AI_ENABLED', 'false')
        runtime.setdefault('AI_ALLOW_PROTOTYPE', 'false')
        runtime.setdefault('AI_RECOMMENDATION_ENABLED', 'false')
        runtime_env = Path(directory) / 'runtime.env'
        runtime_env.write_text(''.join(f'{key}={value}\n' for key, value in runtime.items()))
        runtime_env.chmod(0o600)
        reconcile(active_port)
        # After reconciliation an interrupted deployment may still have requests in the other slot.
        subprocess.run(['docker', 'stop', '--time', '45', name], capture_output=True, check=False, timeout=55)
        subprocess.run(['docker', 'rm', '-f', name], capture_output=True, check=False, timeout=30)
        switched = False
        try:
            run('docker', 'run', '-d', '--name', name, '--network', 'host', '--restart', 'unless-stopped',
                '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
                '--memory', '768m', '--cpus', '1', '--pids-limit', '256', '--ulimit', 'core=0',
                '--log-opt', 'max-size=10m', '--log-opt', 'max-file=3',
                '--env-file', str(runtime_env), image)
            if run('docker', 'inspect', '--format', '{{.Image}}', name) != image_id:
                raise ValueError('Target image identity differs')
            if not ready(f'http://127.0.0.1:{port}/health/ready', release):
                raise ValueError('Target slot not ready')
            switched = True
            write_upstream(re.sub(rb'(server\s+127\.0\.0\.1:)1808[12](\s*;)',
                                  lambda m: m[1] + str(port).encode() + m[2], original))
            reload_nginx()
            for url in ('http://127.0.0.1/wastenot/api/health/ready', PUBLIC):
                if not ready(url, release, attempts=3):
                    raise ValueError('New release not reachable through gateway')
        except Exception:
            if switched:
                write_upstream(original)
                # Do not remove the new slot if the rollback reload fails: Nginx may still use it.
                reload_nginx()
            subprocess.run(['docker', 'rm', '-f', name], capture_output=True, check=False, timeout=30)
            raise
    # Keep the drained container for rollback. It is removed only when its slot is reused.
    subprocess.run(['docker', 'stop', '--time', '45', current_name],
                   capture_output=True, check=False, timeout=55)
    return {'release': release, 'slot': target, 'ready': True}


def main():
    if os.geteuid() != 0 or not 2 <= len(sys.argv) <= 3:
        raise ValueError('Run as administrator with image tag and optional environment path')
    with open('/run/lock/wastenot-deploy.lock', 'w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        print(json.dumps(deploy(sys.argv[1], Path(sys.argv[2] if len(sys.argv) == 3
                                                else '/etc/wastenot/wastenot.env'))))


if __name__ == '__main__':
    try:
        main()
    except Exception:
        # Docker/subprocess exceptions can include secret-bearing configuration; never echo them.
        print('Backend deployment failed; inspect the deployment state on the host.', file=sys.stderr)
        raise SystemExit(1)
