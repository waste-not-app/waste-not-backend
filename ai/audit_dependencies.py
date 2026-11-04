#!/usr/bin/env python3
import re
import subprocess
import tempfile
from pathlib import Path


def main():
    root = Path(__file__).resolve().parent
    # Advisory identities omit the local +cpu build label; installation still requires the wheel hashes.
    requirements = [line.split(' \\')[0].replace('+cpu', '') for line in (root / 'requirements-cpu.lock').read_text().splitlines()
                    if re.match(r'[a-zA-Z0-9_-]+==', line)]
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / 'advisory-versions.txt'
        path.write_text('\n'.join(requirements) + '\n')
        subprocess.run(['pip-audit', '--disable-pip', '--no-deps', '-r', str(path)], check=True)


if __name__ == '__main__':
    main()
