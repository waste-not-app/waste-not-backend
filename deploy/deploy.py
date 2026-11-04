#!/usr/bin/python3
import fcntl
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request
import zipfile
import stat

ROOT = Path('/opt/wastenot')
MAX_ARCHIVE = 80 * 1024 * 1024
NAMES = {'api', 'migrate', 'guides', 'ai-runtime.zip', 'ai-serve.py', 'release.json'}
BINARIES = {'api','migrate','guides'}
AI_LOCK = Path('/etc/wastenot/ai-requirements.sha256')


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
    if set(manifest) != {'commit', 'sha256', 'ai_requirements_sha256'} or not re.fullmatch('[a-f0-9]{40}', manifest['commit']):
        raise ValueError('Invalid source commit')
    if set(manifest['sha256']) != NAMES - {'release.json'}:
        raise ValueError('Invalid checksum manifest')
    if not re.fullmatch('[a-f0-9]{64}', manifest['ai_requirements_sha256']):
        raise ValueError('Invalid AI dependency digest')
    if AI_LOCK.is_file() and AI_LOCK.read_text().strip() != manifest['ai_requirements_sha256']:
        raise ValueError('AI dependencies differ from the installed environment')
    for name in NAMES - {'release.json'}:
        raw = files[name]
        if hashlib.sha256(raw).hexdigest() != manifest['sha256'][name]:
            raise ValueError('Release checksum mismatch')
        if name in BINARIES and (raw[:5] != b'\x7fELF\x02' or raw[18:20] != b'\x3e\x00'):
            raise ValueError('Expected Linux amd64 ELF binary')
    source_files(files['ai-runtime.zip'])
    return manifest['commit'], files


def source_files(raw):
    if len(raw)>2*1024*1024:
        raise ValueError('AI source archive is too large')
    output={}
    total=0
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        if len(archive.infolist())>128:
            raise ValueError('Too many AI source files')
        for entry in archive.infolist():
            path=PurePosixPath(entry.filename)
            mode=entry.external_attr>>16
            if (path.is_absolute() or path.as_posix()!=entry.filename or '\\' in entry.filename or '..' in path.parts or len(path.parts)<2 or path.parts[0]!='wastenot_ai'
                or entry.filename in output or entry.is_dir() or stat.S_ISLNK(mode)
                or entry.file_size>1024*1024 or entry.compress_size==0):
                raise ValueError('Invalid AI source member')
            total+=entry.file_size
            if total>4*1024*1024:
                raise ValueError('AI source expands beyond its limit')
            output[entry.filename]=archive.read(entry)
    if 'wastenot_ai/__init__.py' not in output:
        raise ValueError('AI source package is incomplete')
    return output


def environment(path=Path('/etc/wastenot/migrate.env')):
    result = {'PATH': '/usr/local/bin:/usr/sbin:/usr/bin:/bin', 'LANG': 'C.UTF-8'}
    for line in path.read_text().splitlines():
        if line and not line.startswith('#'):
            key, value = line.split('=', 1)
            result[key] = value
    return result


def activate(target):
    temporary = ROOT / '.current-new'
    temporary.unlink(missing_ok=True)
    temporary.symlink_to(target)
    temporary.replace(ROOT / 'current')
    if AI_LOCK.is_file():
        subprocess.run(['/usr/bin/systemctl','restart','wastenot-yolo.service','wastenot-decisions.service'],check=True,timeout=90)
    subprocess.run(['/usr/bin/systemctl', 'restart', 'wastenot-api.service'], check=True, timeout=55)


def ready():
    requests = [('http://127.0.0.1:18080/health/ready',{})]
    if Path('/etc/nginx/snippets/wastenot-api.conf').exists():
        requests.append(('http://127.0.0.1/wastenot/api/health/ready',{}))
    if AI_LOCK.is_file():
        for port,name,key in [(8091,'ai-detection.env','AI_SERVICE_TOKEN'),(8093,'ai-recommendation.env','AI_RECOMMENDATION_TOKEN')]:
            settings=environment(Path('/etc/wastenot')/name)
            requests.append((f'http://127.0.0.1:{port}/health/ready',{'Authorization':'Bearer '+settings[key]}))
    for _ in range(60):
        try:
            for url,headers in requests:
                request = urllib.request.Request(url, headers={'Host': 'pred-autoscaler.bccdev.id',**headers})
                with urllib.request.urlopen(request, timeout=2) as response:
                    value=json.load(response)
                    if response.status != 200 or not (value.get('ready') is True or value.get('data',{}).get('ready') is True):
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
            for name,raw in source_files(files['ai-runtime.zip']).items():
                path=stage/'python'/name
                path.parent.mkdir(parents=True,exist_ok=True)
                path.write_bytes(raw)
                path.chmod(0o444)
            for directory in (stage/'python').rglob('*'):
                if directory.is_dir():directory.chmod(0o755)
            (stage/'python').chmod(0o755)
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
