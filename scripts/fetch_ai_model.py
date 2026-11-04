#!/usr/bin/env python3
import hashlib
import json
from pathlib import Path
import sys
import urllib.request


def main():
    if len(sys.argv) != 2:
        raise SystemExit('usage: fetch_ai_model.py destination-directory')
    manifest = json.loads((Path(__file__).resolve().parents[1] / 'ai/model.json').read_text())
    target = Path(sys.argv[1]) / manifest['filename']
    target.parent.mkdir(parents=True, exist_ok=True)
    if not target.exists():
        temporary = target.with_suffix('.part')
        try:
            with urllib.request.urlopen(manifest['url'], timeout=60) as response:
                raw = response.read(32 * 1024 * 1024 + 1)
            if len(raw) > 32 * 1024 * 1024 or hashlib.sha256(raw).hexdigest() != manifest['sha256']:
                raise ValueError('Model size or checksum invalid')
            temporary.write_bytes(raw)
            temporary.replace(target)
        finally:
            temporary.unlink(missing_ok=True)
    if hashlib.sha256(target.read_bytes()).hexdigest() != manifest['sha256']:
        raise ValueError('Existing model checksum differs; preserve it for inspection')
    print('Verified model', manifest['name'], manifest['sha256'])


if __name__ == '__main__':
    main()
