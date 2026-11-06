#!/usr/bin/env python3
"""Check the reviewed contract bundle, optionally against a peer checkout."""
import argparse
import hashlib
import json
from pathlib import Path


def check(directory, pin, peer=None):
    expected = json.loads(pin.read_text())['sha256']
    actual = {path.relative_to(directory).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
              for path in directory.rglob('*.json')}
    if actual != expected:
        raise ValueError('Contract bundle differs from the reviewed integration pin')
    if peer is not None:
        other = {path.relative_to(peer).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
                 for path in peer.rglob('*.json')}
        if other != expected:
            raise ValueError('Peer contract bundle differs; update both repositories together')
    print(f'Checked {len(expected)} pinned contracts' + (' and peer checkout.' if peer else '.'))


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('directory', type=Path)
    parser.add_argument('pin', type=Path)
    parser.add_argument('--peer', type=Path)
    args = parser.parse_args()
    check(args.directory, args.pin, args.peer)
