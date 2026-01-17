#!/usr/bin/env bats

# Tests for the ./al launcher behavior.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

teardown() {
  cleanup_test_temp_dirs
}

# Helper: write a stub command that logs args to a file.
write_stub_logger() {
  local bin="$1" name="$2" log_path="$3"
  cat >"$bin/$name" << EOS
#!/usr/bin/env bash
printf "%s\n" "\$@" >> "$log_path"
exit 0
EOS
  chmod +x "$bin/$name"
}

# Helper: write a stub command that prints env vars.
write_stub_env_echo() {
  local bin="$1" name="$2"
  cat >"$bin/$name" << 'EOS'
#!/usr/bin/env bash
printf "%s|%s" "${AGENT_ENV:-}" "${PROJECT_ENV:-}"
exit 0
EOS
  chmod +x "$bin/$name"
}

# Test: ./al runs sync before launching the command.
@test "al runs sync before launch" {
  local root stub_bin sync_log
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  sync_log="$root/sync.log"

  mkdir -p "$stub_bin"
  write_stub_logger "$stub_bin" "probe" "$root/args.log"

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" SYNC_LOG="$sync_log" \
    "$root/.agent-layer/agent-layer" probe ok)"
  status=$?
  [ "$status" -eq 0 ]
  [ -f "$sync_log" ]

  run rg -n "\"parentRoot\"" "$sync_log"
  [ "$status" -eq 0 ]

  run rg -n "^ok$" "$root/args.log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: ./al does not load project env by default.
@test "al does not load project env by default" {
  local root stub_bin output
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"

  mkdir -p "$stub_bin"
  write_stub_env_echo "$stub_bin" "probe"

  printf "AGENT_ENV=agent\n" > "$root/.agent-layer/.env"
  printf "PROJECT_ENV=project\n" > "$root/.env"

  output="$(cd "$root/sub/dir" && PATH="$stub_bin:$PATH" PROJECT_ENV= \
    "$root/.agent-layer/agent-layer" probe)"
  status=$?
  [ "$status" -eq 0 ]
  [ "$output" = "agent|" ]

  rm -rf "$root"
}

# Test: ./al --no-sync skips sync before launching the command.
@test "al --no-sync skips sync before launch" {
  local root stub_bin sync_log
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  sync_log="$root/sync.log"

  mkdir -p "$stub_bin"
  write_stub_logger "$stub_bin" "probe" "$root/args.log"

  run bash -c "cd '$root/sub/dir' && PATH='$stub_bin:$PATH' SYNC_LOG='$sync_log' '$root/.agent-layer/agent-layer' --no-sync probe ok"
  [ "$status" -eq 0 ]
  [ ! -f "$sync_log" ]

  run rg -n "^ok$" "$root/args.log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: ./al blocks disabled agents.
@test "al blocks disabled agents" {
  local root stub_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"

  mkdir -p "$stub_bin"
  write_stub_logger "$stub_bin" "codex" "$root/args.log"

  write_agent_config "$root/.agent-layer/config/agents.json" true true false true

  run bash -c "cd '$root/sub/dir' && PATH='$stub_bin:$PATH' '$root/.agent-layer/agent-layer' codex 2>&1"
  [ "$status" -ne 0 ]
  [[ "$output" == *"codex is disabled"* ]]
  [ ! -f "$root/args.log" ]

  rm -rf "$root"
}

# Test: ./al applies default args without overriding user flags.
@test "al applies default args" {
  local root stub_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"

  mkdir -p "$stub_bin"
  write_stub_logger "$stub_bin" "codex" "$root/args.log"

  run bash -c "cd '$root/sub/dir' && PATH='$stub_bin:$PATH' '$root/.agent-layer/agent-layer' codex"
  [ "$status" -eq 0 ]
  run rg -n "^--model$" "$root/args.log"
  [ "$status" -eq 0 ]
  run rg -n "^gpt-5.2-codex$" "$root/args.log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: ./al keeps user flag values over defaults.
@test "al preserves user flags over defaults" {
  local root stub_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"

  mkdir -p "$stub_bin"
  write_stub_logger "$stub_bin" "codex" "$root/args.log"

  run bash -c "cd '$root/sub/dir' && PATH='$stub_bin:$PATH' '$root/.agent-layer/agent-layer' codex --model gpt-4"
  [ "$status" -eq 0 ]
  run rg -n "^--model$" "$root/args.log"
  [ "$status" -eq 0 ]
  run rg -n "^gpt-4$" "$root/args.log"
  [ "$status" -eq 0 ]
  run rg -n "^gpt-5.2-codex$" "$root/args.log"
  [ "$status" -ne 0 ]

  rm -rf "$root"
}
