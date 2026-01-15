#!/usr/bin/env bats

# Tests for the internal run.sh launcher behavior.
# Load shared helpers for temp roots and stub binaries.
load "helpers.bash"

# Helper: write a stub node binary that logs args and simulates --check failures.
write_stub_node() {
  local bin="$1"
  cat >"$bin/node" <<'NODE'
#!/usr/bin/env bash
printf "%s\n" "$*" >> "${NODE_ARGS_LOG:?}"
if [[ " $* " == *" --print-shell "* ]]; then
  printf "enabled=%s\n" "${AGENT_CONFIG_ENABLED:-true}"
  if [[ -n "${AGENT_CONFIG_DEFAULT_ARGS:-}" ]]; then
    while IFS= read -r arg; do
      if [[ -n "$arg" ]]; then
        printf "defaultArg=%s\n" "$arg"
      fi
    done <<< "${AGENT_CONFIG_DEFAULT_ARGS}"
  fi
  exit 0
fi
if [[ "${NODE_FAIL_CHECK:-}" == "1" && " $* " == *" --check "* ]]; then
  exit 1
fi
exit 0
NODE
  chmod +x "$bin/node"
}

# Test: run.sh sync-env runs sync and with-env
@test "run.sh sync-env runs sync and with-env" {
  local root stub_bin node_log env_log bash_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  node_log="$root/node-args.log"
  env_log="$root/env-args.log"
  bash_bin="$(command -v bash)"

  mkdir -p "$stub_bin"
  write_stub_node "$stub_bin"

  cat >"$root/.agent-layer/with-env.sh" <<'EOF'
#!/usr/bin/env bash
printf "%s\n" "$@" > "$ENV_LOG"
exit 0
EOF
  chmod +x "$root/.agent-layer/with-env.sh"

  run "$bash_bin" -c "cd '$root/sub/dir' && NODE_ARGS_LOG='$node_log' ENV_LOG='$env_log' PATH='$stub_bin:/usr/bin:/bin' '$root/.agent-layer/run.sh' echo ok"
  [ "$status" -eq 0 ]

  run rg -n "sync.mjs" "$node_log"
  [ "$status" -eq 0 ]
  run rg -n "^echo$" "$env_log"
  [ "$status" -eq 0 ]
  run rg -n "^ok$" "$env_log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: run.sh --env-only skips sync
@test "run.sh --env-only skips sync" {
  local root stub_bin node_log env_log bash_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  node_log="$root/node-args.log"
  env_log="$root/env-args.log"
  bash_bin="$(command -v bash)"

  mkdir -p "$stub_bin"
  write_stub_node "$stub_bin"

  cat >"$root/.agent-layer/with-env.sh" <<'EOF'
#!/usr/bin/env bash
printf "%s\n" "$@" > "$ENV_LOG"
exit 0
EOF
  chmod +x "$root/.agent-layer/with-env.sh"

  run "$bash_bin" -c "cd '$root/sub/dir' && NODE_ARGS_LOG='$node_log' ENV_LOG='$env_log' PATH='$stub_bin:/usr/bin:/bin' '$root/.agent-layer/run.sh' --env-only echo ok"
  [ "$status" -eq 0 ]

  [ ! -f "$node_log" ]
  run rg -n "^echo$" "$env_log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: run.sh --sync skips with-env
@test "run.sh --sync skips with-env" {
  local root stub_bin node_log env_log bash_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  node_log="$root/node-args.log"
  env_log="$root/env-args.log"
  bash_bin="$(command -v bash)"

  mkdir -p "$stub_bin"
  write_stub_node "$stub_bin"

  cat >"$root/.agent-layer/with-env.sh" <<'EOF'
#!/usr/bin/env bash
printf "%s\n" "$@" > "$ENV_LOG"
exit 0
EOF
  chmod +x "$root/.agent-layer/with-env.sh"

  run "$bash_bin" -c "cd '$root/sub/dir' && NODE_ARGS_LOG='$node_log' ENV_LOG='$env_log' PATH='$stub_bin:/usr/bin:/bin' '$root/.agent-layer/run.sh' --sync echo ok"
  [ "$status" -eq 0 ]

  run rg -n "sync.mjs" "$node_log"
  [ "$status" -eq 0 ]
  [ ! -f "$env_log" ]

  rm -rf "$root"
}

# Test: run.sh --check-env reruns sync on failed check
@test "run.sh --check-env reruns sync on failed check" {
  local root stub_bin node_log env_log bash_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  node_log="$root/node-args.log"
  env_log="$root/env-args.log"
  bash_bin="$(command -v bash)"

  mkdir -p "$stub_bin"
  write_stub_node "$stub_bin"

  cat >"$root/.agent-layer/with-env.sh" <<'EOF'
#!/usr/bin/env bash
printf "%s\n" "$@" > "$ENV_LOG"
exit 0
EOF
  chmod +x "$root/.agent-layer/with-env.sh"

  run "$bash_bin" -c "cd '$root/sub/dir' && NODE_ARGS_LOG='$node_log' ENV_LOG='$env_log' NODE_FAIL_CHECK=1 PATH='$stub_bin:/usr/bin:/bin' '$root/.agent-layer/run.sh' --check-env echo ok"
  [ "$status" -eq 0 ]

  run rg -n -- "--check" "$node_log"
  [ "$status" -eq 0 ]
  [ "$(wc -l < "$node_log")" -eq 2 ]

  rm -rf "$root"
}

# Test: run.sh --project-env forwards flag
@test "run.sh --project-env forwards flag" {
  local root stub_bin node_log env_log bash_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  node_log="$root/node-args.log"
  env_log="$root/env-args.log"
  bash_bin="$(command -v bash)"

  mkdir -p "$stub_bin"
  write_stub_node "$stub_bin"

  cat >"$root/.agent-layer/with-env.sh" <<'EOF'
#!/usr/bin/env bash
printf "%s\n" "$@" > "$ENV_LOG"
exit 0
EOF
  chmod +x "$root/.agent-layer/with-env.sh"

  run "$bash_bin" -c "cd '$root/sub/dir' && NODE_ARGS_LOG='$node_log' ENV_LOG='$env_log' PATH='$stub_bin:/usr/bin:/bin' '$root/.agent-layer/run.sh' --project-env echo ok"
  [ "$status" -eq 0 ]

  run rg -n -- "^--project-env$" "$env_log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: run.sh adds --codex and sets AGENT_LAYER_RUN_CODEX
@test "run.sh adds --codex and sets AGENT_LAYER_RUN_CODEX" {
  local root stub_bin node_log env_log bash_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  node_log="$root/node-args.log"
  env_log="$root/env-args.log"
  bash_bin="$(command -v bash)"

  mkdir -p "$stub_bin"
  write_stub_node "$stub_bin"

  cat >"$root/.agent-layer/with-env.sh" <<'EOF'
#!/usr/bin/env bash
printf "%s\n" "${AGENT_LAYER_RUN_CODEX:-}" > "$ENV_LOG"
exit 0
EOF
  chmod +x "$root/.agent-layer/with-env.sh"

  run "$bash_bin" -c "cd '$root/sub/dir' && NODE_ARGS_LOG='$node_log' ENV_LOG='$env_log' PATH='$stub_bin:/usr/bin:/bin' '$root/.agent-layer/run.sh' codex"
  [ "$status" -eq 0 ]

  run rg -n -- "--codex" "$node_log"
  [ "$status" -eq 0 ]
  run rg -n "^1$" "$env_log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: run.sh blocks disabled agents
@test "run.sh blocks disabled agents" {
  local root stub_bin node_log env_log bash_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  node_log="$root/node-args.log"
  env_log="$root/env-args.log"
  bash_bin="$(command -v bash)"

  mkdir -p "$stub_bin"
  write_stub_node "$stub_bin"

  cat >"$root/.agent-layer/with-env.sh" <<'EOF'
#!/usr/bin/env bash
printf "%s\n" "$@" > "$ENV_LOG"
exit 0
EOF
  chmod +x "$root/.agent-layer/with-env.sh"

  run "$bash_bin" -c "cd '$root/sub/dir' && NODE_ARGS_LOG='$node_log' ENV_LOG='$env_log' AGENT_CONFIG_ENABLED=false PATH='$stub_bin:/usr/bin:/bin' '$root/.agent-layer/run.sh' codex 2>&1"
  [ "$status" -ne 0 ]
  [[ "$output" == *"codex is disabled"* ]]
  [ ! -f "$env_log" ]

  rm -rf "$root"
}

# Test: run.sh appends default args when missing
@test "run.sh appends default args when missing" {
  local root stub_bin node_log env_log bash_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  node_log="$root/node-args.log"
  env_log="$root/env-args.log"
  bash_bin="$(command -v bash)"

  mkdir -p "$stub_bin"
  write_stub_node "$stub_bin"

  cat >"$root/.agent-layer/with-env.sh" <<'EOF'
#!/usr/bin/env bash
printf "%s\n" "$@" > "$ENV_LOG"
exit 0
EOF
  chmod +x "$root/.agent-layer/with-env.sh"

  run "$bash_bin" -c "cd '$root/sub/dir' && NODE_ARGS_LOG='$node_log' ENV_LOG='$env_log' AGENT_CONFIG_DEFAULT_ARGS=$'--model\\ngpt-5.2-codex\\n--reasoning\\nhigh' PATH='$stub_bin:/usr/bin:/bin' '$root/.agent-layer/run.sh' codex"
  [ "$status" -eq 0 ]

  run rg -n "^--model$" "$env_log"
  [ "$status" -eq 0 ]
  run rg -n "^gpt-5.2-codex$" "$env_log"
  [ "$status" -eq 0 ]
  run rg -n "^--reasoning$" "$env_log"
  [ "$status" -eq 0 ]
  run rg -n "^high$" "$env_log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}

# Test: run.sh keeps user flag values over defaults
@test "run.sh keeps user flag values over defaults" {
  local root stub_bin node_log env_log bash_bin
  root="$(create_isolated_parent_root)"
  stub_bin="$root/stub-bin"
  node_log="$root/node-args.log"
  env_log="$root/env-args.log"
  bash_bin="$(command -v bash)"

  mkdir -p "$stub_bin"
  write_stub_node "$stub_bin"

  cat >"$root/.agent-layer/with-env.sh" <<'EOF'
#!/usr/bin/env bash
printf "%s\n" "$@" > "$ENV_LOG"
exit 0
EOF
  chmod +x "$root/.agent-layer/with-env.sh"

  run "$bash_bin" -c "cd '$root/sub/dir' && NODE_ARGS_LOG='$node_log' ENV_LOG='$env_log' AGENT_CONFIG_DEFAULT_ARGS=$'--model\\ngpt-5.2-codex\\n--reasoning\\nhigh' PATH='$stub_bin:/usr/bin:/bin' '$root/.agent-layer/run.sh' codex --model gpt-4"
  [ "$status" -eq 0 ]

  run rg -n "^--model$" "$env_log"
  [ "$status" -eq 0 ]
  run rg -n "^gpt-4$" "$env_log"
  [ "$status" -eq 0 ]
  run rg -n "^gpt-5.2-codex$" "$env_log"
  [ "$status" -ne 0 ]
  run rg -n "^--reasoning$" "$env_log"
  [ "$status" -eq 0 ]

  rm -rf "$root"
}
