#!/usr/bin/env bats

load "helpers.bash"

create_min_agentlayer() {
  local root="$1"
  mkdir -p "$root/.agentlayer/sync"
  cat >"$root/.agentlayer/setup.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exit 0
EOF
  chmod +x "$root/.agentlayer/setup.sh"
  printf "EXAMPLE=1\n" >"$root/.agentlayer/.env.example"
  : >"$root/.agentlayer/sync/sync.mjs"
  git -C "$root/.agentlayer" init -q
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

@test "installer updates an existing agentlayer .gitignore block in place" {
  local root work stub_bin installer gitignore
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agentlayer "$work"

  cat >"$work/.gitignore" <<'EOF'
start

# >>> agentlayer
old
# <<< agentlayer

end
EOF

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer'"
  [ "$status" -eq 0 ]

  gitignore="$work/.gitignore"
  start_line="$(grep -n '^start$' "$gitignore" | cut -d: -f1)"
  block_start="$(grep -n '^# >>> agentlayer$' "$gitignore" | cut -d: -f1)"
  block_end="$(grep -n '^# <<< agentlayer$' "$gitignore" | cut -d: -f1)"
  end_line="$(grep -n '^end$' "$gitignore" | cut -d: -f1)"

  [ "$start_line" -lt "$block_start" ]
  [ "$block_end" -lt "$end_line" ]
  grep -q '^al$' "$gitignore"
  ! grep -q '^\.vscode/settings\.json$' "$gitignore"

  rm -rf "$root"
}

@test "installer removes duplicate agentlayer blocks and keeps the first position" {
  local root work stub_bin installer gitignore
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agentlayer "$work"

  cat >"$work/.gitignore" <<'EOF'
top

# >>> agentlayer
old-one
# <<< agentlayer

middle

# >>> agentlayer
old-two
# <<< agentlayer

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
  block_start="$(grep -n '^# >>> agentlayer$' "$gitignore" | cut -d: -f1)"
  block_end="$(grep -n '^# <<< agentlayer$' "$gitignore" | cut -d: -f1)"
  block_count="$(grep -c '^# >>> agentlayer$' "$gitignore")"

  [ "$block_count" -eq 1 ]
  [ "$top_line" -lt "$block_start" ]
  [ "$block_end" -lt "$middle_line" ]
  [ "$middle_line" -lt "$bottom_line" ]
  grep -q '^al$' "$gitignore"

  rm -rf "$root"
}

@test "installer appends agentlayer block when missing" {
  local root work stub_bin installer gitignore
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agentlayer "$work"

  printf "top\n" >"$work/.gitignore"
  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer'"
  [ "$status" -eq 0 ]

  gitignore="$work/.gitignore"
  grep -q '^# >>> agentlayer$' "$gitignore"
  grep -q '^# <<< agentlayer$' "$gitignore"
  grep -q '^al$' "$gitignore"
  ! grep -q '^\.vscode/settings\.json$' "$gitignore"

  rm -rf "$root"
}

@test "installer errors when .agentlayer exists but is not a git repo" {
  local root work stub_bin installer
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work/.agentlayer"
  git -C "$work" init -q

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer'"
  [ "$status" -ne 0 ]
  [[ "$output" == *".agentlayer exists but is not a git repo"* ]]

  rm -rf "$root"
}

@test "installer leaves existing ./al without --force" {
  local root work stub_bin installer
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agentlayer "$work"

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
  create_min_agentlayer "$work"

  printf "original\n" >"$work/al"
  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENTLAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' --force"
  [ "$status" -eq 0 ]
  grep -q '\.agentlayer/al' "$work/al"

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

@test "installer clones from local repo when .agentlayer is missing" {
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

  [ -d "$work/.agentlayer/.git" ]
  [ -f "$work/.agentlayer/.env" ]
  grep -q '^# >>> agentlayer$' "$work/.gitignore"

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
