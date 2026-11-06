#!/usr/bin/env bash
set -euo pipefail
exec sudo -n /usr/bin/python3 "$(dirname -- "$0")/bluegreen.py" "$@"
