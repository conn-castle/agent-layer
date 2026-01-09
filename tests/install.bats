#!/usr/bin/env bats

load "helpers.bash"

create_min_agent_layer() {
  local root="$1"
  mkdir -p "$root/.agent-layer/sync"
  cat >"$root/.agent-layer/setup.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exit 0
EOF
  chmod +x "$root/.agent-layer/setup.sh"
  printf "EXAMPLE=1\n" >"$root/.agent-layer/.env.example"
  : >"$root/.agent-layer/sync/sync.mjs"
  git -C "$root/.agent-layer" init -q
}

create_source_repo() {
  local repo="$1"
  mkdir -p "$repo/sync"
  cat >"$repo/setup.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exit 0
EOF
  chmod +x "$repo/setup.sh"
  printf "EXAMPLE=1\n" >"$repo/.env.example"
  : >"$repo/sync/sync.mjs"
  git -C "$repo" init -q
  git -C "$repo" config user.email "test@example.com"
  git -C "$repo" config user.name "Test User"
  git -C "$repo" add .
  git -C "$repo" commit -m "init" -q
}

@test "installer updates an existing agent-layer .gitignore block in place" {
  local root work stub_bin installer gitignore
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agent_layer "$work"

  cat >"$work/.gitignore" <<'EOF'
start

# >>> agent-layer
old
# <<< agent-layer

end
EOF

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer'"
  [ "$status" -eq 0 ]

  gitignore="$work/.gitignore"
  start_line="$(grep -n '^start$' "$gitignore" | cut -d: -f1)"
  block_start="$(grep -n '^# >>> agent-layer$' "$gitignore" | cut -d: -f1)"
  block_end="$(grep -n '^# <<< agent-layer$' "$gitignore" | cut -d: -f1)"
  end_line="$(grep -n '^end$' "$gitignore" | cut -d: -f1)"

  [ "$start_line" -lt "$block_start" ]
  [ "$block_end" -lt "$end_line" ]
  grep -q '^al$' "$gitignore"
  grep -q '^\.codex/$' "$gitignore"
  grep -q '^\.gemini/$' "$gitignore"
  grep -q '^\.claude/$' "$gitignore"
  grep -q '^\.vscode/$' "$gitignore"

  rm -rf "$root"
}

@test "installer removes duplicate agent-layer blocks and keeps the first position" {
  local root work stub_bin installer gitignore
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agent_layer "$work"

  cat >"$work/.gitignore" <<'EOF'
top

# >>> agent-layer
old-one
# <<< agent-layer

middle

# >>> agent-layer
old-two
# <<< agent-layer

bottom
EOF

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer'"
  [ "$status" -eq 0 ]

  gitignore="$work/.gitignore"
  top_line="$(grep -n '^top$' "$gitignore" | cut -d: -f1)"
  middle_line="$(grep -n '^middle$' "$gitignore" | cut -d: -f1)"
  bottom_line="$(grep -n '^bottom$' "$gitignore" | cut -d: -f1)"
  block_start="$(grep -n '^# >>> agent-layer$' "$gitignore" | cut -d: -f1)"
  block_end="$(grep -n '^# <<< agent-layer$' "$gitignore" | cut -d: -f1)"
  block_count="$(grep -c '^# >>> agent-layer$' "$gitignore")"

  [ "$block_count" -eq 1 ]
  [ "$top_line" -lt "$block_start" ]
  [ "$block_end" -lt "$middle_line" ]
  [ "$middle_line" -lt "$bottom_line" ]
  grep -q '^al$' "$gitignore"

  rm -rf "$root"
}

@test "installer appends agent-layer block when missing" {
  local root work stub_bin installer gitignore
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agent_layer "$work"

  printf "top\n" >"$work/.gitignore"
  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer'"
  [ "$status" -eq 0 ]

  gitignore="$work/.gitignore"
  grep -q '^# >>> agent-layer$' "$gitignore"
  grep -q '^# <<< agent-layer$' "$gitignore"
  grep -q '^al$' "$gitignore"
  grep -q '^\.codex/$' "$gitignore"
  grep -q '^\.gemini/$' "$gitignore"
  grep -q '^\.claude/$' "$gitignore"
  grep -q '^\.vscode/$' "$gitignore"

  rm -rf "$root"
}

@test "installer errors when .agent-layer exists but is not a git repo" {
  local root work stub_bin installer
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work/.agent-layer"
  git -C "$work" init -q

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer'"
  [ "$status" -ne 0 ]
  [[ "$output" == *".agent-layer exists but is not a git repo"* ]]

  rm -rf "$root"
}

@test "installer leaves existing ./al without --force" {
  local root work stub_bin installer
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agent_layer "$work"

  printf "original\n" >"$work/al"
  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer'"
  [ "$status" -eq 0 ]
  [ "$(cat "$work/al")" = "original" ]

  rm -rf "$root"
}

@test "installer overwrites ./al with --force" {
  local root work stub_bin installer
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agent_layer "$work"

  printf "original\n" >"$work/al"
  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' --force"
  [ "$status" -eq 0 ]
  grep -q '\.agent-layer/al' "$work/al"

  rm -rf "$root"
}

@test "installer fails without git repo when non-interactive" {
  local root stub_bin installer
  root="$(make_tmp_dir)"
  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$root' && PATH='$stub_bin:$PATH' '$installer'"
  [ "$status" -ne 0 ]
  [[ "$output" == *"Not a git repo and no TTY available to confirm."* ]]

  rm -rf "$root"
}

@test "installer clones from local repo when .agent-layer is missing" {
  local root work src stub_bin installer
  root="$(make_tmp_dir)"
  work="$root/work"
  src="$root/src"
  mkdir -p "$work" "$src"
  git -C "$work" init -q
  create_source_repo "$src"

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' --repo-url '$src'"
  [ "$status" -eq 0 ]

  [ -d "$work/.agent-layer/.git" ]
  [ -f "$work/.agent-layer/.env" ]
  grep -q '^# >>> agent-layer$' "$work/.gitignore"

  rm -rf "$root"
}

@test "installer errors when --repo-url is missing a value" {
  local root stub_bin installer
  root="$(make_tmp_dir)"
  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$root' && PATH='$stub_bin:$PATH' '$installer' --repo-url"
  [ "$status" -ne 0 ]
  [[ "$output" == *"--repo-url requires a value"* ]]

  rm -rf "$root"
}
