# Shared test helpers for Bats suites.
# Keep helpers deterministic and side-effect free outside temp directories.

# Resolve the agent-layer root for test fixtures.
AGENTLAYER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Create a temporary directory under .agent-layer/tmp.
make_tmp_dir() {
  local base
  base="$AGENTLAYER_ROOT/tmp"
  mkdir -p "$base"
  mktemp -d "$base/agent-layer-test.XXXXXX"
}

# Create a working repo root that symlinks the real .agent-layer.
create_working_root() {
  local root
  root="$(make_tmp_dir)"
  ln -s "$AGENTLAYER_ROOT" "$root/.agent-layer"
  mkdir -p "$root/sub/dir"
  printf "%s" "$root"
}

# Create a stub node binary that exits successfully.
create_stub_node() {
  local root="$1"
  local bin="$root/stub-bin"
  mkdir -p "$bin"
  cat >"$bin/node" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$bin/node"
  printf "%s" "$bin"
}

# Create stub node and npm binaries that exit successfully.
create_stub_tools() {
  local root="$1"
  local bin="$root/stub-bin"
  mkdir -p "$bin"
  cat >"$bin/node" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  cat >"$bin/npm" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$bin/node" "$bin/npm"
  printf "%s" "$bin"
}

# Build an isolated working root with copied agent-layer scripts.
create_isolated_working_root() {
  local root agent_layer_dir
  root="$(make_tmp_dir)"
  agent_layer_dir="$root/.agent-layer"
  mkdir -p "$agent_layer_dir/src/lib" "$agent_layer_dir/src/sync" "$agent_layer_dir/dev" \
    "$agent_layer_dir/.githooks" "$agent_layer_dir/tests"
  cp "$AGENTLAYER_ROOT/src/lib/discover-root.sh" "$agent_layer_dir/src/lib/discover-root.sh"
  cp "$AGENTLAYER_ROOT/src/lib/entrypoint.sh" "$agent_layer_dir/src/lib/entrypoint.sh"
  cp "$AGENTLAYER_ROOT/src/lib/temp-work-root.sh" "$agent_layer_dir/src/lib/temp-work-root.sh"
  cp "$AGENTLAYER_ROOT/src/sync/utils.mjs" "$agent_layer_dir/src/sync/utils.mjs"
  cp "$AGENTLAYER_ROOT/src/sync/paths.mjs" "$agent_layer_dir/src/sync/paths.mjs"
  cp "$AGENTLAYER_ROOT/src/sync/policy.mjs" "$agent_layer_dir/src/sync/policy.mjs"
  cp "$AGENTLAYER_ROOT/src/sync/clean.mjs" "$agent_layer_dir/src/sync/clean.mjs"
  cp "$AGENTLAYER_ROOT/setup.sh" "$agent_layer_dir/setup.sh"
  cp "$AGENTLAYER_ROOT/dev/bootstrap.sh" "$agent_layer_dir/dev/bootstrap.sh"
  cp "$AGENTLAYER_ROOT/dev/format.sh" "$agent_layer_dir/dev/format.sh"
  cp "$AGENTLAYER_ROOT/.githooks/pre-commit" "$agent_layer_dir/.githooks/pre-commit"
  cp "$AGENTLAYER_ROOT/with-env.sh" "$agent_layer_dir/with-env.sh"
  cp "$AGENTLAYER_ROOT/run.sh" "$agent_layer_dir/run.sh"
  cp "$AGENTLAYER_ROOT/check-updates.sh" "$agent_layer_dir/check-updates.sh"
  cp "$AGENTLAYER_ROOT/al" "$agent_layer_dir/al"
  cp "$AGENTLAYER_ROOT/clean.sh" "$agent_layer_dir/clean.sh"
  chmod +x "$agent_layer_dir/with-env.sh" "$agent_layer_dir/run.sh" \
    "$agent_layer_dir/check-updates.sh" "$agent_layer_dir/al" \
    "$agent_layer_dir/clean.sh" "$agent_layer_dir/setup.sh" \
    "$agent_layer_dir/dev/bootstrap.sh" "$agent_layer_dir/dev/format.sh" \
    "$agent_layer_dir/.githooks/pre-commit"
  : >"$agent_layer_dir/src/sync/sync.mjs"
  mkdir -p "$root/sub/dir"
  printf "%s" "$root"
}

# Create a working root with full sync sources copied in.
create_sync_working_root() {
  local root
  root="$(make_tmp_dir)"
  mkdir -p "$root/.agent-layer/src" "$root/.agent-layer/config"
  cp -R "$AGENTLAYER_ROOT/src/sync" "$root/.agent-layer/src/sync"
  cp -R "$AGENTLAYER_ROOT/config/instructions" "$root/.agent-layer/config/instructions"
  cp -R "$AGENTLAYER_ROOT/config/workflows" "$root/.agent-layer/config/workflows"
  mkdir -p "$root/.agent-layer/config/policy"
  cp "$AGENTLAYER_ROOT/config/mcp-servers.json" "$root/.agent-layer/config/mcp-servers.json"
  cp "$AGENTLAYER_ROOT/config/policy/commands.json" "$root/.agent-layer/config/policy/commands.json"
  mkdir -p "$root/sub/dir"
  printf "%s" "$root"
}
