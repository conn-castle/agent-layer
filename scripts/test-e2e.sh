#!/usr/bin/env bash
# test-e2e.sh — scenario-based end-to-end test orchestrator.
# Replaces the previous monolithic e2e script. Run via: make test-e2e
#
# Environment variables:
#   AL_E2E_VERSION          — version to build/test (default: 0.0.0)
#   AL_E2E_SCENARIOS        — glob filter for scenario files (default: *)
#                             e.g. AL_E2E_SCENARIOS="upgrade*" for upgrade scenarios only
#   AL_E2E_ONLINE           — set to "1" to enable downloading release binaries from
#                             GitHub (default: offline, uses persistent cache only)
#   AL_E2E_REQUIRE_UPGRADE  — set to "1" to fail hard if upgrade binaries are missing
#                             (for CI with pre-cached binaries)
#   AL_E2E_LATEST_VERSION   — pin the "latest release" version for upgrade tests
#                             (default: resolved from GitHub API when online)
#   AL_E2E_BIN_CACHE        — persistent binary cache dir
#                             (default: $HOME/.cache/al-e2e/bin)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# ---------------------------------------------------------------------------
# Source harness
# ---------------------------------------------------------------------------
# shellcheck source=test-e2e/harness.sh
source "$SCRIPT_DIR/test-e2e/harness.sh"

# ---------------------------------------------------------------------------
# Global environment policy
# ---------------------------------------------------------------------------
# Keep e2e runs hermetic: only AL_E2E_* control vars are inherited.
# Other host AL_* variables (especially secrets) must not influence scenarios.
while IFS= read -r env_name; do
  case "$env_name" in
    AL_E2E_*) ;;
    *) unset "$env_name" ;;
  esac
done < <(env | awk -F= '/^AL_/ {print $1}')

export AL_NO_NETWORK=1

# If AL_E2E_VERSION is not set, auto-detect the latest migration manifest
# version from the codebase. This ensures upgrade scenarios always run
# (they require a version with a migration manifest). The binary is built
# from the current code but tagged with the detected version.
if [[ -z "${AL_E2E_VERSION:-}" || "${AL_E2E_VERSION:-}" == "0.0.0" ]]; then
  _manifest_dir="$ROOT_DIR/internal/templates/migrations"
  if [[ -d "$_manifest_dir" ]]; then
    _detected_version="$(ls "$_manifest_dir"/*.json 2>/dev/null \
      | xargs -I{} basename {} .json \
      | sort -t. -k1,1n -k2,2n -k3,3n \
      | tail -1 || true)"
  fi
  if [[ -n "${_detected_version:-}" ]]; then
    AL_E2E_VERSION="v${_detected_version}"
    echo "Info: AL_E2E_VERSION not set; auto-detected ${AL_E2E_VERSION} from migration manifests."
  else
    AL_E2E_VERSION="0.0.0"
    echo "Info: AL_E2E_VERSION not set and no manifests found; using ${AL_E2E_VERSION} (upgrade scenarios will skip)."
  fi
fi
AL_E2E_VERSION_NO_V="${AL_E2E_VERSION#v}"
export AL_E2E_VERSION
export AL_E2E_VERSION_NO_V

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
section "Build"

E2E_TMP_ROOT="$(mktemp -d)"
export E2E_TMP_ROOT
trap 'rm -rf "$E2E_TMP_ROOT"' EXIT

dist_dir="$E2E_TMP_ROOT/dist"
AL_VERSION="$AL_E2E_VERSION" DIST_DIR="$dist_dir" "$ROOT_DIR/scripts/build-release.sh"

# Detect local platform (exported for harness download_or_cache_binary)
E2E_PLATFORM_OS="$(uname -s)"
E2E_PLATFORM_ARCH="$(uname -m)"
case "$E2E_PLATFORM_OS" in
  Darwin) E2E_PLATFORM_OS="darwin" ;;
  Linux)  E2E_PLATFORM_OS="linux" ;;
  *)      echo "Error: unsupported OS: $E2E_PLATFORM_OS" >&2; exit 1 ;;
esac
case "$E2E_PLATFORM_ARCH" in
  x86_64|amd64)   E2E_PLATFORM_ARCH="amd64" ;;
  arm64|aarch64)   E2E_PLATFORM_ARCH="arm64" ;;
  *)               echo "Error: unsupported architecture: $E2E_PLATFORM_ARCH" >&2; exit 1 ;;
esac
export E2E_PLATFORM_OS E2E_PLATFORM_ARCH

E2E_BIN="$dist_dir/al-${E2E_PLATFORM_OS}-${E2E_PLATFORM_ARCH}"
if [[ ! -f "$E2E_BIN" ]]; then
  echo "Error: missing built binary: $E2E_BIN" >&2
  exit 1
fi
export E2E_BIN

# Install to a prefix so `al` is on PATH (required by resolvePromptServerCommand)
E2E_INSTALL_PREFIX="$E2E_TMP_ROOT/install-prefix"
mkdir -p "$E2E_INSTALL_PREFIX/bin"
cp "$E2E_BIN" "$E2E_INSTALL_PREFIX/bin/al"
chmod +x "$E2E_INSTALL_PREFIX/bin/al"
export E2E_INSTALL_PREFIX

# Put the installed al on PATH (scenarios prepend mock-bin before this)
export PATH="$E2E_INSTALL_PREFIX/bin:$PATH"

# Export paths needed by scenarios
export E2E_DIST_DIR="$dist_dir"
E2E_FIXTURE_DIR="$SCRIPT_DIR/test-e2e/fixtures"
export E2E_FIXTURE_DIR

# ---------------------------------------------------------------------------
# Generate defaults.toml at runtime (prevents drift from template)
# ---------------------------------------------------------------------------
E2E_DEFAULTS_TOML="$E2E_TMP_ROOT/defaults.toml"
cp "$ROOT_DIR/internal/templates/config.toml" "$E2E_DEFAULTS_TOML"
export E2E_DEFAULTS_TOML

pass "build completed (${AL_E2E_VERSION})"

# ---------------------------------------------------------------------------
# Resolve upgrade binaries (hermetic by default)
# ---------------------------------------------------------------------------
# Default: fully offline — reads only from the persistent binary cache.
# AL_E2E_ONLINE=1: downloads missing binaries from GitHub releases.
# AL_E2E_REQUIRE_UPGRADE=1: fails hard if binaries are unavailable (CI mode).

E2E_OLDEST_VERSION=""
E2E_OLDEST_BINARY=""
E2E_LATEST_VERSION=""
E2E_LATEST_BINARY=""

if [[ "$AL_E2E_VERSION_NO_V" != "0.0.0" ]]; then
  section "Resolve upgrade binaries"

  # --- Oldest supported version (pinned) ---
  E2E_OLDEST_VERSION="0.8.0"
  E2E_OLDEST_BINARY="$(download_or_cache_binary "$E2E_OLDEST_VERSION")" || true
  if [[ -n "$E2E_OLDEST_BINARY" && -x "$E2E_OLDEST_BINARY" ]]; then
    pass "v${E2E_OLDEST_VERSION} binary available"
  else
    E2E_OLDEST_BINARY=""
    if [[ "${AL_E2E_REQUIRE_UPGRADE:-}" == "1" ]]; then
      fail "v${E2E_OLDEST_VERSION} binary required but not available (run with AL_E2E_ONLINE=1 to download)"
    else
      warn "v${E2E_OLDEST_VERSION} binary not cached; upgrade scenarios will skip (run with AL_E2E_ONLINE=1 to download)"
    fi
  fi

  # --- Latest release version ---
  # Use explicit pin if provided, otherwise resolve from GitHub API (online only).
  E2E_LATEST_VERSION="${AL_E2E_LATEST_VERSION:-}"
  if [[ -z "$E2E_LATEST_VERSION" ]]; then
    E2E_LATEST_VERSION="$(resolve_latest_release_version)" || true
  fi

  if [[ -n "$E2E_LATEST_VERSION" && "$E2E_LATEST_VERSION" != "$AL_E2E_VERSION_NO_V" ]]; then
    E2E_LATEST_BINARY="$(download_or_cache_binary "$E2E_LATEST_VERSION")" || true
    if [[ -n "$E2E_LATEST_BINARY" && -x "$E2E_LATEST_BINARY" ]]; then
      pass "v${E2E_LATEST_VERSION} (latest release) binary available"
    else
      E2E_LATEST_BINARY=""
      if [[ "${AL_E2E_REQUIRE_UPGRADE:-}" == "1" ]]; then
        fail "v${E2E_LATEST_VERSION} binary required but not available (run with AL_E2E_ONLINE=1 to download)"
      else
        warn "v${E2E_LATEST_VERSION} binary not cached; latest-release upgrade scenarios will skip"
      fi
    fi
  elif [[ -n "$E2E_LATEST_VERSION" ]]; then
    # Latest release IS the current build version — no upgrade to test.
    E2E_LATEST_VERSION=""
    E2E_LATEST_BINARY=""
    warn "latest release matches build version; skipping latest-release upgrade tests"
  else
    if [[ "${AL_E2E_REQUIRE_UPGRADE:-}" == "1" ]]; then
      fail "latest release version unknown (set AL_E2E_LATEST_VERSION or run with AL_E2E_ONLINE=1)"
    else
      warn "latest release version unknown (set AL_E2E_LATEST_VERSION or run with AL_E2E_ONLINE=1)"
    fi
  fi
fi

export E2E_OLDEST_VERSION E2E_OLDEST_BINARY
export E2E_LATEST_VERSION E2E_LATEST_BINARY

# ---------------------------------------------------------------------------
# Isolate HOME so compiled binaries cannot write to the real user's home
# directory (e.g. ~/.gemini/trustedFolders.json).
# ---------------------------------------------------------------------------
# Pin the binary cache dir before changing HOME so upgrade scenarios
# still resolve the persistent cache at its original location.
export AL_E2E_BIN_CACHE="${AL_E2E_BIN_CACHE:-$HOME/.cache/al-e2e/bin}"

E2E_ORIG_HOME="$HOME"
export E2E_ORIG_HOME
export HOME="$E2E_TMP_ROOT/home"
mkdir -p "$HOME"

# ---------------------------------------------------------------------------
# Auto-discover and source scenarios
# ---------------------------------------------------------------------------
scenario_filter="${AL_E2E_SCENARIOS:-*}"
scenario_dir="$SCRIPT_DIR/test-e2e/scenarios"
scenario_functions=()

for scenario_file in "$scenario_dir"/${scenario_filter}.sh; do
  [[ -f "$scenario_file" ]] || continue
  # shellcheck disable=SC1090
  source "$scenario_file"
  # Derive function name from filename: fresh-install-claude.sh -> run_scenario_fresh_install_claude
  basename="$(basename "$scenario_file" .sh)"
  func_name="${basename//-/_}"
  scenario_functions+=("run_scenario_${func_name}")
done

if [[ ${#scenario_functions[@]} -eq 0 ]]; then
  echo "Error: no scenarios found matching filter: $scenario_filter" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Run scenarios
# ---------------------------------------------------------------------------
# Disable set -e so fail() can accumulate without aborting
set +e

for func in "${scenario_functions[@]}"; do
  if declare -f "$func" >/dev/null 2>&1; then
    "$func"
  else
    fail "scenario function not found: $func"
  fi
done

set -e

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
section "E2E Summary"

total=$((E2E_PASS_COUNT + E2E_FAIL_COUNT))
printf 'Tests: %s total, %b%s passed%b, %b%s failed%b\n' \
  "$total" "$_GREEN" "$E2E_PASS_COUNT" "$_NC" "$_RED" "$E2E_FAIL_COUNT" "$_NC"
printf 'Upgrade scenarios executed: %s\n' "$E2E_UPGRADE_SCENARIO_COUNT"

# When require mode is on, ensure at least one upgrade scenario actually ran.
if [[ "${AL_E2E_REQUIRE_UPGRADE:-}" == "1" && "$E2E_UPGRADE_SCENARIO_COUNT" -eq 0 ]]; then
  fail "AL_E2E_REQUIRE_UPGRADE=1 but zero upgrade scenarios executed"
fi

if [[ $E2E_FAIL_COUNT -gt 0 ]]; then
  exit 1
fi
