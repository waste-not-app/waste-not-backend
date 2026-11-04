#!/usr/bin/python3
import fcntl
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request

ROOT = Path('/opt/wastenot')
MAX_ARCHIVE = 80 * 1024 * 1024
NAMES = {'api', 'migrate', 'guides', 'release.json'}


def parse_release(payload):
    if len(payload) > MAX_ARCHIVE:
        raise ValueError('Release too large')
    files = {}
    with tarfile.open(fileobj=io.BytesIO(payload), mode='r:') as archive:
        for member in archive:
            if member.name not in NAMES or member.name in files or not member.isfile():
                raise ValueError('Unexpected release member')
            limit = 8192 if member.name == 'release.json' else 24 * 1024 * 1024
            if not 0 < member.size <= limit or member.pax_headers:
                raise ValueError('Invalid release member size or headers')
            files[member.name] = archive.extractfile(member).read(limit + 1)
            if len(files[member.name]) != member.size:
                raise ValueError('Truncated release')
    if set(files) != NAMES:
        raise ValueError('Incomplete release')
    manifest = json.loads(files['release.json'])
    if set(manifest) != {'commit', 'sha256'} or not re.fullmatch('[a-f0-9]{40}', manifest['commit']):
        raise ValueError('Invalid source commit')
    if set(manifest['sha256']) != NAMES - {'release.json'}:
        raise ValueError('Invalid checksum manifest')
    for name in NAMES - {'release.json'}:
        raw = files[name]
        if hashlib.sha256(raw).hexdigest() != manifest['sha256'][name]:
            raise ValueError('Release checksum mismatch')
        if raw[:5] != b'\x7fELF\x02' or raw[18:20] != b'\x3e\x00':
            raise ValueError('Expected Linux amd64 ELF binary')
    return manifest['commit'], files


def environment():
    result = {'PATH': '/usr/local/bin:/usr/sbin:/usr/bin:/bin', 'LANG': 'C.UTF-8'}
    for line in Path('/etc/wastenot/migrate.env').read_text().splitlines():
        if line and not line.startswith('#'):
            key, value = line.split('=', 1)
            result[key] = value
    return result


def activate(target):
    temporary = ROOT / '.current-new'
    temporary.unlink(missing_ok=True)
    temporary.symlink_to(target)
    temporary.replace(ROOT / 'current')
    subprocess.run(['/usr/bin/systemctl', 'restart', 'wastenot-api.service'], check=True, timeout=55)


def ready():
    urls = ['http://127.0.0.1:18080/health/ready']
    if Path('/etc/nginx/snippets/wastenot-api.conf').exists():
        urls.append('http://127.0.0.1/wastenot/api/health/ready')
    for _ in range(60):
        try:
            for url in urls:
                request = urllib.request.Request(url, headers={'Host': 'pred-autoscaler.bccdev.id'})
                with urllib.request.urlopen(request, timeout=2) as response:
                    if response.status != 200 or json.load(response).get('ready') is not True:
                        raise ValueError('Readiness failed')
            return True
        except (OSError, ValueError):
            time.sleep(0.5)
    return False


def deploy(payload):
    commit, files = parse_release(payload)
    releases = ROOT / 'releases'
    target = releases / commit
    previous = (ROOT / 'current').resolve() if (ROOT / 'current').is_symlink() else None
    if previous is not None and (previous.parent != releases or not re.fullmatch('[a-f0-9]{40}', previous.name)):
        raise ValueError('Invalid current release link')
    if target.exists():
        if any((target / name).read_bytes() != raw for name, raw in files.items()):
            raise ValueError('Commit already has a different release')
    else:
        stage = Path(tempfile.mkdtemp(prefix='.stage-', dir=releases))
        try:
            for name, raw in files.items():
                (stage / name).write_bytes(raw)
                (stage / name).chmod(0o444 if name == 'release.json' else 0o555)
            stage.chmod(0o755)
            stage.replace(target)
        finally:
            if stage.exists():
                shutil.rmtree(stage)
    migrate = subprocess.run(['/usr/sbin/runuser', '-u', 'wastenot-migrate', '--', str(target / 'migrate')],
                             env=environment(), capture_output=True, timeout=45)
    if migrate.returncode:
        raise ValueError('Migration failed; current API was preserved')
    try:
        activate(target)
        if not ready():
            raise ValueError('New release did not become ready')
    except Exception:
        if previous is not None:
            activate(previous)
        raise ValueError('Deployment failed; previous API release restored when available') from None
    if previous is not None and previous != target:
        backup = ROOT / '.previous-new'
        backup.unlink(missing_ok=True)
        backup.symlink_to(previous)
        backup.replace(ROOT / 'previous')
    protected = {target, previous}
    old = sorted((p for p in releases.iterdir() if p.is_dir() and re.fullmatch('[a-f0-9]{40}', p.name)),
                 key=lambda p: p.stat().st_mtime, reverse=True)
    for directory in old[3:]:
        if directory not in protected:
            shutil.rmtree(directory)
    return {'commit': commit, 'ready': True}


def main():
    if os.geteuid() != 0 or len(sys.argv) != 1:
        raise SystemExit('This helper requires the fixed deployment command')
    with open('/run/lock/wastenot-deploy.lock', 'w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        payload = sys.stdin.buffer.read(MAX_ARCHIVE + 1)
        print(json.dumps(deploy(payload)))


if __name__ == '__main__':
    try:
        main()
    except Exception:
        print('WasteNot deployment failed; inspect the dedicated service and release directory.', file=sys.stderr)
        raise SystemExit(1)
