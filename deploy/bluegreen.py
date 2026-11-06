#!/usr/bin/env python3
"""Deploy the verified image, preserving the old slot until routing is verified."""
import fcntl
import base64
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
PENDING = Path('/var/lib/wastenot-deploy/pending.json')
PREFIX = 'http://127.0.0.1/wastenot/api/health/ready'


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
    if not ready(PREFIX, None, attempts=3):
        raise ValueError('Existing upstream cannot be reconciled safely')


def pending(image=None):
    value = json.loads(PENDING.read_text()) if PENDING.exists() else None
    if value is not None and image is not None and value['image'] != image:
        raise ValueError('Pending deployment belongs to another image')
    return value


def drain(name, *, required=False):
    result = subprocess.run(['docker', 'stop', '--time', '45', name], capture_output=True, check=False, timeout=55)
    if required and result.returncode != 0:
        raise ValueError('Container could not be drained safely')


def rollback(image=None):
    value = pending(image)
    if value is None:
        return {'rolled_back': False}
    original = base64.b64decode(value['upstream'], validate=True)
    ports = re.findall(rb'server\s+127\.0\.0\.1:(1808[12])\s*;', original)
    if len(ports) != 1:
        raise ValueError('Pending upstream is invalid')
    # Confirmation may have stopped the old slot just before the process was interrupted.
    run('docker', 'start', value['previous'])
    if not ready(f'http://127.0.0.1:{int(ports[0])}/health/ready', None):
        raise ValueError('Previous slot cannot recover; preserve the running route and journal')
    write_upstream(original)
    # Keep both slots and the journal if reload fails; Nginx may still use the new one.
    reload_nginx()
    if not ready(PREFIX, None, attempts=3):
        raise ValueError('Restored upstream is not ready; preserve both slots for recovery')
    drain(value['target'], required=True)
    subprocess.run(['docker', 'rm', '-f', value['target']], capture_output=True, check=False, timeout=30)
    PENDING.unlink()
    return {'rolled_back': True}


def confirm(image):
    value = pending(image)
    if value is None:
        raise ValueError('No pending deployment to confirm')
    release = image.split(':', 1)[1]
    if not ready(f'http://127.0.0.1:{value["port"]}/health/ready', release) or not ready(PREFIX, release, attempts=3):
        raise ValueError('Pending deployment no longer serves its release')
    drain(value['previous'], required=True)
    PENDING.unlink()
    return {'release': release, 'ready': True, 'confirmed': True}


def deploy(image, env_file):
    if not re.fullmatch(r'wastenot-backend:[a-f0-9]{40}', image):
        raise ValueError('Use a full source commit image tag')
    release = image.split(':', 1)[1]
    values = settings(env_file)
    if pending() is not None:
        rollback()
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
        drain(name)
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
            PENDING.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            stage = PENDING.with_suffix('.pending')
            stage.write_text(json.dumps({'image': image, 'previous': current_name, 'target': name,
                                         'port': port, 'upstream': base64.b64encode(original).decode()}))
            stage.chmod(0o600)
            stage.replace(PENDING)
            switched = True
            write_upstream(re.sub(rb'(server\s+127\.0\.0\.1:)1808[12](\s*;)',
                                  lambda m: m[1] + str(port).encode() + m[2], original))
            reload_nginx()
            if not ready(PREFIX, release, attempts=3):
                raise ValueError('New release not reachable through Nginx')
        except Exception:
            if switched:
                rollback(image)
            else:
                subprocess.run(['docker', 'rm', '-f', name], capture_output=True, check=False, timeout=30)
            raise
    # The external runner can reach public HTTPS; retain the old slot until it confirms.
    return {'release': release, 'slot': target, 'ready': True, 'awaiting_public_verification': True}


def main():
    if os.geteuid() != 0 or not 2 <= len(sys.argv) <= 3:
        raise ValueError('Run as administrator with image tag and optional environment path')
    with open('/run/lock/wastenot-deploy.lock', 'w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if sys.argv[1] in ('--confirm', '--rollback') and len(sys.argv) == 3:
            result = confirm(sys.argv[2]) if sys.argv[1] == '--confirm' else rollback(sys.argv[2])
        else:
            result = deploy(sys.argv[1], Path(sys.argv[2] if len(sys.argv) == 3 else '/etc/wastenot/wastenot.env'))
        print(json.dumps(result))


if __name__ == '__main__':
    try:
        main()
    except Exception:
        # Docker/subprocess exceptions can include secret-bearing configuration; never echo them.
        print('Backend deployment failed; inspect the deployment state on the host.', file=sys.stderr)
        raise SystemExit(1)
