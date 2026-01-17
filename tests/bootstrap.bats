#!/usr/bin/env bats

# Tests for the developer bootstrap flow.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

teardown() {
  cleanup_test_temp_dirs
}

# Helper: write a stub command that exits successfully.
write_stub_cmd() {
  local bin="$1" name="$2"
  cat >"$bin/$name" <<'CMD'
#!/usr/bin/env bash
exit 0
CMD
  chmod +x "$bin/$name"
}

# Test: bootstrap fails without TTY unless --yes
@test "bootstrap fails without TTY unless --yes" {
  local root bash_bin
  root="$(create_isolated_parent_root)"
  bash_bin="$(command -v bash)"

  run "$bash_bin" -c "cd '$root' && '$root/.agent-layer/dev/bootstrap.sh' --temp-parent-root < /dev/null 2>&1"
  [ "$status" -ne 0 ]
  [[ "$output" == *"No TTY available"* ]]

  rm -rf "$root"
}

# Test: bootstrap requires a parent root choice
@test "bootstrap requires parent root choice" {
  local root bash_bin
  root="$(create_isolated_parent_root)"
  bash_bin="$(command -v bash)"

  run "$bash_bin" -c "cd '$root' && '$root/.agent-layer/dev/bootstrap.sh' --yes"
  [ "$status" -ne 0 ]
  [[ "$output" == *"dev bootstrap requires a parent root target"* ]]

  rm -rf "$root"
}

# Test: bootstrap --yes runs setup and enables hooks
@test "bootstrap --yes runs setup and enables hooks" {
  local root bash_bin stub_bin setup_log git_bin rg_bin
  root="$(create_isolated_parent_root)"
  bash_bin="$(command -v bash)"
  stub_bin="$root/stub-bin"
  setup_log="$root/setup.log"
  git_bin="$(dirname "$(command -v git)")"
  rg_bin="$(dirname "$(command -v rg)")"

  git -C "$root" init -q

  cat >"$root/.agent-layer/agent-layer" <<STUB
#!/usr/bin/env bash
printf '%s\n' "\$@" > "$setup_log"
exit 0
STUB
  chmod +x "$root/.agent-layer/agent-layer"

  mkdir -p "$root/.agent-layer/node_modules/.bin"
  cat >"$root/.agent-layer/node_modules/.bin/prettier" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$root/.agent-layer/node_modules/.bin/prettier"

  mkdir -p "$stub_bin"
  write_stub_cmd "$stub_bin" "node"
  write_stub_cmd "$stub_bin" "npm"
  write_stub_cmd "$stub_bin" "bats"
  write_stub_cmd "$stub_bin" "shfmt"
  write_stub_cmd "$stub_bin" "shellcheck"

  run "$bash_bin" -c "cd '$root' && PATH='$stub_bin:$git_bin:$rg_bin:/usr/bin:/bin' '$root/.agent-layer/dev/bootstrap.sh' --yes --parent-root '$root'"
  [ "$status" -eq 0 ]

  run rg -n -- "--skip-checks" "$setup_log"
  [ "$status" -eq 0 ]
  run rg -n -- "--parent-root" "$setup_log"
  [ "$status" -eq 0 ]

  run git -C "$root" config core.hooksPath
  [ "$status" -eq 0 ]
  [ "$output" = ".agent-layer/.githooks" ]
  [ -x "$root/.agent-layer/.githooks/pre-commit" ]

  rm -rf "$root"
}
