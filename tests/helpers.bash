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
  mkdir -p "$agent_layer_dir/lib" "$agent_layer_dir/sync"
  cp "$AGENTLAYER_ROOT/lib/paths.sh" "$agent_layer_dir/lib/paths.sh"
  cp "$AGENTLAYER_ROOT/with-env.sh" "$agent_layer_dir/with-env.sh"
  cp "$AGENTLAYER_ROOT/al" "$agent_layer_dir/al"
  cp "$AGENTLAYER_ROOT/clean.sh" "$agent_layer_dir/clean.sh"
  chmod +x "$agent_layer_dir/with-env.sh" "$agent_layer_dir/al" "$agent_layer_dir/clean.sh"
  : >"$agent_layer_dir/sync/sync.mjs"
  mkdir -p "$root/sub/dir"
  printf "%s" "$root"
}
