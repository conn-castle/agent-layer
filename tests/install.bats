#!/usr/bin/env bats

# Tests for the installer and gitignore updates.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

teardown() {
  cleanup_test_temp_dirs
}

# Helper: create a minimal .agent-layer repo for installer tests.
create_min_agent_layer() {
  local root="$1"
  mkdir -p "$root/.agent-layer/src/lib" "$root/.agent-layer/src/sync" \
    "$root/.agent-layer/config/templates/docs" "$root/.agent-layer/config" \
    "$root/.agent-layer/src/mcp/agent-layer-prompts"
  printf "EXAMPLE=1\n" >"$root/.agent-layer/.env.example"
  cp "$AGENT_LAYER_ROOT/config/agents.json" "$root/.agent-layer/config/agents.json"
  cp "$AGENT_LAYER_ROOT/src/cli.mjs" "$root/.agent-layer/src/cli.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/agent-config.mjs" "$root/.agent-layer/src/lib/agent-config.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/cleanup.mjs" "$root/.agent-layer/src/lib/cleanup.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/env.mjs" "$root/.agent-layer/src/lib/env.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/generated-outputs.mjs" "$root/.agent-layer/src/lib/generated-outputs.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/install-config.mjs" "$root/.agent-layer/src/lib/install-config.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/launcher.mjs" "$root/.agent-layer/src/lib/launcher.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/roots.mjs" "$root/.agent-layer/src/lib/roots.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/setup.mjs" "$root/.agent-layer/src/lib/setup.mjs"
  cp "$AGENT_LAYER_ROOT/src/sync/utils.mjs" "$root/.agent-layer/src/sync/utils.mjs"
  write_stub_sync_mjs "$root/.agent-layer/src/sync/sync.mjs"
  cat >"$root/.agent-layer/src/mcp/agent-layer-prompts/package.json" <<'EOF'
{
  "name": "agent-layer-prompts",
  "version": "0.0.0"
}
EOF
  cp "$AGENT_LAYER_ROOT/config/templates/docs/"*.md "$root/.agent-layer/config/templates/docs/"
  cp "$AGENT_LAYER_ROOT/agent-layer" "$root/.agent-layer/agent-layer"
  chmod +x "$root/.agent-layer/agent-layer"
  git -C "$root/.agent-layer" init -q
}

# Helper: create a source repo to simulate cloning during install.
create_source_repo() {
  local repo="$1"
  mkdir -p "$repo/src/lib" "$repo/src/sync" "$repo/config/templates/docs" "$repo/config" \
    "$repo/src/mcp/agent-layer-prompts"
  printf "EXAMPLE=1\n" >"$repo/.env.example"
  cp "$AGENT_LAYER_ROOT/config/agents.json" "$repo/config/agents.json"
  cp "$AGENT_LAYER_ROOT/src/cli.mjs" "$repo/src/cli.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/agent-config.mjs" "$repo/src/lib/agent-config.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/cleanup.mjs" "$repo/src/lib/cleanup.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/env.mjs" "$repo/src/lib/env.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/generated-outputs.mjs" "$repo/src/lib/generated-outputs.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/install-config.mjs" "$repo/src/lib/install-config.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/launcher.mjs" "$repo/src/lib/launcher.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/roots.mjs" "$repo/src/lib/roots.mjs"
  cp "$AGENT_LAYER_ROOT/src/lib/setup.mjs" "$repo/src/lib/setup.mjs"
  cp "$AGENT_LAYER_ROOT/src/sync/utils.mjs" "$repo/src/sync/utils.mjs"
  write_stub_sync_mjs "$repo/src/sync/sync.mjs"
  cat >"$repo/src/mcp/agent-layer-prompts/package.json" <<'EOF'
{
  "name": "agent-layer-prompts",
  "version": "0.0.0"
}
EOF
  cp "$AGENT_LAYER_ROOT/config/templates/docs/"*.md "$repo/config/templates/docs/"
  cp "$AGENT_LAYER_ROOT/agent-layer" "$repo/agent-layer"
  chmod +x "$repo/agent-layer"
  git -C "$repo" init -q
  git -C "$repo" config user.email "test@example.com"
  git -C "$repo" config user.name "Test User"
  git -C "$repo" add .
  git -C "$repo" commit -m "init" -q
}

# Test: installer updates an existing agent-layer .gitignore block in place
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
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' < /dev/null"
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
  grep -q '^\.vscode/mcp\.json$' "$gitignore"
  grep -q '^\.vscode/prompts/$' "$gitignore"
  grep -q '^\.agent/workflows/$' "$gitignore"

  rm -rf "$root"
}

# Test: installer removes duplicate agent-layer blocks and keeps the first position
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
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' < /dev/null"
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

# Test: installer appends agent-layer block when missing
@test "installer appends agent-layer block when missing" {
  local root work stub_bin installer gitignore
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agent_layer "$work"

  printf "top\n" >"$work/.gitignore"
  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' < /dev/null"
  [ "$status" -eq 0 ]

  gitignore="$work/.gitignore"
  grep -q '^# >>> agent-layer$' "$gitignore"
  grep -q '^# <<< agent-layer$' "$gitignore"
  grep -q '^al$' "$gitignore"
  grep -q '^\.codex/$' "$gitignore"
  grep -q '^\.gemini/$' "$gitignore"
  grep -q '^\.claude/$' "$gitignore"
  grep -q '^\.vscode/mcp\.json$' "$gitignore"
  grep -q '^\.vscode/prompts/$' "$gitignore"
  grep -q '^\.agent/workflows/$' "$gitignore"

  rm -rf "$root"
}

# Test: installer errors when .agent-layer exists but is not a git repo
@test "installer errors when .agent-layer exists but is not a git repo" {
  local root work stub_bin installer
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work/.agent-layer"
  git -C "$work" init -q

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && GIT_CEILING_DIRECTORIES='$work' PATH='$stub_bin:$PATH' '$installer' < /dev/null"
  [ "$status" -ne 0 ]
  [[ "$output" == *".agent-layer exists but is not a git repo"* ]]

  rm -rf "$root"
}

# Test: installer leaves existing ./al without --force
@test "installer leaves existing ./al without --force" {
  local root work stub_bin installer
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agent_layer "$work"

  printf "original\n" >"$work/al"
  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' < /dev/null"
  [ "$status" -eq 0 ]
  [ "$(cat "$work/al")" = "original" ]

  rm -rf "$root"
}

# Test: installer overwrites ./al with --force
@test "installer overwrites ./al with --force" {
  local root work stub_bin installer
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agent_layer "$work"

  printf "original\n" >"$work/al"
  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' --force < /dev/null"
  [ "$status" -eq 0 ]
  grep -q '\.agent-layer/agent-layer' "$work/al"

  rm -rf "$root"
}

# Test: installer does not run sync after setup
@test "installer does not run sync after setup" {
  local root work stub_bin installer
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work"
  git -C "$work" init -q
  create_min_agent_layer "$work"

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' < /dev/null"
  [ "$status" -eq 0 ]
  [[ "$output" != *"==> Running sync"* ]]

  rm -rf "$root"
}

# Test: installer fails without git repo when non-interactive
@test "installer fails without git repo when non-interactive" {
  local root stub_bin installer tmp_base
  tmp_base="${BATS_TEST_TMPDIR:-/tmp}"
  mkdir -p "$tmp_base"
  root="$(mktemp -d "${tmp_base%/}/agent-layer-nogit.XXXXXX")"
  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$root' && unset GIT_DIR GIT_WORK_TREE && GIT_CEILING_DIRECTORIES='$root' PATH='$stub_bin:$PATH' '$installer' < /dev/null"
  [ "$status" -ne 0 ]
  [[ "$output" == *"Not a git repo and no TTY available to confirm."* ]]

  rm -rf "$root"
}

# Test: installer clones from local repo when .agent-layer is missing
@test "installer clones from local repo when .agent-layer is missing" {
  local root work src stub_bin installer tag
  root="$(make_tmp_dir)"
  work="$root/work"
  src="$root/src"
  mkdir -p "$work" "$src"
  git -C "$work" init -q
  create_source_repo "$src"
  git -C "$src" tag v0.1.0

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' --repo-url '$src' < /dev/null"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Next steps (required):"* ]]
  [[ "$output" == *".env.example"* ]]
  [[ "$output" == *".agent-layer/config/agents.json"* ]]
  [[ "$output" == *".agent-layer/config/mcp-servers.json"* ]]
  [[ "$output" == *"./al --setup"* ]]
  [[ "$output" == *"./al gemini"* ]]

  [ -d "$work/.agent-layer/.git" ]
  [ -f "$work/.agent-layer/.env" ]
  grep -q '^# >>> agent-layer$' "$work/.gitignore"
  [ -f "$work/docs/ISSUES.md" ]
  [ -f "$work/docs/FEATURES.md" ]
  [ -f "$work/docs/ROADMAP.md" ]
  [ -f "$work/docs/DECISIONS.md" ]
  [ -f "$work/docs/COMMANDS.md" ]
  tag="$(git -C "$work/.agent-layer" describe --tags --exact-match)"
  [ "$tag" = "v0.1.0" ]
  cmp -s "$src/config/templates/docs/ISSUES.md" "$work/docs/ISSUES.md"
  cmp -s "$src/config/templates/docs/FEATURES.md" "$work/docs/FEATURES.md"
  cmp -s "$src/config/templates/docs/ROADMAP.md" "$work/docs/ROADMAP.md"
  cmp -s "$src/config/templates/docs/DECISIONS.md" "$work/docs/DECISIONS.md"
  cmp -s "$src/config/templates/docs/COMMANDS.md" "$work/docs/COMMANDS.md"

  rm -rf "$root"
}

# Test: installer installs a specific tag with --version
@test "installer installs a specific tag with --version" {
  local root work src stub_bin installer tag
  root="$(make_tmp_dir)"
  work="$root/work"
  src="$root/src"
  mkdir -p "$work" "$src"
  git -C "$work" init -q
  create_source_repo "$src"
  git -C "$src" tag v0.1.0

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' --version v0.1.0 --repo-url '$src' < /dev/null"
  [ "$status" -eq 0 ]
  tag="$(git -C "$work/.agent-layer" describe --tags --exact-match)"
  [ "$tag" = "v0.1.0" ]

  rm -rf "$root"
}

# Test: installer errors when --version tag is missing
@test "installer errors when --version tag is missing" {
  local root work src stub_bin installer
  root="$(make_tmp_dir)"
  work="$root/work"
  src="$root/src"
  mkdir -p "$work" "$src"
  git -C "$work" init -q
  create_source_repo "$src"
  git -C "$src" tag v0.1.0

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' --version v9.9.9 --repo-url '$src' < /dev/null"
  [ "$status" -ne 0 ]
  [[ "$output" == *"Tag 'v9.9.9' not found"* ]]
  [ ! -e "$work/.agent-layer" ]

  rm -rf "$root"
}

# Test: installer keeps existing docs without prompt in non-interactive mode
@test "installer keeps existing docs without prompt in non-interactive mode" {
  local root work stub_bin installer
  root="$(make_tmp_dir)"
  work="$root/work"
  mkdir -p "$work/docs"
  git -C "$work" init -q
  create_min_agent_layer "$work"

  printf "custom\n" >"$work/docs/ISSUES.md"

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' < /dev/null"
  [ "$status" -eq 0 ]
  [ "$(cat "$work/docs/ISSUES.md")" = "custom" ]
  [ -f "$work/docs/FEATURES.md" ]
  [ -f "$work/docs/ROADMAP.md" ]
  [ -f "$work/docs/DECISIONS.md" ]
  [ -f "$work/docs/COMMANDS.md" ]

  rm -rf "$root"
}

# Test: installer upgrades .agent-layer to the latest tag with --upgrade
@test "installer upgrades .agent-layer to the latest tag with --upgrade" {
  local root work src stub_bin installer tag
  root="$(make_tmp_dir)"
  work="$root/work"
  src="$root/src"
  mkdir -p "$work" "$src"
  git -C "$work" init -q
  create_source_repo "$src"
  git -C "$src" tag v0.1.0

  git clone "$src" "$work/.agent-layer" >/dev/null

  printf "change\n" >"$src/CHANGELOG.md"
  git -C "$src" add CHANGELOG.md
  git -C "$src" commit -m "release v0.2.0" -q
  git -C "$src" tag v0.2.0

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' --upgrade --repo-url '$src' < /dev/null"
  [ "$status" -eq 0 ]
  tag="$(git -C "$work/.agent-layer" describe --tags --exact-match)"
  [ "$tag" = "v0.2.0" ]
  [[ "$output" == *"Changes since"* ]]

  rm -rf "$root"
}

# Test: installer preserves user config during upgrade without --force
@test "installer preserves user config during --upgrade" {
  local root work src stub_bin installer tag
  root="$(make_tmp_dir)"
  work="$root/work"
  src="$root/src"
  mkdir -p "$work" "$src"
  git -C "$work" init -q
  create_source_repo "$src"
  git -C "$src" tag v0.1.0

  git clone "$src" "$work/.agent-layer" >/dev/null

  cat >"$work/.agent-layer/config/agents.json" <<'EOF'
{
  "gemini": { "enabled": false },
  "claude": { "enabled": false },
  "codex": { "enabled": false },
  "vscode": { "enabled": false }
}
EOF

  printf "release\n" >"$src/CHANGELOG.md"
  cat >"$src/config/agents.json" <<'EOF'
{
  "gemini": { "enabled": true },
  "claude": { "enabled": true },
  "codex": { "enabled": true },
  "vscode": { "enabled": true }
}
EOF
  git -C "$src" add CHANGELOG.md config/agents.json
  git -C "$src" commit -m "release v0.2.0" -q
  git -C "$src" tag v0.2.0

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' --upgrade --repo-url '$src' < /dev/null"
  [ "$status" -eq 0 ]
  tag="$(git -C "$work/.agent-layer" describe --tags --exact-match)"
  [ "$tag" = "v0.2.0" ]
  run rg -n "\"gemini\": \\{ \"enabled\": false \\}" "$work/.agent-layer/config/agents.json"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: installer updates .agent-layer to the latest branch commit with --latest-branch
@test "installer updates .agent-layer to the latest branch commit with --latest-branch" {
  local root work src stub_bin installer dev_commit head_ref
  root="$(make_tmp_dir)"
  work="$root/work"
  src="$root/src"
  mkdir -p "$work" "$src"
  git -C "$work" init -q
  create_source_repo "$src"

  git -C "$src" checkout -b dev -q
  printf "dev\n" >"$src/DEV.md"
  git -C "$src" add DEV.md
  git -C "$src" commit -m "dev commit" -q
  dev_commit="$(git -C "$src" rev-parse --short dev)"

  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$work' && PATH='$stub_bin:$PATH' '$installer' --latest-branch dev --repo-url '$src' < /dev/null"
  [ "$status" -eq 0 ]
  [ "$(git -C "$work/.agent-layer" rev-parse --short HEAD)" = "$dev_commit" ]
  head_ref="$(git -C "$work/.agent-layer" symbolic-ref --short -q HEAD || true)"
  [ -z "$head_ref" ]

  rm -rf "$root"
}

# Test: installer errors when --repo-url is missing a value
@test "installer errors when --repo-url is missing a value" {
  local root stub_bin installer
  root="$(make_tmp_dir)"
  stub_bin="$(create_stub_tools "$root")"
  installer="$AGENT_LAYER_ROOT/agent-layer-install.sh"
  run bash -c "cd '$root' && PATH='$stub_bin:$PATH' '$installer' --repo-url < /dev/null"
  [ "$status" -ne 0 ]
  [[ "$output" == *"--repo-url requires a value"* ]]

  rm -rf "$root"
}
