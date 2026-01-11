#!/usr/bin/env bash
set -euo pipefail

# Developer bootstrap: install deps, run setup, and enable git hooks.

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

# Resolve the entrypoint helper so we can locate the repo root reliably.
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

# Lightweight command-existence check for dependency discovery.
has_cmd() { command -v "$1" > /dev/null 2>&1; }

# Parse CLI flags and reject unknown options.
ASSUME_YES="0"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes | -y)
      ASSUME_YES="1"
      ;;
    --help | -h)
      say "Usage: ./.agent-layer/dev/bootstrap.sh [--yes]"
      exit 0
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
  shift
done

# Allow non-interactive runs via environment override.
if [[ "${AGENTLAYER_BOOTSTRAP_ASSUME_YES:-}" == "1" ]]; then
  ASSUME_YES="1"
fi

# Detect missing system dependencies needed for dev workflows.
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

# Report what will be installed or skipped.
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

# Confirm before making changes unless --yes was provided.
say "This will:"
say "  - install missing system dependencies (if any)"
say "  - run npm install in .agent-layer (if needed)"
say "  - enable git hooks for this repo (dev-only)"
say "  - run setup (sync + MCP deps; no checks)"
if [[ "$ASSUME_YES" == "1" ]]; then
  say "Proceeding without prompt (--yes)."
else
  if [[ ! -t 0 ]]; then
    die "No TTY available to confirm bootstrap. Use --yes to proceed."
  fi
  read -r -p "Continue? [y/N] " reply
  case "$reply" in
    y | Y | yes | YES) ;;
    *)
      die "Aborted."
      ;;
  esac
fi

# Choose the package manager used for system dependencies.
pkg_manager=""
if has_cmd brew; then
  pkg_manager="brew"
elif has_cmd apt-get; then
  pkg_manager="apt-get"
fi

# Map missing tools to their package names for the detected manager.
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

# Install missing system packages, if any.
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

# Install Node dev dependencies needed by the formatter.
if [[ "$prettier_installed" == "0" ]]; then
  say "==> Installing node dev dependencies (Prettier)"
  (cd "$AGENTLAYER_ROOT" && npm install)
fi

# Run the standard setup script without checks.
say "==> Running setup (no checks)"
bash "$AGENTLAYER_ROOT/setup.sh" --skip-checks

# Enable repo-local git hooks if this is a git working tree.
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

# Print next steps for the developer.
say ""
say "Next steps:"
say "  - Run tests (includes checks):"
say "    - From a consumer repo: ./.agent-layer/tests/run.sh"
say "    - From the agent-layer repo: ./tests/run.sh --work-root <consumer-root>"
say ""
say "Dev bootstrap complete."
