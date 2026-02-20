#!/usr/bin/env bash
# harness.sh — shared helpers for the e2e scenario-based test framework.
# Sourced by test-e2e.sh; not executable on its own.

# ---------------------------------------------------------------------------
# Test tracking (same pattern as test-release.sh)
# ---------------------------------------------------------------------------

# Colors (disabled if stdout is not a terminal)
if [[ -t 1 ]]; then
  _RED='\033[0;31m'
  _GREEN='\033[0;32m'
  _YELLOW='\033[0;33m'
  _NC='\033[0m'
else
  _RED=''
  _GREEN=''
  _YELLOW=''
  _NC=''
fi

E2E_PASS_COUNT=0
E2E_FAIL_COUNT=0
E2E_UPGRADE_SCENARIO_COUNT=0

# pass <label> — record a passing assertion.
pass() {
  echo -e "${_GREEN}PASS${_NC}: $1"
  E2E_PASS_COUNT=$((E2E_PASS_COUNT + 1))
}

# fail <label> — record a failing assertion (does not exit).
fail() {
  echo -e "${_RED}FAIL${_NC}: $1"
  E2E_FAIL_COUNT=$((E2E_FAIL_COUNT + 1))
}

# warn <label> — print a warning (not counted).
warn() {
  echo -e "${_YELLOW}WARN${_NC}: $1"
}

# section <title> — print a section header.
section() {
  echo ""
  echo "=== $1 ==="
}

# skip_if_no_upgrade_manifest — return 0 (true) if upgrade scenarios should be
# skipped because the build version has no migration manifest. Prints a warning.
skip_if_no_upgrade_manifest() {
  if [[ "${AL_E2E_VERSION_NO_V:-0.0.0}" == "0.0.0" ]]; then
    warn "skipping upgrade scenario (AL_E2E_VERSION=0.0.0 has no migration manifest)"
    return 0
  fi
  return 1
}

# ---------------------------------------------------------------------------
# Scenario isolation
# ---------------------------------------------------------------------------

# setup_scenario_dir — create an isolated temp directory with a fake .git/
# dir under $E2E_TMP_ROOT. Prints the path to stdout.
setup_scenario_dir() {
  local dir
  dir="$(mktemp -d "$E2E_TMP_ROOT/scenario-XXXXXX")"
  mkdir -p "$dir/.git"
  echo "$dir"
}

# cleanup_scenario_dir <dir> — remove a scenario directory.
# Also cleaned by the top-level trap in the orchestrator.
cleanup_scenario_dir() {
  local dir="$1"
  if [[ -n "$dir" && -d "$dir" ]]; then
    rm -rf "$dir"
  fi
}

# ---------------------------------------------------------------------------
# Mock management
# ---------------------------------------------------------------------------

# Global: path to the current mock claude log file.
MOCK_CLAUDE_LOG=""

# install_mock_claude <dir> [exit_code] — write a mock "claude" script into
# <dir>/mock-bin/claude and export PATH so it is found first. Sets
# MOCK_CLAUDE_LOG to the log path. Default exit code is 0.
install_mock_claude() {
  local dir="$1"
  local exit_code="${2:-0}"
  local mock_bin="$dir/mock-bin"
  local log_dir="$dir/mock-logs"
  mkdir -p "$mock_bin" "$log_dir"

  MOCK_CLAUDE_LOG="$log_dir/claude.log"
  : > "$MOCK_CLAUDE_LOG"

  cat > "$mock_bin/claude" <<'MOCK_EOF'
#!/usr/bin/env bash
# Mock claude binary — records invocation details to a log file.
log="${MOCK_CLAUDE_LOG_DIR}/claude.log"

{
  echo "ARGC=$#"
  echo "ARGS=$*"
  i=0
  for arg in "$@"; do
    echo "ARG_${i}=${arg}"
    i=$((i + 1))
  done
  # Record all AL_* environment variables
  env | grep '^AL_' | sort || true
  echo "---END---"
} >> "$log"

exit "${MOCK_CLAUDE_EXIT_CODE:-0}"
MOCK_EOF

  chmod +x "$mock_bin/claude"

  # Export env vars so they propagate through launch.go's cmd.Env = env
  export MOCK_CLAUDE_LOG_DIR="$log_dir"
  export MOCK_CLAUDE_EXIT_CODE="$exit_code"
  export PATH="$mock_bin:$PATH"
}

# reset_mock_claude_log — truncate the current mock log (for multi-invocation
# scenarios like 090-idempotency).
reset_mock_claude_log() {
  if [[ -n "$MOCK_CLAUDE_LOG" ]]; then
    : > "$MOCK_CLAUDE_LOG"
  fi
}

# Global: path to the current mock agent log file (set by install_mock_agent).
MOCK_AGENT_LOG=""

# install_mock_agent <dir> <binary_name> [exit_code] — write a generic mock
# agent binary into <dir>/mock-bin/<binary_name>. Sets MOCK_AGENT_LOG to the
# log path. Reuses the same mock-bin directory as install_mock_claude so
# multiple mock binaries coexist on the same PATH.
install_mock_agent() {
  local dir="$1"
  local binary="$2"
  local exit_code="${3:-0}"
  local mock_bin="$dir/mock-bin"
  local log_dir="$dir/mock-logs"
  mkdir -p "$mock_bin" "$log_dir"

  MOCK_AGENT_LOG="$log_dir/${binary}.log"
  : > "$MOCK_AGENT_LOG"

  cat > "$mock_bin/$binary" <<MOCK_EOF
#!/usr/bin/env bash
# Mock $binary binary — records invocation details to a log file.
log="$log_dir/${binary}.log"

{
  echo "ARGC=\$#"
  echo "ARGS=\$*"
  i=0
  for arg in "\$@"; do
    echo "ARG_\${i}=\${arg}"
    i=\$((i + 1))
  done
  env | grep '^AL_' | sort || true
  echo "---END---"
} >> "\$log"

exit $exit_code
MOCK_EOF

  chmod +x "$mock_bin/$binary"
  export PATH="$mock_bin:$PATH"
}

# assert_mock_agent_called <log> — verify mock agent was invoked exactly once.
assert_mock_agent_called() {
  local log="$1"
  if [[ ! -f "$log" ]]; then
    fail "mock agent log not found: $log"
    return
  fi
  local count
  count=$(grep -c -- '---END---' "$log") || count="0"
  if [[ "$count" -eq 1 ]]; then
    pass "mock agent was called exactly once"
  else
    fail "mock agent call count: expected 1, got $count"
  fi
}

# assert_mock_agent_has_arg <log> <arg> — verify a specific arg was passed.
# Uses literal string comparison (not regex) to avoid false positives with
# metacharacters like . in "gemini-2.5-pro".
assert_mock_agent_has_arg() {
  local log="$1" arg="$2"
  local found=0
  while IFS= read -r line; do
    local val="${line#*=}"
    if [[ "$val" == "$arg" ]]; then
      found=1
      break
    fi
  done < <(grep "^ARG_[0-9]*=" "$log" 2>/dev/null)
  if [[ $found -eq 1 ]]; then
    pass "mock agent received arg: $arg"
  else
    fail "mock agent missing arg: $arg"
  fi
}

# assert_mock_agent_lacks_arg <log> <arg> — verify arg was NOT passed.
assert_mock_agent_lacks_arg() {
  local log="$1" arg="$2"
  local found=0
  while IFS= read -r line; do
    local val="${line#*=}"
    if [[ "$val" == "$arg" ]]; then
      found=1
      break
    fi
  done < <(grep "^ARG_[0-9]*=" "$log" 2>/dev/null)
  if [[ $found -eq 1 ]]; then
    fail "mock agent has unexpected arg: $arg"
  else
    pass "mock agent does not have arg: $arg"
  fi
}

# assert_mock_agent_not_called <log> <label> — verify mock agent was NOT invoked.
assert_mock_agent_not_called() {
  local log="$1" label="${2:-mock agent was not called}"
  if [[ ! -f "$log" ]] || ! grep -q -- '---END---' "$log" 2>/dev/null; then
    pass "$label"
  else
    fail "$label (mock agent was invoked)"
  fi
}

# assert_claude_mock_not_called <log> — verify mock claude was NOT invoked.
assert_claude_mock_not_called() {
  local log="$1"
  if [[ ! -f "$log" ]] || ! grep -q -- '---END---' "$log" 2>/dev/null; then
    pass "mock claude was not called (agent is disabled)"
  else
    fail "mock claude should not have been called (agent is disabled)"
  fi
}

# assert_mock_agent_env <log> <var> [value] — verify an env var was set.
assert_mock_agent_env() {
  local log="$1" var="$2" value="${3:-}"
  if [[ -n "$value" ]]; then
    if grep -q "^${var}=${value}$" "$log" 2>/dev/null; then
      pass "mock agent env: ${var}=${value}"
    else
      fail "mock agent env: expected ${var}=${value}"
    fi
  else
    if grep -q "^${var}=" "$log" 2>/dev/null; then
      pass "mock agent env: ${var} is set"
    else
      fail "mock agent env: ${var} not set"
    fi
  fi
}

# assert_mock_agent_env_non_empty <log> <var> — verify env var is set AND non-empty.
assert_mock_agent_env_non_empty() {
  local log="$1" var="$2"
  local val
  val="$(grep "^${var}=" "$log" 2>/dev/null | head -1 | cut -d'=' -f2-)"
  if [[ -z "$val" ]]; then
    fail "mock agent env: ${var} is empty or not set"
  else
    pass "mock agent env: ${var} has non-empty value"
  fi
}

# assert_claude_mock_env_non_empty <log> <var> — verify env var is set AND non-empty.
assert_claude_mock_env_non_empty() {
  local log="$1" var="$2"
  local val
  val="$(grep "^${var}=" "$log" 2>/dev/null | head -1 | cut -d'=' -f2-)"
  if [[ -z "$val" ]]; then
    fail "mock claude env: ${var} is empty or not set"
  else
    pass "mock claude env: ${var} has non-empty value"
  fi
}

# assert_json_valid <file> <label> — verify file contains valid JSON.
# Uses python3 json.load as fallback since jq may not be available.
assert_json_valid() {
  local file="$1" label="$2"
  if command -v python3 &>/dev/null; then
    if python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$file" 2>/dev/null; then
      pass "$label"
    else
      fail "$label (invalid JSON)"
    fi
  else
    warn "$label (skipped: python3 not available for JSON validation)"
  fi
}

# ---------------------------------------------------------------------------
# Release binary download / cache
# ---------------------------------------------------------------------------
# Binaries are stored in a persistent cache (survives across test runs).
# Network downloads are gated behind AL_E2E_ONLINE=1.

# _e2e_bin_cache_dir — persistent cache for downloaded release binaries.
_e2e_bin_cache_dir() {
  local dir="${AL_E2E_BIN_CACHE:-$HOME/.cache/al-e2e/bin}"
  mkdir -p "$dir"
  echo "$dir"
}

# resolve_latest_release_version — query GitHub for the latest release tag.
# Prints the version without the "v" prefix (e.g. "0.8.3"). Returns 1 on
# failure. Requires AL_E2E_ONLINE=1 (makes a network call).
resolve_latest_release_version() {
  if [[ "${AL_E2E_ONLINE:-}" != "1" ]]; then
    return 1
  fi
  local response tag
  response=$(curl -fsSL \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/conn-castle/agent-layer/releases/latest" 2>/dev/null) || return 1
  # Extract tag_name without jq (minimise dependencies).
  tag=$(echo "$response" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')
  if [[ -z "$tag" ]]; then
    return 1
  fi
  echo "${tag#v}"
}

# download_or_cache_binary <version> — return path to a cached release binary,
# downloading it first if AL_E2E_ONLINE=1 and it is not already cached.
# Uses a persistent cache at ~/.cache/al-e2e/bin/ (override with
# AL_E2E_BIN_CACHE). Requires E2E_PLATFORM_OS and E2E_PLATFORM_ARCH
# (exported by orchestrator). Returns 1 when the binary is not available.
download_or_cache_binary() {
  local version="$1"
  local cache_dir
  cache_dir="$(_e2e_bin_cache_dir)"

  local binary_name="al-${E2E_PLATFORM_OS}-${E2E_PLATFORM_ARCH}"
  local cached_path="$cache_dir/al-${version}-${E2E_PLATFORM_OS}-${E2E_PLATFORM_ARCH}"

  # Return immediately if already cached.
  if [[ -f "$cached_path" && -x "$cached_path" ]]; then
    echo "$cached_path"
    return 0
  fi

  # Download only when online mode is enabled.
  if [[ "${AL_E2E_ONLINE:-}" != "1" ]]; then
    return 1
  fi

  local url="https://github.com/conn-castle/agent-layer/releases/download/v${version}/${binary_name}"
  if ! curl -fsSL -o "$cached_path" "$url" 2>/dev/null; then
    rm -f "$cached_path"
    return 1
  fi

  chmod +x "$cached_path"
  echo "$cached_path"
}

# setup_old_version_via_binary <dir> <binary_path> — run an old release
# binary's `init --no-wizard` inside <dir> to create authentic .agent-layer/
# state. Creates .git/ if missing. Returns 1 on failure.
setup_old_version_via_binary() {
  local dir="$1"
  local binary_path="$2"

  if [[ ! -x "$binary_path" ]]; then
    fail "old binary not executable: $binary_path"
    return 1
  fi

  mkdir -p "$dir/.git"
  if ! (cd "$dir" && "$binary_path" init --no-wizard) >/dev/null 2>&1; then
    fail "old binary init failed: $binary_path"
    return 1
  fi
}

# skip_if_no_oldest_binary — return 0 (true) if scenarios requiring the
# oldest release binary should be skipped.
skip_if_no_oldest_binary() {
  if skip_if_no_upgrade_manifest; then return 0; fi
  if [[ -z "${E2E_OLDEST_BINARY:-}" || ! -x "${E2E_OLDEST_BINARY:-}" ]]; then
    warn "skipping scenario (v${E2E_OLDEST_VERSION:-?} binary not available)"
    return 0
  fi
  return 1
}

# skip_if_no_latest_binary — return 0 (true) if scenarios requiring the
# latest release binary should be skipped.
skip_if_no_latest_binary() {
  if skip_if_no_upgrade_manifest; then return 0; fi
  if [[ -z "${E2E_LATEST_BINARY:-}" || ! -x "${E2E_LATEST_BINARY:-}" ]]; then
    warn "skipping scenario (latest release binary not available)"
    return 0
  fi
  return 1
}

# get_latest_snapshot_id <dir> — list upgrade snapshots and return the stem
# of the newest file.
get_latest_snapshot_id() {
  local dir="$1"
  local snapshot_dir="$dir/.agent-layer/state/upgrade-snapshots"
  if [[ ! -d "$snapshot_dir" ]]; then
    echo ""
    return 1
  fi
  # shellcheck disable=SC2012
  ls -t "$snapshot_dir"/*.json 2>/dev/null | head -1 | xargs -I{} basename {} .json
}

# ---------------------------------------------------------------------------
# Portable SHA-256
# ---------------------------------------------------------------------------

# portable_sha256 <file> — cross-platform SHA-256 hash. Tries sha256sum
# first, falls back to shasum -a 256 (macOS). Outputs: <hash>  <file>
portable_sha256() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file"
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file"
  else
    fail "neither sha256sum nor shasum available"
    return 1
  fi
}

# ---------------------------------------------------------------------------
# Assertions
# ---------------------------------------------------------------------------

# assert_file_exists <path> <label>
assert_file_exists() {
  local path="$1" label="$2"
  if [[ -f "$path" ]]; then
    pass "$label"
  else
    fail "$label (file not found: $path)"
  fi
}

# assert_file_not_exists <path> <label>
assert_file_not_exists() {
  local path="$1" label="$2"
  if [[ ! -f "$path" ]]; then
    pass "$label"
  else
    fail "$label (file unexpectedly exists: $path)"
  fi
}

# assert_dir_exists <path> <label>
assert_dir_exists() {
  local path="$1" label="$2"
  if [[ -d "$path" ]]; then
    pass "$label"
  else
    fail "$label (directory not found: $path)"
  fi
}

# assert_file_contains <path> <pattern> <label> — fixed-string match.
assert_file_contains() {
  local path="$1" pattern="$2" label="$3"
  if [[ ! -f "$path" ]]; then
    fail "$label (file not found: $path)"
    return
  fi
  if grep -qF -- "$pattern" "$path"; then
    pass "$label"
  else
    fail "$label (pattern not found in $path: $pattern)"
  fi
}

# assert_file_not_contains <path> <pattern> <label> — fixed-string match.
assert_file_not_contains() {
  local path="$1" pattern="$2" label="$3"
  if [[ ! -f "$path" ]]; then
    pass "$label (file does not exist)"
    return
  fi
  if grep -qF -- "$pattern" "$path"; then
    fail "$label (pattern found in $path: $pattern)"
  else
    pass "$label"
  fi
}

# assert_output_equals <actual> <expected> <label>
assert_output_equals() {
  local actual="$1" expected="$2" label="$3"
  if [[ "$actual" == "$expected" ]]; then
    pass "$label"
  else
    fail "$label (expected: '$expected', got: '$actual')"
  fi
}

# assert_exit_zero <label> <cmd...> — run command, pass if exit code is 0.
# Captures stdout+stderr for debugging on failure.
assert_exit_zero() {
  local label="$1"
  shift
  local output
  local rc=0
  output=$("$@" 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "$label"
  else
    fail "$label (exit code: $rc)"
    # Print first few lines of output for debugging
    if [[ -n "$output" ]]; then
      echo "  output (first 10 lines):"
      echo "$output" | head -10 | sed 's/^/    /'
    fi
  fi
}

# assert_exit_zero_in <dir> <label> <cmd...> — cd into dir and run command,
# pass if exit code is 0. The cd happens in a command substitution subshell so
# the caller's cwd is not changed, but pass/fail counting happens in the parent.
assert_exit_zero_in() {
  local dir="$1" label="$2"
  shift 2
  local output
  local rc=0
  output=$(cd "$dir" && "$@" 2>&1) || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "$label"
  else
    fail "$label (exit code: $rc)"
    if [[ -n "$output" ]]; then
      echo "  output (first 10 lines):"
      echo "$output" | head -10 | sed 's/^/    /'
    fi
  fi
}

# assert_exit_nonzero <label> <cmd...> — run command, pass if exit code != 0.
assert_exit_nonzero() {
  local label="$1"
  shift
  local rc=0
  "$@" >/dev/null 2>&1 || rc=$?
  if [[ $rc -ne 0 ]]; then
    pass "$label"
  else
    fail "$label (expected non-zero exit, got 0)"
  fi
}

# run_capturing <var_name> <cmd...> — run command, store combined stdout+stderr
# in the named variable. Returns the command's exit code.
run_capturing() {
  local __var_name="$1"
  shift
  local __rc=0
  local __output
  __output=$("$@" 2>&1) || __rc=$?
  eval "$__var_name=\$__output"
  return "$__rc"
}

# assert_output_contains <output> <pattern> <label> — check captured output
# contains the given fixed string (not regex).
assert_output_contains() {
  local output="$1" pattern="$2" label="$3"
  if echo "$output" | grep -qF -- "$pattern"; then
    pass "$label"
  else
    fail "$label (pattern not found in output: $pattern)"
    echo "  output (first 5 lines):"
    echo "$output" | head -5 | sed 's/^/    /'
  fi
}

# assert_output_not_contains <output> <pattern> <label> — check captured output
# does NOT contain the given fixed string (not regex).
assert_output_not_contains() {
  local output="$1" pattern="$2" label="$3"
  if echo "$output" | grep -qF -- "$pattern"; then
    fail "$label (unexpected pattern found in output: $pattern)"
  else
    pass "$label"
  fi
}

# assert_al_init_structure <dir> — verify the .agent-layer/ structure
# created by `al init`.
assert_al_init_structure() {
  local dir="$1"
  assert_file_exists "$dir/.agent-layer/config.toml" "init created config.toml"
  assert_file_exists "$dir/.agent-layer/al.version" "init created al.version"
  assert_dir_exists "$dir/.agent-layer/instructions" "init created instructions/"
  assert_dir_exists "$dir/.agent-layer/slash-commands" "init created slash-commands/"
  assert_file_exists "$dir/.agent-layer/.env" "init created .env"
  assert_file_exists "$dir/.agent-layer/commands.allow" "init created commands.allow"
}

# assert_al_version_content <dir> <expected> — verify al.version matches.
assert_al_version_content() {
  local dir="$1" expected="$2"
  local actual
  if [[ ! -f "$dir/.agent-layer/al.version" ]]; then
    fail "al.version missing at $dir/.agent-layer/al.version"
    return
  fi
  actual="$(cat "$dir/.agent-layer/al.version")"
  assert_output_equals "$actual" "$expected" "al.version content is $expected"
}

# Sync output paths (relative to repo root) — source of truth from
# internal/sync/*.go. Used by assert_generated_artifacts and idempotency.
_SYNC_OUTPUT_PATHS=(
  "CLAUDE.md"
  "AGENTS.md"
  "GEMINI.md"
  ".github/copilot-instructions.md"
  ".codex/AGENTS.md"
  ".claude/settings.json"
  ".mcp.json"
)

# assert_generated_artifacts <dir> — verify all sync output files exist and
# contain expected content markers (not just existence checks).
assert_generated_artifacts() {
  local dir="$1"
  for rel_path in "${_SYNC_OUTPUT_PATHS[@]}"; do
    assert_file_exists "$dir/$rel_path" "sync generated $rel_path"
  done
  # Verify managed markers in instruction shims (all use the same header)
  assert_file_contains "$dir/CLAUDE.md" "GENERATED FILE" "CLAUDE.md has managed marker"
  assert_file_contains "$dir/AGENTS.md" "GENERATED FILE" "AGENTS.md has managed marker"
  assert_file_contains "$dir/GEMINI.md" "GENERATED FILE" "GEMINI.md has managed marker"
  assert_file_contains "$dir/.github/copilot-instructions.md" "GENERATED FILE" \
    "copilot-instructions.md has managed marker"
  assert_file_contains "$dir/.codex/AGENTS.md" "GENERATED FILE" \
    "codex AGENTS.md has managed marker"
  # Verify JSON files have expected structure
  assert_file_contains "$dir/.mcp.json" "agent-layer" ".mcp.json has agent-layer server"
  assert_file_contains "$dir/.mcp.json" "mcp-prompts" ".mcp.json has mcp-prompts command"
  assert_file_contains "$dir/.claude/settings.json" "permissions" \
    "settings.json has permissions key"
}

# assert_claude_mock_called <log> — verify mock was invoked exactly once.
assert_claude_mock_called() {
  local log="$1"
  if [[ ! -f "$log" ]]; then
    fail "mock claude log not found: $log"
    return
  fi
  local count
  count=$(grep -c -- '---END---' "$log") || count="0"
  if [[ "$count" -eq 1 ]]; then
    pass "mock claude was called exactly once"
  else
    fail "mock claude call count: expected 1, got $count"
  fi
}

# assert_claude_mock_has_arg <log> <arg> — verify a specific arg was passed.
# Uses literal string comparison (not regex) to avoid false positives with
# metacharacters like . in version strings.
assert_claude_mock_has_arg() {
  local log="$1" arg="$2"
  local found=0
  while IFS= read -r line; do
    local val="${line#*=}"
    if [[ "$val" == "$arg" ]]; then
      found=1
      break
    fi
  done < <(grep "^ARG_[0-9]*=" "$log" 2>/dev/null)
  if [[ $found -eq 1 ]]; then
    pass "mock claude received arg: $arg"
  else
    fail "mock claude missing arg: $arg"
  fi
}

# assert_claude_mock_lacks_arg <log> <arg> — verify arg was NOT passed.
assert_claude_mock_lacks_arg() {
  local log="$1" arg="$2"
  local found=0
  while IFS= read -r line; do
    local val="${line#*=}"
    if [[ "$val" == "$arg" ]]; then
      found=1
      break
    fi
  done < <(grep "^ARG_[0-9]*=" "$log" 2>/dev/null)
  if [[ $found -eq 1 ]]; then
    fail "mock claude has unexpected arg: $arg"
  else
    pass "mock claude does not have arg: $arg"
  fi
}

# assert_claude_mock_env <log> <var> [value] — verify an env var was set.
assert_claude_mock_env() {
  local log="$1" var="$2" value="${3:-}"
  if [[ -n "$value" ]]; then
    if grep -q "^${var}=${value}$" "$log" 2>/dev/null; then
      pass "mock claude env: ${var}=${value}"
    else
      fail "mock claude env: expected ${var}=${value}"
    fi
  else
    if grep -q "^${var}=" "$log" 2>/dev/null; then
      pass "mock claude env: ${var} is set"
    else
      fail "mock claude env: ${var} not set"
    fi
  fi
}

# _snapshot_sync_outputs <dir> — compute SHA-256 hashes of all sync output
# files. Prints sorted hash lines to stdout.
_snapshot_sync_outputs() {
  local dir="$1"
  for rel_path in "${_SYNC_OUTPUT_PATHS[@]}"; do
    local full_path="$dir/$rel_path"
    if [[ -f "$full_path" ]]; then
      portable_sha256 "$full_path"
    fi
  done | sort
}

# assert_generated_files_unchanged <dir> <label> — compare a previously
# captured snapshot (in $E2E_SNAPSHOT_FILE) with the current state.
# Call _snapshot_sync_outputs before the second run and store in
# $E2E_SNAPSHOT_FILE, then call this after.
assert_generated_files_unchanged() {
  local dir="$1" label="$2"
  if [[ -z "${E2E_SNAPSHOT_FILE:-}" || ! -f "${E2E_SNAPSHOT_FILE:-}" ]]; then
    fail "$label (no snapshot file set)"
    return
  fi
  local current
  current="$(_snapshot_sync_outputs "$dir")"
  local previous
  previous="$(cat "$E2E_SNAPSHOT_FILE")"
  if [[ "$current" == "$previous" ]]; then
    pass "$label"
  else
    fail "$label (sync outputs changed between runs)"
    diff <(echo "$previous") <(echo "$current") | head -20 | sed 's/^/    /' || true
  fi
}

# _snapshot_agent_layer_state <dir> — compute SHA-256 hashes of core .agent-layer/
# state files (config, version, env, instructions, slash-commands). Prints sorted
# hash lines to stdout.
_snapshot_agent_layer_state() {
  local dir="$1"
  local al_dir="$dir/.agent-layer"
  {
    for f in config.toml al.version .env commands.allow; do
      [[ -f "$al_dir/$f" ]] && portable_sha256 "$al_dir/$f"
    done
    if [[ -d "$al_dir/instructions" ]]; then
      for f in "$al_dir/instructions"/*.md; do
        [[ -f "$f" ]] && portable_sha256 "$f"
      done
    fi
    if [[ -d "$al_dir/slash-commands" ]]; then
      for f in "$al_dir/slash-commands"/*.md; do
        [[ -f "$f" ]] && portable_sha256 "$f"
      done
    fi
  } | sort
}

# _snapshot_all_state <dir> — combined snapshot of sync outputs and core
# .agent-layer/ state. Used by idempotency tests.
_snapshot_all_state() {
  local dir="$1"
  {
    _snapshot_sync_outputs "$dir"
    _snapshot_agent_layer_state "$dir"
  } | sort
}

# assert_all_state_unchanged <dir> <snapshot_file> <label> — compare a
# previously captured all-state snapshot with the current state.
assert_all_state_unchanged() {
  local dir="$1" snapshot_file="$2" label="$3"
  if [[ ! -f "$snapshot_file" ]]; then
    fail "$label (no snapshot file: $snapshot_file)"
    return
  fi
  local current
  current="$(_snapshot_all_state "$dir")"
  local previous
  previous="$(cat "$snapshot_file")"
  if [[ "$current" == "$previous" ]]; then
    pass "$label"
  else
    fail "$label (state changed between runs)"
    diff <(echo "$previous") <(echo "$current") | head -20 | sed 's/^/    /' || true
  fi
}

# assert_agent_layer_state_unchanged <dir> <snapshot_file> <label> — compare a
# previously captured .agent-layer/ state snapshot with the current state.
assert_agent_layer_state_unchanged() {
  local dir="$1" snapshot_file="$2" label="$3"
  if [[ ! -f "$snapshot_file" ]]; then
    fail "$label (no snapshot file: $snapshot_file)"
    return
  fi
  local current
  current="$(_snapshot_agent_layer_state "$dir")"
  local previous
  previous="$(cat "$snapshot_file")"
  if [[ "$current" == "$previous" ]]; then
    pass "$label"
  else
    fail "$label (.agent-layer/ state changed)"
    diff <(echo "$previous") <(echo "$current") | head -20 | sed 's/^/    /' || true
  fi
}

# assert_no_crash_markers <output> <label> — verify output has no Go crash
# indicators (panic, runtime error, fatal error, goroutine dump).
assert_no_crash_markers() {
  local output="$1" label="$2"
  local found=0
  for marker in "panic:" "runtime error:" "fatal error:" "goroutine "; do
    if echo "$output" | grep -qF -- "$marker"; then
      found=1
      fail "$label (crash marker found: $marker)"
    fi
  done
  if [[ $found -eq 0 ]]; then
    pass "$label"
  fi
}
