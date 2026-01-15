# Shared test helpers for Bats suites.
# Keep helpers deterministic and side-effect free outside temp directories.

# Reset any global root overrides inherited from the test runner.
unset PARENT_ROOT AGENT_LAYER_ROOT

# Resolve the agent-layer root for test fixtures.
AGENT_LAYER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
export AGENT_LAYER_ROOT

# Ensure a default GitHub token is available for sync tests that inline it.
export GITHUB_PERSONAL_ACCESS_TOKEN="${GITHUB_PERSONAL_ACCESS_TOKEN:-agent-layer-test-token}"

# Join arguments into a newline-delimited string.
multiline() {
  printf '%s\n' "$@"
}

# Create a temporary directory under .agent-layer/tmp.
make_tmp_dir() {
  local base dir
  base="$AGENT_LAYER_ROOT/tmp"
  if [[ ! -d "$base" ]]; then
    printf "ERROR: base directory does not exist: %s\n" "$base" >&2
    printf "  AGENT_LAYER_ROOT=%s\n" "$AGENT_LAYER_ROOT" >&2
    printf "  PWD=%s\n" "$PWD" >&2
    mkdir -p "$base" || {
      printf "ERROR: mkdir -p failed\n" >&2
      return 1
    }
  fi
  dir="$(mktemp -d "$base/agent-layer-test.XXXXXX" 2>&1)"
  if [[ $? -ne 0 ]]; then
    printf "ERROR: mktemp failed: %s\n" "$dir" >&2
    return 1
  fi
  if [[ ! -d "$dir" ]]; then
    printf "ERROR: mktemp succeeded but directory doesn't exist: %s\n" "$dir" >&2
    ls -la "$base" >&2 2>&1 || true
    return 1
  fi
  printf "%s" "$dir"
}

# Write an agent config file with explicit enable flags.
write_agent_config() {
  local path="$1"
  local gemini_enabled="${2:-false}"
  local claude_enabled="${3:-false}"
  local codex_enabled="${4:-false}"
  local vscode_enabled="${5:-false}"
  cat >"$path" <<EOF
{
  "gemini": { "enabled": $gemini_enabled },
  "claude": { "enabled": $claude_enabled },
  "codex": {
    "enabled": $codex_enabled,
    "defaultArgs": ["--model", "gpt-5.2-codex", "--reasoning", "high"]
  },
  "vscode": { "enabled": $vscode_enabled }
}
EOF
}

# Backup and restore the repo-level agent config.
backup_agent_config() {
  local backup_dir backup_path
  backup_dir="$AGENT_LAYER_ROOT/tmp"
  mkdir -p "$backup_dir"
  backup_path="$(mktemp "$backup_dir/agent-config.backup.XXXXXX")"
  cp "$AGENT_LAYER_ROOT/config/agents.json" "$backup_path"
  printf "%s" "$backup_path"
}

restore_agent_config() {
  local backup_path="$1"
  if [[ -n "$backup_path" && -f "$backup_path" ]]; then
    cp "$backup_path" "$AGENT_LAYER_ROOT/config/agents.json"
    rm -f "$backup_path"
  fi
}

# Create a parent repo root that symlinks the real .agent-layer.
create_parent_root() {
  local root
  root="$(make_tmp_dir)"
  ln -s "$AGENT_LAYER_ROOT" "$root/.agent-layer"
  mkdir -p "$root/sub/dir"
  printf "%s" "$root"
}

# Create a stub node binary that exits successfully.
create_stub_node() {
  local root="$1"
  local bin="$root/stub-bin"
  mkdir -p "$bin"
  cat >"$bin/node" <<'NODE'
#!/usr/bin/env bash
if [[ " $* " == *" --print-shell "* ]]; then
  printf "%s\n" "enabled=true"
fi
exit 0
NODE
  chmod +x "$bin/node"
  printf "%s" "$bin"
}

# Create stub node and npm binaries that exit successfully.
create_stub_tools() {
  local root="$1"
  local bin="$root/stub-bin"
  mkdir -p "$bin"
  cat >"$bin/node" <<'NODE'
#!/usr/bin/env bash
if [[ " $* " == *" --print-shell "* ]]; then
  printf "%s\n" "enabled=true"
fi
exit 0
NODE
  cat >"$bin/npm" <<'NPM'
#!/usr/bin/env bash
exit 0
NPM
  chmod +x "$bin/node" "$bin/npm"
  printf "%s" "$bin"
}

# Populate a minimal agent-layer root at the provided path.
create_agent_layer_root() {
  local root="$1"
  mkdir -p "$root/src/lib" "$root/src/sync" "$root/config"
  cp "$AGENT_LAYER_ROOT/src/lib/parent-root.sh" "$root/src/lib/parent-root.sh"
  cp "$AGENT_LAYER_ROOT/src/lib/temp-parent-root.sh" "$root/src/lib/temp-parent-root.sh"
  cp "$AGENT_LAYER_ROOT/src/lib/entrypoint.sh" "$root/src/lib/entrypoint.sh"
  cp "$AGENT_LAYER_ROOT/config/agents.json" "$root/config/agents.json"
  : >"$root/src/sync/sync.mjs"
}

# Build an isolated parent root with copied agent-layer scripts.
create_isolated_parent_root() {
  local root agent_layer_dir
  root="$(make_tmp_dir)"
  agent_layer_dir="$root/.agent-layer"
  mkdir -p "$agent_layer_dir/src/lib" "$agent_layer_dir/src/sync" "$agent_layer_dir/dev" \
    "$agent_layer_dir/.githooks" "$agent_layer_dir/tests" "$agent_layer_dir/config"
  cp "$AGENT_LAYER_ROOT/src/lib/parent-root.sh" "$agent_layer_dir/src/lib/parent-root.sh"
  cp "$AGENT_LAYER_ROOT/src/lib/entrypoint.sh" "$agent_layer_dir/src/lib/entrypoint.sh"
  cp "$AGENT_LAYER_ROOT/src/lib/temp-parent-root.sh" "$agent_layer_dir/src/lib/temp-parent-root.sh"
  cp "$AGENT_LAYER_ROOT/src/lib/agent-config.mjs" "$agent_layer_dir/src/lib/agent-config.mjs"
  cp "$AGENT_LAYER_ROOT/src/sync/utils.mjs" "$agent_layer_dir/src/sync/utils.mjs"
  cp "$AGENT_LAYER_ROOT/src/sync/paths.mjs" "$agent_layer_dir/src/sync/paths.mjs"
  cp "$AGENT_LAYER_ROOT/src/sync/policy.mjs" "$agent_layer_dir/src/sync/policy.mjs"
  cp "$AGENT_LAYER_ROOT/src/sync/mcp.mjs" "$agent_layer_dir/src/sync/mcp.mjs"
  cp "$AGENT_LAYER_ROOT/src/sync/clean.mjs" "$agent_layer_dir/src/sync/clean.mjs"
  cp "$AGENT_LAYER_ROOT/config/agents.json" "$agent_layer_dir/config/agents.json"
  write_agent_config "$agent_layer_dir/config/agents.json" true true true true
  cp "$AGENT_LAYER_ROOT/setup.sh" "$agent_layer_dir/setup.sh"
  cp "$AGENT_LAYER_ROOT/dev/bootstrap.sh" "$agent_layer_dir/dev/bootstrap.sh"
  cp "$AGENT_LAYER_ROOT/dev/format.sh" "$agent_layer_dir/dev/format.sh"
  cp "$AGENT_LAYER_ROOT/.githooks/pre-commit" "$agent_layer_dir/.githooks/pre-commit"
  cp "$AGENT_LAYER_ROOT/with-env.sh" "$agent_layer_dir/with-env.sh"
  cp "$AGENT_LAYER_ROOT/run.sh" "$agent_layer_dir/run.sh"
  cp "$AGENT_LAYER_ROOT/check-updates.sh" "$agent_layer_dir/check-updates.sh"
  cp "$AGENT_LAYER_ROOT/agent-layer" "$agent_layer_dir/agent-layer"
  cp "$AGENT_LAYER_ROOT/clean.sh" "$agent_layer_dir/clean.sh"
  chmod +x "$agent_layer_dir/with-env.sh" "$agent_layer_dir/run.sh" \
    "$agent_layer_dir/check-updates.sh" "$agent_layer_dir/agent-layer" \
    "$agent_layer_dir/clean.sh" "$agent_layer_dir/setup.sh" \
    "$agent_layer_dir/dev/bootstrap.sh" "$agent_layer_dir/dev/format.sh" \
    "$agent_layer_dir/.githooks/pre-commit"
  : >"$agent_layer_dir/src/sync/sync.mjs"
  mkdir -p "$root/sub/dir"
  printf "%s" "$root"
}

# Create a parent root with full sync sources copied in.
create_sync_parent_root() {
  local root
  root="$(make_tmp_dir)"
  mkdir -p "$root/.agent-layer/src/lib" "$root/.agent-layer/config"
  cp -R "$AGENT_LAYER_ROOT/src/sync" "$root/.agent-layer/src/sync"
  cp "$AGENT_LAYER_ROOT/src/lib/agent-config.mjs" "$root/.agent-layer/src/lib/agent-config.mjs"
  cp -R "$AGENT_LAYER_ROOT/config/instructions" "$root/.agent-layer/config/instructions"
  cp -R "$AGENT_LAYER_ROOT/config/workflows" "$root/.agent-layer/config/workflows"
  mkdir -p "$root/.agent-layer/config/policy"
  cp "$AGENT_LAYER_ROOT/config/mcp-servers.json" "$root/.agent-layer/config/mcp-servers.json"
  cp "$AGENT_LAYER_ROOT/config/policy/commands.json" "$root/.agent-layer/config/policy/commands.json"
  cp "$AGENT_LAYER_ROOT/config/agents.json" "$root/.agent-layer/config/agents.json"
  write_agent_config "$root/.agent-layer/config/agents.json" true true true true
  mkdir -p "$root/sub/dir"
  printf "%s" "$root"
}
