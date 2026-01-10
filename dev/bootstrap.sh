#!/usr/bin/env bash
set -euo pipefail

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTRYPOINT_SH="$SCRIPT_DIR/.agent-layer/src/lib/entrypoint.sh"
if [[ ! -f "$ENTRYPOINT_SH" ]]; then
  ENTRYPOINT_SH="$SCRIPT_DIR/src/lib/entrypoint.sh"
fi
if [[ ! -f "$ENTRYPOINT_SH" ]]; then
  ENTRYPOINT_SH="$SCRIPT_DIR/../src/lib/entrypoint.sh"
fi
if [[ ! -f "$ENTRYPOINT_SH" ]]; then
  die "Missing src/lib/entrypoint.sh (expected near .agent-layer/)."
fi
# shellcheck disable=SC1090
source "$ENTRYPOINT_SH"
resolve_entrypoint_root || exit $?

has_cmd() { command -v "$1" > /dev/null 2>&1; }

missing=()
has_cmd git || missing+=("git")
has_cmd node || missing+=("node")
has_cmd npm || missing+=("npm")
has_cmd bats || missing+=("bats")
has_cmd shfmt || missing+=("shfmt")
has_cmd shellcheck || missing+=("shellcheck")

prettier_installed="0"
if [[ -x "$AGENTLAYER_ROOT/node_modules/.bin/prettier" ]]; then
  prettier_installed="1"
fi

say "Dev bootstrap will ensure these dependencies are installed:"
say "  - git"
say "  - node + npm"
say "  - bats"
say "  - shfmt"
say "  - shellcheck"
say "  - npm install (Prettier in .agent-layer)"
say ""

if [[ "${#missing[@]}" -eq 0 && "$prettier_installed" == "1" ]]; then
  say "All dependencies are already installed."
else
  say "Missing:"
  if [[ "${#missing[@]}" -gt 0 ]]; then
    for dep in "${missing[@]}"; do
      say "  - $dep"
    done
  fi
  if [[ "$prettier_installed" == "0" ]]; then
    say "  - npm install (Prettier in .agent-layer)"
  fi
  say ""
fi

if [[ ! -t 0 ]]; then
  die "No TTY available to confirm bootstrap."
fi

say "This will:"
say "  - install missing system dependencies (if any)"
say "  - run npm install in .agent-layer (if needed)"
say "  - enable git hooks for this repo (dev-only)"
say "  - run setup (sync + MCP deps; no checks)"
read -r -p "Continue? [y/N] " reply
case "$reply" in
  y | Y | yes | YES) ;;
  *)
    die "Aborted."
    ;;
esac

pkg_manager=""
if has_cmd brew; then
  pkg_manager="brew"
elif has_cmd apt-get; then
  pkg_manager="apt-get"
fi

packages=()
missing_joined=" ${missing[*]-} "
if [[ "$missing_joined" == *" git "* ]]; then
  packages+=("git")
fi
if [[ "$missing_joined" == *" node "* || "$missing_joined" == *" npm "* ]]; then
  if [[ "$pkg_manager" == "brew" ]]; then
    packages+=("node")
  elif [[ "$pkg_manager" == "apt-get" ]]; then
    packages+=("nodejs" "npm")
  fi
fi
if [[ "$missing_joined" == *" bats "* ]]; then
  if [[ "$pkg_manager" == "brew" ]]; then
    packages+=("bats-core")
  elif [[ "$pkg_manager" == "apt-get" ]]; then
    packages+=("bats")
  fi
fi
if [[ "$missing_joined" == *" shfmt "* ]]; then
  packages+=("shfmt")
fi
if [[ "$missing_joined" == *" shellcheck "* ]]; then
  packages+=("shellcheck")
fi

if [[ "${#packages[@]}" -gt 0 ]]; then
  if [[ -z "$pkg_manager" ]]; then
    die "No supported package manager found (brew or apt-get). Install manually."
  fi
  if [[ "$pkg_manager" == "brew" ]]; then
    say "==> Installing system packages via brew: ${packages[*]}"
    brew install "${packages[@]}"
  else
    say "==> Installing system packages via apt-get: ${packages[*]}"
    sudo apt-get update
    sudo apt-get install -y "${packages[@]}"
  fi
fi

if [[ "$prettier_installed" == "0" ]]; then
  say "==> Installing node dev dependencies (Prettier)"
  (cd "$AGENTLAYER_ROOT" && npm install)
fi

say "==> Running setup (no checks)"
bash "$AGENTLAYER_ROOT/setup.sh" --skip-checks

if git rev-parse --is-inside-work-tree > /dev/null 2>&1; then
  say "==> Enabling git hooks (core.hooksPath=.agent-layer/.githooks)"
  git config core.hooksPath .agent-layer/.githooks

  if [[ -f "$AGENTLAYER_ROOT/.githooks/pre-commit" ]]; then
    chmod +x "$AGENTLAYER_ROOT/.githooks/pre-commit" 2> /dev/null || true
  else
    die "Missing .agent-layer/.githooks/pre-commit"
  fi
else
  say "Skipping hook enable/test (not a git repo)."
fi

say ""
say "Next steps:"
say "  - Run tests (includes checks): ./tests/run.sh"
say ""
say "Dev bootstrap complete."
