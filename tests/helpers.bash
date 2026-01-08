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
