#!/usr/bin/env python3
import hashlib
import io
import json
from pathlib import Path
import re
import subprocess
import tarfile
import zipfile


def main():
    if subprocess.check_output(['git','status','--porcelain']):
        raise SystemExit('Commit the reviewed source before packaging a release')
    commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    if not re.fullmatch('[a-f0-9]{40}', commit):
        raise SystemExit('Invalid source commit')
    files = {name: (Path('bin') / name).read_bytes() for name in ('api', 'migrate', 'guides')}
    source = io.BytesIO()
    with zipfile.ZipFile(source, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
        for path in sorted(Path('ai/runtime/src/wastenot_ai').rglob('*')):
            if path.is_file() and '__pycache__' not in path.parts and path.suffix != '.pyc':
                entry = zipfile.ZipInfo(path.relative_to('ai/runtime/src').as_posix(), date_time=(2026, 1, 1, 0, 0, 0))
                entry.compress_type = zipfile.ZIP_DEFLATED
                archive.writestr(entry, path.read_bytes())
    files['ai-runtime.zip'] = source.getvalue()
    files['ai-serve.py'] = Path('ai/serve.py').read_bytes()
    manifest = {'commit': commit, 'ai_requirements_sha256': hashlib.sha256(Path('ai/requirements-cpu.lock').read_bytes()).hexdigest(),
                'sha256': {name: hashlib.sha256(raw).hexdigest() for name, raw in files.items()}}
    files['release.json'] = json.dumps(manifest, sort_keys=True).encode()
    with tarfile.open('bin/release.tar', 'w', format=tarfile.USTAR_FORMAT) as archive:
        for name, raw in files.items():
            info = tarfile.TarInfo(name)
            info.size = len(raw)
            info.mode = 0o555 if name != 'release.json' else 0o444
            archive.addfile(info, io.BytesIO(raw))
    print('Packaged source commit', commit)


if __name__ == '__main__':
    main()
