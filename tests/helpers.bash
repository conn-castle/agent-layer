AGENTLAYER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

make_tmp_dir() {
  local base
  base="$AGENTLAYER_ROOT/tmp"
  mkdir -p "$base"
  mktemp -d "$base/agentlayer-test.XXXXXX"
}

create_working_root() {
  local root
  root="$(make_tmp_dir)"
  ln -s "$AGENTLAYER_ROOT" "$root/.agentlayer"
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
  local root agentlayer
  root="$(make_tmp_dir)"
  agentlayer="$root/.agentlayer"
  mkdir -p "$agentlayer/lib" "$agentlayer/sync"
  cp "$AGENTLAYER_ROOT/lib/paths.sh" "$agentlayer/lib/paths.sh"
  cp "$AGENTLAYER_ROOT/with-env.sh" "$agentlayer/with-env.sh"
  cp "$AGENTLAYER_ROOT/al" "$agentlayer/al"
  cp "$AGENTLAYER_ROOT/clean.sh" "$agentlayer/clean.sh"
  chmod +x "$agentlayer/with-env.sh" "$agentlayer/al" "$agentlayer/clean.sh"
  : >"$agentlayer/sync/sync.mjs"
  mkdir -p "$root/sub/dir"
  printf "%s" "$root"
}
