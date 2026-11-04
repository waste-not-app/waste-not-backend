#!/usr/bin/env python3
import hashlib
import json
from pathlib import Path


def main():
    root = Path(__file__).resolve().parent
    manifest = json.loads((root / 'upstream.json').read_text())
    hashes = dict(manifest['files'])
    for patch in manifest['patches']:
        hashes[patch['file']] = patch['sha256']
    actual = {p.relative_to(root / 'runtime/src').as_posix(): p for p in (root / 'runtime/src/wastenot_ai').rglob('*')
              if p.is_file() and '__pycache__' not in p.parts and p.suffix != '.pyc'}
    if set(actual) != set(hashes):
        raise SystemExit('Unexpected or missing upstream source file')
    for name, path in actual.items():
        if hashlib.sha256(path.read_bytes()).hexdigest() != hashes[name]:
            raise SystemExit('Unexpected Python policy/source change: ' + name)
    print('Verified Python upstream snapshot and declared runtime patch:', len(actual), 'files')


if __name__ == '__main__':
    main()
