#!/usr/bin/env bash
# Agent Dispatch — verifies discovery and the asynchronous conversation API.

install_dispatch_mock_codex() {
  local dir="$1"
  local mock_bin="$dir/mock-bin"
  local log_dir="$dir/mock-logs"
  mkdir -p "$mock_bin" "$log_dir"

  export MOCK_DISPATCH_CODEX_LOG="$log_dir/codex-dispatch.log"
  export MOCK_DISPATCH_CODEX_PROMPT="$log_dir/codex-dispatch.prompt"
  : > "$MOCK_DISPATCH_CODEX_LOG"
  : > "$MOCK_DISPATCH_CODEX_PROMPT"

  cat > "$mock_bin/codex" <<'MOCK_EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "--version" ]]; then
  printf '0.144.1\n'
  exit 0
fi
{
  echo "ARGS=$*"
  i=0
  for arg in "$@"; do
    echo "ARG_${i}=${arg}"
    i=$((i + 1))
  done
  env | grep -E '^(AL_DISPATCH|CODEX_HOME)' | sort || true
  echo "---END---"
} >> "$MOCK_DISPATCH_CODEX_LOG"
cat > "$MOCK_DISPATCH_CODEX_PROMPT"
sleep "${MOCK_DISPATCH_DELAY_SECONDS:-0}"
printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n'
printf '{"type":"item.completed","item":{"type":"agent_message","text":"codex-dispatch-ok"}}\n'
printf '{"type":"turn.completed"}\n'
MOCK_EOF

  chmod +x "$mock_bin/codex"
  export PATH="$mock_bin:$PATH"
}

dispatch_json_field() {
  local path="$1"
  local field="$2"
  sed -nE "s/.*\"${field}\":\"([^\"]+)\".*/\1/p" "$path" | head -n 1
}

# Match the harness's bounded failure output for commands with JSON on stdout.
dispatch_show_stderr() {
  local path="$1"
  if [[ -s "$path" ]]; then
    echo "  stderr (last 20 lines):"
    tail -n 20 "$path" | sed 's/^/    /'
  fi
}

# Optional state and invocation ID preserve each caller's additional assertions.
dispatch_wait_terminated() {
  local repo_dir="$1" handle="$2" wait_file="$3" stderr_file="$4" label="$5"
  local expected_state="${6:-}" expected_invocation="${7:-}"
  local wait_rc=0
  (cd "$repo_dir" && al dispatch wait "$handle" --condition termination_confirmed >"$wait_file" 2>"$stderr_file") || wait_rc=$?
  if [[ $wait_rc -eq 0 ]] && grep -q '"condition_met":true' "$wait_file" \
    && grep -q '"termination_confirmed":true' "$wait_file" \
    && { [[ -z "$expected_state" ]] || grep -q "\"state\":\"$expected_state\"" "$wait_file"; } \
    && { [[ -z "$expected_invocation" ]] || grep -q "\"invocation_id\":\"$expected_invocation\"" "$wait_file"; }; then
    pass "$label"
  else
    fail "$label (exit code: $wait_rc)"
    dispatch_show_stderr "$stderr_file"
  fi
}

# check_dispatch_reservations exercises reserve-then-start against the built
# binary: every start of one reservation launches the provider at most once.
check_dispatch_reservations() {
  local repo_dir="$1"
  local plain_handle="$2"
  local reserve_file="$repo_dir/reserve.json"
  local rc=0
  local stderr_file="$repo_dir/reserved.stderr"

  : > "$MOCK_DISPATCH_CODEX_LOG"
  (cd "$repo_dir" && al dispatch reserve >"$reserve_file" 2>"$stderr_file") || rc=$?
  local reservation reserved_id
  reservation="$(dispatch_json_field "$reserve_file" handle)"
  reserved_id="$(dispatch_json_field "$reserve_file" invocation_id)"
  if [[ $rc -eq 0 ]] && grep -q '"state":"reserved"' "$reserve_file" && [[ -n "$reservation" && -n "$reserved_id" ]]; then
    pass "dispatch reserve returns a reserved handle and invocation ID"
  else
    fail "dispatch reserve returns a reserved handle and invocation ID (exit code: $rc)"
    dispatch_show_stderr "$stderr_file"
  fi
  assert_mock_agent_not_called "$MOCK_DISPATCH_CODEX_LOG" "reserve launches nothing"

  local selector
  for selector in "${reservation}x" "$reserved_id" "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" "$plain_handle" ""; do
    rc=0
    (cd "$repo_dir" && al dispatch start --reservation "$selector" --agent codex \
      --prompt "Reserved work" >/dev/null 2>&1) || rc=$?
    if [[ $rc -eq 80 ]]; then
      pass "start rejects non-reservation '$selector' with exit 80"
    else
      fail "start rejects non-reservation '$selector' with exit 80 (got: $rc)"
    fi
  done
  assert_mock_agent_not_called "$MOCK_DISPATCH_CODEX_LOG" "unknown reservations launch nothing"

  local index pids=() stderr_paths=() started=0 already=0
  local start_stderr
  for index in {1..8}; do
    start_stderr="$repo_dir/reserved-${index}.stderr"
    (cd "$repo_dir" && MOCK_DISPATCH_DELAY_SECONDS=0.5 al dispatch start --reservation "$reservation" \
      --agent codex --prompt "Reserved work" >"$repo_dir/reserved-${index}.json" \
      2>"$start_stderr") &
    pids+=("$!")
    stderr_paths+=("$start_stderr")
  done
  for index in "${!pids[@]}"; do
    if wait "${pids[$index]}"; then
      started=$((started + 1))
    else
      rc=$?
      if [[ $rc -eq 82 ]]; then
        already=$((already + 1))
      else
        fail "concurrent reservation start exited $rc, want 0 or 82"
        dispatch_show_stderr "${stderr_paths[$index]}"
      fi
    fi
  done
  local same_id
  same_id="$(grep -l "\"invocation_id\":\"$reserved_id\"" "$repo_dir"/reserved-*.json | wc -l | tr -d ' ')"
  if [[ "$started" -eq 1 && "$same_id" -eq 1 && "$already" -eq 7 ]]; then
    pass "eight concurrent starts launch once and reject seven as already started"
  else
    fail "eight concurrent starts launch once and reject seven as already started (started: $started, same id: $same_id, already: $already)"
  fi
  local wait_file="$repo_dir/reserved-wait.json"
  # A terminal result can precede stop proof; continuation requires that proof.
  dispatch_wait_terminated "$repo_dir" "$reservation" "$wait_file" "$stderr_file" \
    "wait on the reservation handle returns the completed invocation" completed "$reserved_id"

  local repeat_file="$repo_dir/reserved-repeat.json"
  local repeat_error="$repo_dir/reserved-repeat.stderr"
  rc=0
  (cd "$repo_dir" && al dispatch start --reservation "$reservation" --agent codex \
    --prompt "Reserved work" >"$repeat_file" 2>"$repeat_error") || rc=$?
  if [[ $rc -eq 82 && ! -s "$repeat_file" ]] && grep -q 'already started' "$repeat_error" \
    && grep -q 'al dispatch inspect' "$repeat_error" && grep -q 'continue only when available' "$repeat_error"; then
    pass "repeated start errors with inspect guidance"
  else
    fail "repeated start errors with inspect guidance (exit code: $rc)"
    dispatch_show_stderr "$repeat_error"
  fi

  rc=0
  (cd "$repo_dir" && al dispatch start --reservation "$reservation" --agent codex \
    --prompt "Different work" >/dev/null 2>&1) || rc=$?
  if [[ $rc -eq 82 ]]; then
    pass "repeated start with different arguments also exits 82"
  else
    fail "repeated start with different arguments also exits 82 (got: $rc)"
  fi
  local launches
  launches="$(grep -c -- '---END---' "$MOCK_DISPATCH_CODEX_LOG" || true)"
  if [[ "$launches" -eq 1 ]]; then
    pass "a reservation launches the provider exactly once"
  else
    fail "a reservation launches the provider exactly once (launches: $launches)"
  fi

  local continuation_file="$repo_dir/reserved-continuation.json"
  rc=0
  (cd "$repo_dir" && al dispatch continue "$reservation" --prompt "Follow up" >"$continuation_file" 2>"$stderr_file") || rc=$?
  local continuation_id
  continuation_id="$(dispatch_json_field "$continuation_file" invocation_id)"
  if [[ $rc -eq 0 && -n "$continuation_id" && "$continuation_id" != "$reserved_id" ]]; then
    pass "reserved conversation can be continued"
  else
    fail "reserved conversation can be continued (exit code: $rc)"
    dispatch_show_stderr "$stderr_file"
  fi
  local original_after_continue="$repo_dir/reserved-after-continuation.json"
  rc=0
  (cd "$repo_dir" && al dispatch start --reservation "$reservation" --agent codex \
    --prompt "Reserved work" >"$original_after_continue" 2>"$repeat_error") || rc=$?
  if [[ $rc -eq 82 && ! -s "$original_after_continue" ]] && grep -q 'already started' "$repeat_error"; then
    pass "repeated start after continuation still errors as already started"
  else
    fail "repeated start after continuation still errors as already started (exit code: $rc)"
    dispatch_show_stderr "$repeat_error"
  fi
  dispatch_wait_terminated "$repo_dir" "$reservation" "$wait_file" "$stderr_file" \
    "reserved continuation termination is confirmed" completed "$continuation_id"
  launches="$(grep -c -- '---END---' "$MOCK_DISPATCH_CODEX_LOG" || true)"
  if [[ "$launches" -eq 2 ]]; then
    pass "repeated reservation start after continuation launches nothing"
  else
    fail "repeated reservation start after continuation launches nothing (launches: $launches)"
  fi
}

run_scenario_agent_dispatch() {
  section "Agent Dispatch"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"
  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard
  enable_codex_local_config_dir "$repo_dir"
  install_dispatch_mock_codex "$repo_dir"

  local options_file="$repo_dir/dispatch-options.json"
  assert_exit_zero_in "$repo_dir" "dispatch options returns selection metadata" \
    al dispatch options
  (cd "$repo_dir" && al dispatch options >"$options_file")
  assert_file_contains "$options_file" '"agent":"codex","available":true' \
    "dispatch options reports available codex"
  assert_file_contains "$options_file" '"reasoning_effort":{"supported":true' \
    "dispatch options reports supported overrides"

  local skill_dir="$repo_dir/.agent-layer/skills/e2e-skill"
  mkdir -p "$skill_dir"
  cat > "$skill_dir/SKILL.md" <<'SKILL'
---
name: e2e-skill
description: Exercise dispatch skill prefixing.
---

Use the e2e dispatch skill.
SKILL

  local start_file="$repo_dir/dispatch-start.json"
  local wait_file="$repo_dir/dispatch-wait.json"
  local stderr_file="$repo_dir/dispatch.stderr"

  local start_rc=0
  (cd "$repo_dir" && al dispatch start --agent codex \
    --prompt "Use defaults" >"$start_file" 2>"$stderr_file") || start_rc=$?
  local handle
  handle="$(dispatch_json_field "$start_file" handle)"
  if [[ $start_rc -eq 0 && -n "$handle" ]]; then
    pass "dispatch start accepts omitted overrides"
    dispatch_wait_terminated "$repo_dir" "$handle" "$wait_file" "$stderr_file" \
      "dispatch wait after omitted overrides completes" completed
  else
    fail "dispatch start accepts omitted overrides (exit code: $start_rc)"
    dispatch_show_stderr "$stderr_file"
  fi
  assert_file_not_contains "$MOCK_DISPATCH_CODEX_LOG" "--model" \
    "start omits an unconfigured model override"
  assert_file_not_contains "$MOCK_DISPATCH_CODEX_LOG" "model_reasoning_effort=" \
    "start omits an unconfigured reasoning override"

  : > "$MOCK_DISPATCH_CODEX_LOG"
  local rc=0
  (cd "$repo_dir" && al dispatch start --agent codex --model gpt-test \
    --reasoning-effort high --prompt "Return ok" >"$start_file" 2>"$stderr_file") || rc=$?
  if [[ $rc -eq 0 ]] && grep -q '"state":"running"' "$start_file"; then
    pass "dispatch start returns a running handle"
  else
    fail "dispatch start returns a running handle (exit code: $rc)"
    dispatch_show_stderr "$stderr_file"
  fi

  handle="$(dispatch_json_field "$start_file" handle)"
  rc=0
  (cd "$repo_dir" && al dispatch wait "$handle" >"$wait_file" 2>"$stderr_file") || rc=$?
  local result_path
  result_path="$(dispatch_json_field "$wait_file" result_path)"
  if [[ $rc -eq 0 ]] && grep -q '"state":"completed"' "$wait_file" \
    && grep -q '"condition_met":true' "$wait_file" && [[ -f "$result_path" ]]; then
    pass "dispatch wait returns a durable completed result"
  else
    fail "dispatch wait returns a durable completed result (exit code: $rc)"
    dispatch_show_stderr "$stderr_file"
  fi
  assert_file_contains "$result_path" "codex-dispatch-ok" "completed Markdown contains the answer"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "ARG_0=exec" "start invokes codex exec"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "CODEX_HOME=$repo_dir/.codex" "worker receives repo CODEX_HOME"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "AL_DISPATCH_ACTIVE=1" "worker receives depth marker"
  assert_file_contains "$MOCK_DISPATCH_CODEX_PROMPT" "Return ok" "start passes prompt"

  dispatch_wait_terminated "$repo_dir" "$handle" "$wait_file" "$stderr_file" \
    "dispatch termination is confirmed before continue"

  : > "$MOCK_DISPATCH_CODEX_LOG"
  local continue_rc=0
  (cd "$repo_dir" && al dispatch continue "$handle" --prompt "Continue" >"$start_file" 2>"$stderr_file") || continue_rc=$?
  if [[ $continue_rc -eq 0 ]]; then
    pass "dispatch continue starts the next invocation"
    dispatch_wait_terminated "$repo_dir" "$handle" "$wait_file" "$stderr_file" \
      "dispatch wait after continue completes the conversation" completed
  else
    fail "dispatch continue starts the next invocation (exit code: $continue_rc)"
    dispatch_show_stderr "$stderr_file"
  fi
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "ARG_1=resume" "continue resumes the provider conversation"
  assert_file_contains "$MOCK_DISPATCH_CODEX_PROMPT" "Continue" "continue passes its prompt"

  : > "$MOCK_DISPATCH_CODEX_PROMPT"
  start_rc=0
  (cd "$repo_dir" && al dispatch start --agent codex --model gpt-test \
    --reasoning-effort high --skill e2e-skill --prompt "Use skill" >"$start_file" 2>"$stderr_file") || start_rc=$?
  handle="$(dispatch_json_field "$start_file" handle)"
  if [[ $start_rc -eq 0 && -n "$handle" ]]; then
    pass "dispatch start accepts the selected skill"
    dispatch_wait_terminated "$repo_dir" "$handle" "$wait_file" "$stderr_file" \
      "dispatch wait after skill start completes" completed
  else
    fail "dispatch start accepts the selected skill (exit code: $start_rc)"
    dispatch_show_stderr "$stderr_file"
  fi
  assert_file_contains "$MOCK_DISPATCH_CODEX_PROMPT" '$e2e-skill' "start invokes the selected skill"

  rc=0
  local handles=()
  local index
  for index in {1..8}; do
    local parallel_start="$repo_dir/parallel-${index}-start.json"
    (cd "$repo_dir" && MOCK_DISPATCH_DELAY_SECONDS=0.3 al dispatch start --agent codex \
      --model gpt-test --reasoning-effort high --prompt "Review ${index}" >"$parallel_start") || rc=$?
    handles+=("$(dispatch_json_field "$parallel_start" handle)")
  done
  local waiter_pids=()
  for index in {1..8}; do
    (cd "$repo_dir" && al dispatch wait "${handles[$((index - 1))]}" >"$repo_dir/parallel-${index}-wait.json") &
    waiter_pids+=("$!")
  done
  for index in "${waiter_pids[@]}"; do
    wait "$index" || rc=$?
  done
  if [[ $rc -eq 0 ]] && [[ "$(grep -l '"state":"completed"' "$repo_dir"/parallel-*-wait.json | wc -l | tr -d ' ')" -eq 8 ]]; then
    pass "eight independent conversations complete in parallel"
  else
    fail "eight independent conversations complete in parallel"
  fi

  start_rc=0
  (cd "$repo_dir" && MOCK_DISPATCH_DELAY_SECONDS=5 al dispatch start --agent codex \
    --model gpt-test --reasoning-effort high --prompt "Cancel me" >"$start_file" 2>"$stderr_file") || start_rc=$?
  handle="$(dispatch_json_field "$start_file" handle)"
  if [[ $start_rc -eq 0 && -n "$handle" ]]; then
    pass "dispatch start accepts work to cancel"
    rc=0
    (cd "$repo_dir" && al dispatch cancel "$handle" >"$wait_file" 2>"$stderr_file") || rc=$?
    if [[ $rc -eq 0 ]] && grep -q '"state":"cancelled"' "$wait_file"; then
      pass "dispatch cancel returns cancelled"
    else
      fail "dispatch cancel returns cancelled (exit code: $rc)"
      dispatch_show_stderr "$stderr_file"
    fi
    rc=0
    (cd "$repo_dir" && al dispatch cancel "$handle" >"$wait_file" 2>"$stderr_file") || rc=$?
    if [[ $rc -eq 0 ]] && grep -q '"state":"cancelled"' "$wait_file"; then
      pass "repeated cancel is idempotent"
    else
      fail "repeated cancel is idempotent (exit code: $rc)"
      dispatch_show_stderr "$stderr_file"
    fi

    dispatch_wait_terminated "$repo_dir" "$handle" "$wait_file" "$stderr_file" \
      "cancelled provider termination is confirmed" cancelled
  else
    fail "dispatch start accepts work to cancel (exit code: $start_rc)"
    dispatch_show_stderr "$stderr_file"
  fi

  check_dispatch_reservations "$repo_dir" "$handle"

  sed -i.bak 's/^max_depth = 3$/max_depth = 1/' "$repo_dir/.agent-layer/config.toml"
  rm -f "$repo_dir/.agent-layer/config.toml.bak"
  : > "$MOCK_DISPATCH_CODEX_LOG"
  rc=0
  (cd "$repo_dir" && AL_DISPATCH_ACTIVE=1 al dispatch start --agent codex --model gpt-test \
    --reasoning-effort high --prompt nested >"$start_file" 2>"$stderr_file") || rc=$?
  if [[ $rc -eq 75 ]]; then
    pass "nested dispatch exits 75"
  else
    fail "nested dispatch exits 75 (got: $rc)"
    dispatch_show_stderr "$stderr_file"
  fi
  assert_mock_agent_not_called "$MOCK_DISPATCH_CODEX_LOG" "nested dispatch did not invoke target"

  cleanup_scenario_dir "$repo_dir"
}
