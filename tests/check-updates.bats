#!/usr/bin/env bats

# Tests for the local tag update warning script.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Test: check-updates.sh exits quietly when git is unavailable
@test "check-updates.sh exits quietly when git is unavailable" {
  local root stub_bin bash_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  bash_bin="$(command -v bash)"

  mkdir -p "$stub_bin"
  cat >"$stub_bin/git" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
  chmod +x "$stub_bin/git"

  run "$bash_bin" -c "PATH='$stub_bin:/usr/bin:/bin' '$bash_bin' '$root/.agent-layer/check-updates.sh' 2>&1"
  [ "$status" -eq 0 ]
  [ -z "$output" ]

  rm -rf "$root"
}

# Test: check-updates.sh warns when local tag is behind
@test "check-updates.sh warns when local tag is behind" {
  local root bash_bin
  root="$(create_isolated_parent_root)"
  bash_bin="$(command -v bash)"

  git -C "$root/.agent-layer" init -q
  git -C "$root/.agent-layer" config user.email "test@example.com"
  git -C "$root/.agent-layer" config user.name "Test User"

  printf "one\n" >"$root/.agent-layer/version.txt"
  git -C "$root/.agent-layer" add version.txt
  git -C "$root/.agent-layer" commit -m "v1" -q
  git -C "$root/.agent-layer" tag v1.0.0

  printf "two\n" >"$root/.agent-layer/version.txt"
  git -C "$root/.agent-layer" add version.txt
  git -C "$root/.agent-layer" commit -m "v1.1" -q
  git -C "$root/.agent-layer" tag v1.1.0

  git -C "$root/.agent-layer" checkout -q v1.0.0

  run "$bash_bin" -c "cd '$root/.agent-layer' && ./check-updates.sh 2>&1"
  [ "$status" -eq 0 ]
  [[ "$output" == *"latest local tag is v1.1.0"* ]]
  [[ "$output" == *"agent-layer-install.sh --upgrade"* ]]

  rm -rf "$root"
}
