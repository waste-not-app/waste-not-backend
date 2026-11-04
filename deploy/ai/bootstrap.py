#!/usr/bin/python3
import datetime
import hashlib
import json
import os
from pathlib import Path
import pwd
import secrets
import shutil
import socket
import subprocess
import sys
import time
import urllib.request

CONFIG = Path('/etc/wastenot')
CURRENT = Path('/opt/wastenot/current')
AI_ROOT = Path('/opt/wastenot-ai')


def private_write(path, text):
    temporary=path.with_suffix('.new')
    temporary.write_text(text)
    temporary.chmod(0o600)
    temporary.replace(path)


def run(*args):
    return subprocess.run(args,check=True,capture_output=True)


def health(port,token):
    request=urllib.request.Request(f'http://127.0.0.1:{port}/health/ready',headers={'Authorization':'Bearer '+token})
    with urllib.request.urlopen(request,timeout=3) as response:
        value=json.load(response)
        return response.status==200 and (value.get('ready') is True or value.get('data',{}).get('ready') is True)


def main(stage):
    if os.geteuid()!=0:
        raise ValueError('Bootstrap requires the authorized administrator session')
    marker=CONFIG/'ai-requirements.sha256'
    if marker.exists():
        raise ValueError('AI is already installed; inspect before updating its environment')
    if not (CURRENT/'ai-serve.py').is_file() or not (CURRENT/'python/wastenot_ai/worker.py').is_file():
        raise ValueError('Deploy the verified Go/Python release before starting AI workers')
    manifest=json.loads((CURRENT/'release.json').read_text())
    digest=hashlib.sha256((stage/'requirements-cpu.lock').read_bytes()).hexdigest()
    if manifest['ai_requirements_sha256']!=digest:
        raise ValueError('Installed dependency plan differs from the release')
    for port in (8091,8093):
        with socket.socket() as probe:
            probe.bind(('127.0.0.1',port))
    model=json.loads((stage/'model.json').read_text())
    raw=(stage/model['filename']).read_bytes()
    if hashlib.sha256(raw).hexdigest()!=model['sha256']:
        raise ValueError('Invalid model checksum')
    result=run(str(AI_ROOT/'tools/uv'),'pip','check','--python',str(AI_ROOT/'venv/bin/python'))
    if b'0 packages' in result.stderr:
        raise ValueError('The CPU environment has no installed packages')
    for user in ('wastenot-yolo','wastenot-decisions'):
        try:pwd.getpwnam(user)
        except KeyError:run('/usr/sbin/useradd','--system','--user-group','--no-create-home','--shell','/usr/sbin/nologin',user)
    (AI_ROOT/'models').mkdir(mode=0o755,exist_ok=True)
    target=AI_ROOT/'models'/model['filename']
    if target.exists() and hashlib.sha256(target.read_bytes()).hexdigest()!=model['sha256']:
        raise ValueError('Existing model differs; preserve it for inspection')
    target.write_bytes(raw);target.chmod(0o444)
    detection_token=secrets.token_urlsafe(32)
    recommendation_token=secrets.token_urlsafe(32)
    for name in ('ai-detection.env','ai-recommendation.env'):
        if (CONFIG/name).exists():raise ValueError('Existing AI environment file must be inspected')
    private_write(CONFIG/'ai-detection.env','AI_SERVICE_TOKEN='+detection_token+'\nAI_MODEL_PATH='+str(target)+'\nAI_MODEL_SHA256='+model['sha256']+'\nAI_DEVICE=cpu\n')
    private_write(CONFIG/'ai-recommendation.env','AI_RECOMMENDATION_TOKEN='+recommendation_token+'\nGEMINI_API_KEY=\nGEMINI_MODEL=\nGEMINI_TIMEOUT_SECONDS=21\nAI_GUIDES_PATH=\n')
    before=(CONFIG/'api.env').read_bytes()
    backup=Path('/var/backups/wastenot')/('api.env.before-ai.'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ'))
    backup.write_bytes(before);backup.chmod(0o600)
    for name in ('wastenot-yolo.service','wastenot-decisions.service'):
        shutil.copyfile(stage/name,Path('/etc/systemd/system')/name)
        (Path('/etc/systemd/system')/name).chmod(0o644)
    run('/usr/bin/systemctl','daemon-reload')
    try:
        run('/usr/bin/systemctl','enable','--now','wastenot-yolo.service','wastenot-decisions.service')
        for attempt in range(60):
            try:
                if health(8091,detection_token) and health(8093,recommendation_token):break
            except (OSError,ValueError):pass
            time.sleep(1)
        else:raise ValueError('AI workers did not become ready')
        settings=dict(line.split('=',1) for line in before.decode().splitlines() if line and not line.startswith('#'))
        settings.update(AI_ENABLED='true',AI_RECOMMENDATION_ENABLED='false',AI_ALLOW_PROTOTYPE='false',
                        AI_DETECTION_URL='http://127.0.0.1:8091',AI_RECOMMENDATION_URL='http://127.0.0.1:8093',
                        AI_SERVICE_TOKEN=detection_token,AI_RECOMMENDATION_TOKEN=recommendation_token)
        private_write(CONFIG/'api.env',''.join(key+'='+value+'\n' for key,value in settings.items()))
        private_write(marker,digest+'\n')
        run('/usr/bin/systemctl','restart','wastenot-api.service')
        for attempt in range(30):
            try:
                with urllib.request.urlopen('http://127.0.0.1:18080/health/ready',timeout=2) as response:
                    if json.load(response).get('ready') is True:break
            except (OSError,ValueError):pass
            time.sleep(.5)
        else:raise ValueError('Core API failed after enabling scanning')
    except Exception:
        private_write(CONFIG/'api.env',before.decode())
        marker.unlink(missing_ok=True)
        subprocess.run(['/usr/bin/systemctl','restart','wastenot-api.service'],check=False)
        subprocess.run(['/usr/bin/systemctl','disable','--now','wastenot-yolo.service','wastenot-decisions.service'],check=False,capture_output=True)
        raise
    print(json.dumps({'ai_scan':True,'ai_recommendation':False,'model':model['name'],'model_sha256':model['sha256'],
                      'dependency_sha256':digest,'api_environment_backup':str(backup)}))


if __name__=='__main__':
    if len(sys.argv)!=2:raise SystemExit('usage: sudo bootstrap.py staging-directory')
    main(Path(sys.argv[1]).resolve())
