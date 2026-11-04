#!/usr/bin/python3
"""One-time bootstrap on the inspected cp-bcc VM; no global service replacement."""
import base64
import datetime
import json
import os
from pathlib import Path
import pwd
import secrets
import shutil
import subprocess
import sys

ROOT = Path('/opt/wastenot')
CONFIG = Path('/etc/wastenot')


def run(*args, input=None):
    return subprocess.run(args, input=input, check=True, capture_output=True).stdout


def save_private(path, text):
    path.write_text(text)
    path.chmod(0o600)


def kube(*args, input=None):
    return run('/usr/local/bin/kubectl', *args, input=input)


def main():
    if os.geteuid() != 0 or len(sys.argv) != 2:
        raise SystemExit('usage: sudo python3 bootstrap.py staging-directory')
    stage = Path(sys.argv[1]).resolve()
    if CONFIG.exists() or ROOT.exists():
        raise SystemExit('WasteNot paths already exist; inspect instead of overwriting bootstrap')
    if b'wastenot' in kube('get', 'namespaces', '-o', 'name'):
        raise SystemExit('WasteNot namespace already exists; inspect before bootstrapping')
    for name in ('wastenot', 'wastenot-migrate', 'wastenot-ci'):
        try:
            pwd.getpwnam(name)
        except KeyError:
            pass
        else:
            raise SystemExit('WasteNot account already exists; inspect before bootstrapping')
    run('/usr/sbin/useradd', '--system', '--user-group', '--no-create-home', '--shell', '/usr/sbin/nologin', 'wastenot')
    run('/usr/sbin/useradd', '--system', '--user-group', '--no-create-home', '--shell', '/usr/sbin/nologin', 'wastenot-migrate')
    run('/usr/sbin/useradd', '--system', '--user-group', '--create-home', '--home-dir', '/home/wastenot-ci', '--shell', '/bin/sh', 'wastenot-ci')
    ROOT.mkdir(mode=0o755)
    (ROOT / 'releases').mkdir(mode=0o755)
    CONFIG.mkdir(mode=0o700)
    backups = Path('/var/backups/wastenot')
    backups.mkdir(mode=0o700, exist_ok=True)
    run('/usr/local/bin/kubectl', 'create', 'namespace', 'wastenot')
    owner_password = secrets.token_urlsafe(32)
    app_password = secrets.token_urlsafe(32)
    jwt_secret = base64.b64encode(secrets.token_bytes(32)).decode()
    secret = {'apiVersion': 'v1', 'kind': 'Secret', 'metadata': {'name': 'postgres-credentials', 'namespace': 'wastenot'},
              'type': 'Opaque', 'stringData': {'password': owner_password}}
    secret_path = CONFIG / 'postgres-secret.json'
    save_private(secret_path, json.dumps(secret))
    kube('apply', '-f', str(secret_path))
    manifest = (stage / 'postgres.yaml').read_text()
    kube('apply', '-f', '-', input=manifest.encode())
    kube('-n', 'wastenot', 'rollout', 'status', 'statefulset/postgres', '--timeout=180s')
    sql = ("CREATE ROLE wastenot_app LOGIN PASSWORD '" + app_password + "';\n"
           "GRANT CONNECT ON DATABASE wastenot TO wastenot_app;\n"
           "GRANT USAGE ON SCHEMA public TO wastenot_app;\n"
           "ALTER DEFAULT PRIVILEGES FOR ROLE wastenot_owner IN SCHEMA public GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO wastenot_app;\n")
    kube('-n', 'wastenot', 'exec', '-i', 'postgres-0', '--', 'psql', '-U', 'wastenot_owner', '-d', 'wastenot', '-v', 'ON_ERROR_STOP=1', input=sql.encode())
    ip = kube('-n', 'wastenot', 'get', 'service', 'postgres', '-o', 'jsonpath={.spec.clusterIP}').decode()
    common = 'HTTP_ADDR=127.0.0.1:18080\nAI_ENABLED=false\nAI_ALLOW_PROTOTYPE=false\nGOMEMLIMIT=384MiB\n'
    save_private(CONFIG / 'api.env', common + 'JWT_SECRET_BASE64=' + jwt_secret + '\nDATABASE_URL=postgres://wastenot_app:' + app_password + '@' + ip + ':5432/wastenot?sslmode=disable\n')
    save_private(CONFIG / 'migrate.env', 'DATABASE_URL=postgres://wastenot_owner:' + owner_password + '@' + ip + ':5432/wastenot?sslmode=disable\n')
    shutil.copyfile(stage / 'deploy.py', '/usr/local/sbin/wastenot-deploy')
    Path('/usr/local/sbin/wastenot-deploy').chmod(0o755)
    shutil.copyfile(stage / 'wastenot-api.service', '/etc/systemd/system/wastenot-api.service')
    Path('/etc/systemd/system/wastenot-api.service').chmod(0o644)
    ci_home = Path('/home/wastenot-ci')
    os.chown(ci_home, 0, 0)
    ci_home.chmod(0o755)
    ssh = ci_home / '.ssh'
    ssh.mkdir(mode=0o755)
    public_key = (stage / 'deploy-key.pub').read_text().strip()
    if not public_key.startswith('ssh-ed25519 ') or '\n' in public_key:
        raise SystemExit('Invalid deploy public key')
    save_private(ssh / 'authorized_keys', 'restrict,command="/usr/bin/sudo -n /usr/local/sbin/wastenot-deploy" ' + public_key + '\n')
    (ssh / 'authorized_keys').chmod(0o444)
    sudoers = Path('/etc/sudoers.d/wastenot-deploy')
    save_private(sudoers, 'wastenot-ci ALL=(root) NOPASSWD: /usr/local/sbin/wastenot-deploy ""\n')
    sudoers.chmod(0o440)
    run('/usr/sbin/visudo', '-cf', str(sudoers))
    run('/usr/bin/systemctl', 'daemon-reload')
    run('/usr/bin/systemctl', 'enable', 'wastenot-api.service')
    print('WasteNot bootstrap complete; API awaits its first release. Existing Nginx routes were preserved.')


if __name__ == '__main__':
    main()
