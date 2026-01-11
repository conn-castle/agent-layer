AGENTLAYER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

make_tmp_dir() {
  local base
  base="$AGENTLAYER_ROOT/tmp"
  mkdir -p "$base"
  mktemp -d "$base/agent-layer-test.XXXXXX"
}

create_working_root() {
  local root
  root="$(make_tmp_dir)"
  ln -s "$AGENTLAYER_ROOT" "$root/.agent-layer"
  mkdir -p "$root/sub/dir"
  printf "%s" "$root"
}

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

create_isolated_working_root() {
  local root agent_layer_dir
  root="$(make_tmp_dir)"
  agent_layer_dir="$root/.agent-layer"
  mkdir -p "$agent_layer_dir/src/lib" "$agent_layer_dir/src/sync"
  cp "$AGENTLAYER_ROOT/src/lib/paths.sh" "$agent_layer_dir/src/lib/paths.sh"
  cp "$AGENTLAYER_ROOT/src/lib/entrypoint.sh" "$agent_layer_dir/src/lib/entrypoint.sh"
  cp "$AGENTLAYER_ROOT/src/sync/utils.mjs" "$agent_layer_dir/src/sync/utils.mjs"
  cp "$AGENTLAYER_ROOT/src/sync/paths.mjs" "$agent_layer_dir/src/sync/paths.mjs"
  cp "$AGENTLAYER_ROOT/src/sync/policy.mjs" "$agent_layer_dir/src/sync/policy.mjs"
  cp "$AGENTLAYER_ROOT/src/sync/clean.mjs" "$agent_layer_dir/src/sync/clean.mjs"
  cp "$AGENTLAYER_ROOT/with-env.sh" "$agent_layer_dir/with-env.sh"
  cp "$AGENTLAYER_ROOT/run.sh" "$agent_layer_dir/run.sh"
  cp "$AGENTLAYER_ROOT/check-updates.sh" "$agent_layer_dir/check-updates.sh"
  cp "$AGENTLAYER_ROOT/al" "$agent_layer_dir/al"
  cp "$AGENTLAYER_ROOT/clean.sh" "$agent_layer_dir/clean.sh"
  chmod +x "$agent_layer_dir/with-env.sh" "$agent_layer_dir/run.sh" \
    "$agent_layer_dir/check-updates.sh" "$agent_layer_dir/al" \
    "$agent_layer_dir/clean.sh"
  : >"$agent_layer_dir/src/sync/sync.mjs"
  mkdir -p "$root/sub/dir"
  printf "%s" "$root"
}

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
