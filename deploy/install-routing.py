#!/usr/bin/python3
import datetime
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import time
import urllib.request


SOURCE = Path('/etc/nginx/sites-available/default')
SNIPPET = Path('/etc/nginx/snippets/wastenot-api.conf')
INCLUDE = '\tinclude /etc/nginx/snippets/wastenot-api.conf;\n'


def fetch(path):
    request = urllib.request.Request('http://127.0.0.1' + path, headers={'Host': 'mlops.bccdev.id'})
    with urllib.request.urlopen(request, timeout=5) as response:
        return response.read()


def install(snippet_source):
    with urllib.request.urlopen('http://127.0.0.1:18080/health/ready', timeout=5) as response:
        if json.load(response).get('ready') is not True:
            raise ValueError('API must be ready before installing a public path')
    before = SOURCE.read_bytes()
    before_root = fetch('/')
    if INCLUDE.encode() in before:
        if SNIPPET.read_bytes() != snippet_source.read_bytes():
            raise ValueError('Existing WasteNot snippet differs; inspect it before editing')
        if json.loads(fetch('/wastenot/api/health/ready')).get('ready') is not True:
            raise ValueError('Existing WasteNot route is not ready')
        print('Existing WasteNot routing verified')
        return
    if before.count(b'server_name _;') != 1 or SNIPPET.exists():
        raise ValueError('Nginx source differs from inspected baseline; inspect before editing')
    backup = Path('/var/backups/wastenot') / ('nginx-default.' + datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '.before')
    shutil.copy2(SOURCE, backup)
    backup.chmod(0o600)
    after = before.replace(b'server_name _;\n', b'server_name _;\n' + INCLUDE.encode(), 1)
    try:
        SNIPPET.write_bytes(snippet_source.read_bytes())
        SNIPPET.chmod(0o644)
        SOURCE.write_bytes(after)
        subprocess.run(['/usr/sbin/nginx', '-t'], check=True)
        subprocess.run(['/usr/bin/systemctl', 'reload', 'nginx'], check=True)
        if fetch('/') != before_root:
            raise ValueError('Existing root response changed')
        for attempt in range(30):
            try:
                if json.loads(fetch('/wastenot/api/health/ready')).get('ready') is True:
                    break
            except (OSError, ValueError):
                pass
            time.sleep(0.1)
        else:
            raise ValueError('Prefixed API route failed')
    except Exception:
        SOURCE.write_bytes(before)
        SNIPPET.unlink(missing_ok=True)
        subprocess.run(['/usr/sbin/nginx', '-t'], check=True)
        subprocess.run(['/usr/bin/systemctl', 'reload', 'nginx'], check=True)
        raise
    report = {'nginx_backup': str(backup), 'before_sha256': hashlib.sha256(before).hexdigest(),
              'after_sha256': hashlib.sha256(after).hexdigest(),
              'root_response_preserved': True, 'prefixed_health_ready': True}
    Path('/var/backups/wastenot/routing-verification.json').write_text(json.dumps(report, indent=2))
    print(json.dumps(report))


if __name__ == '__main__':
    import sys
    if len(sys.argv) != 2:
        raise SystemExit('usage: sudo install-routing.py nginx-location.conf')
    install(Path(sys.argv[1]))
