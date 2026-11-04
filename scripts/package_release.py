#!/usr/bin/env python3
import hashlib
import io
import json
from pathlib import Path
import re
import subprocess
import tarfile


def main():
    commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    if not re.fullmatch('[a-f0-9]{40}', commit):
        raise SystemExit('Invalid source commit')
    files = {name: (Path('bin') / name).read_bytes() for name in ('api', 'migrate', 'guides')}
    manifest = {'commit': commit, 'sha256': {name: hashlib.sha256(raw).hexdigest() for name, raw in files.items()}}
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
