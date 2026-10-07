run_catalog_certification_script_tests() {
  section "Release Catalog Certification Helper Tests"

  local fixture mock_bin log head_sha scenario
  fixture="$(mktemp -d)"
  mock_bin="$fixture/bin"
  log="$fixture/gh.log"
  head_sha="1111111111111111111111111111111111111111"
  mkdir -p "$fixture/scripts" "$mock_bin"
  cp "$ROOT_DIR/scripts/certify-release-catalog.sh" "$fixture/scripts/"

  cat >"$mock_bin/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  "branch --show-current") echo main ;;
  "status --porcelain") ;;
  "rev-parse HEAD") echo "${TEST_HEAD_SHA}" ;;
  "ls-remote --exit-code origin refs/heads/main") printf '%s\trefs/heads/main\n' "${TEST_REMOTE_SHA}" ;;
  *) echo "unexpected git invocation: $*" >&2; exit 2 ;;
esac
EOF
  cat >"$mock_bin/sleep" <<'EOF'
#!/usr/bin/env bash
printf 'sleep %s\n' "$*" >>"${TEST_GH_LOG}"
EOF
  # Exact-commit run lists come from TEST_SCENARIO and are filtered through the
  # script's own --jq expression, so the run-state classification is exercised.
  cat >"$mock_bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${TEST_GH_LOG}"
next_count() {
  local file="${TEST_FIXTURE}/$1.count" count=0
  [[ -f "${file}" ]] && count="$(<"${file}")"
  count=$((count + 1))
  printf '%s' "${count}" >"${file}"
  echo "${count}"
}
case "$*" in
  "run list "*"--json databaseId,status,conclusion"*)
    count="$(next_count state)"
    case "${TEST_SCENARIO}" in
      no-push-run) runs='[]' ;;
      late-push-run) if [[ "${count}" -le 3 ]]; then runs='[]'; else runs='[{"databaseId":50,"status":"queued","conclusion":""}]'; fi ;;
      failed-run) runs='[{"databaseId":30,"status":"completed","conclusion":"failure"}]' ;;
      certified) runs='[{"databaseId":30,"status":"completed","conclusion":"failure"},{"databaseId":31,"status":"completed","conclusion":"success"}]' ;;
      list-error) echo "HTTP 502" >&2; exit 1 ;;
    esac
    jq_expr=""
    while [[ $# -gt 0 ]]; do
      [[ "$1" == "--jq" ]] && jq_expr="$2"
      shift
    done
    jq -r "${jq_expr}" <<<"${runs}"
    ;;
  "run list "*"--event workflow_dispatch"*"--json databaseId"*)
    if [[ "$(next_count dispatch)" -eq 1 ]]; then echo 41; else echo 42; fi
    ;;
  "workflow run release-catalog-certification.yml --ref main") ;;
  "run watch "*" --exit-status") ;;
  "run view "*" --json conclusion --jq .conclusion") echo success ;;
  "run view 31 --json url --jq .url") echo https://example.invalid/runs/31 ;;
  *) echo "unexpected gh invocation: $*" >&2; exit 2 ;;
esac
EOF
  chmod +x "$mock_bin/git" "$mock_bin/gh" "$mock_bin/sleep"

  run_certify() {
    scenario="$1"
    rm -f "$fixture"/*.count
    : >"$log"
    PATH="$mock_bin:$PATH" TEST_HEAD_SHA="$head_sha" TEST_REMOTE_SHA="${2:-$head_sha}" \
      TEST_GH_LOG="$log" TEST_FIXTURE="$fixture" TEST_SCENARIO="$scenario" \
      bash "$fixture/scripts/certify-release-catalog.sh" >"$fixture/$scenario.out" 2>"$fixture/$scenario.err"
  }

  if run_certify no-push-run && \
     grep -q 'Waiting up to 60s for the push-triggered certification run' "$fixture/$scenario.out" && \
     [[ "$(grep -c '^sleep 5$' "$log")" -eq 12 ]] && \
     grep -q 'workflow run release-catalog-certification.yml --ref main' "$log" && \
     grep -q -- "--commit $head_sha" "$log" && \
     grep -q 'run watch 42 --exit-status' "$log" && \
     grep -q "Release catalog certified for $head_sha" "$fixture/$scenario.out"; then
    pass "catalog-certification: dispatches and waits for the new exact-SHA run when no push run appears within 60s"
  else
    fail "catalog-certification: must dispatch and wait for the new exact-SHA run when no push run appears within 60s"
  fi

  if run_certify late-push-run && \
     ! grep -q '^workflow run' "$log" && \
     grep -q 'run watch 50 --exit-status' "$log" && \
     grep -q "Release catalog certified for $head_sha" "$fixture/$scenario.out"; then
    pass "catalog-certification: reuses a push run that appears after the push without dispatching"
  else
    fail "catalog-certification: must reuse a push run that appears after the push without dispatching"
  fi

  if run_certify failed-run && \
     ! grep -q '^sleep 5$' "$log" && \
     grep -q 'workflow run release-catalog-certification.yml --ref main' "$log" && \
     grep -q 'run watch 42 --exit-status' "$log"; then
    pass "catalog-certification: re-dispatches immediately after a completed unsuccessful run"
  else
    fail "catalog-certification: must re-dispatch immediately after a completed unsuccessful run"
  fi

  if run_certify certified && \
     ! grep -q -e '^workflow run' -e '^run watch' "$log" && \
     grep -q "Release catalog already certified for $head_sha" "$fixture/$scenario.out" && \
     grep -q 'https://example.invalid/runs/31' "$fixture/$scenario.out"; then
    pass "catalog-certification: reports an existing successful exact-SHA run without dispatching"
  else
    fail "catalog-certification: must report an existing successful exact-SHA run without dispatching"
  fi

  if run_certify list-error; then
    fail "catalog-certification: succeeded although the run list failed"
  elif grep -q '^workflow run' "$log"; then
    fail "catalog-certification: must not dispatch when the run list fails"
  else
    pass "catalog-certification: stops without dispatching when the run list fails"
  fi

  if run_certify unpushed "2222222222222222222222222222222222222222"; then
    fail "catalog-certification: accepted an unpushed main commit"
  elif [[ -s "$log" ]] || ! grep -q 'is not the pushed origin/main commit' "$fixture/$scenario.err"; then
    fail "catalog-certification: must reject an unpushed commit before GitHub workflow calls"
  else
    pass "catalog-certification: rejects an unpushed commit before GitHub workflow calls"
  fi

  rm -rf "$fixture"

  local scope_fixture scope_output
  scope_fixture="$(mktemp -d)"
  mkdir -p "$scope_fixture/scripts" "$scope_fixture/docs" "$scope_fixture/internal/benchmark"
  cp "$ROOT_DIR/scripts/catalog-certification-scope.sh" "$scope_fixture/scripts/"
  git -C "$scope_fixture" init -q
  git -C "$scope_fixture" config user.name "Release Test"
  git -C "$scope_fixture" config user.email "release-test@example.invalid"
  printf 'module example.invalid/release-test\n\ngo 1.26\n' >"$scope_fixture/go.mod"
  printf 'baseline\n' >"$scope_fixture/docs/note.md"
  printf 'package benchmark\n' >"$scope_fixture/internal/benchmark/readiness.go"
  git -C "$scope_fixture" add .
  git -C "$scope_fixture" commit -q -m baseline

  scope_output="$(cd "$scope_fixture" && ./scripts/catalog-certification-scope.sh 2>scope-error)"
  if [[ "$scope_output" == "true" ]] && grep -q 'No prior stable release tag' "$scope_fixture/scope-error"; then
    pass "catalog-certification-scope: requires full certification without a prior stable tag"
  else
    fail "catalog-certification-scope: must fail safe without a prior stable tag"
  fi

  git -C "$scope_fixture" tag v1.0.0
  printf 'documentation only\n' >>"$scope_fixture/docs/note.md"
  git -C "$scope_fixture" add docs/note.md
  git -C "$scope_fixture" commit -q -m docs
  scope_output="$(cd "$scope_fixture" && ./scripts/catalog-certification-scope.sh 2>scope-error)"
  if [[ "$scope_output" == "false" ]] && grep -q 'No catalog-critical paths changed' "$scope_fixture/scope-error"; then
    pass "catalog-certification-scope: skips full certification for unrelated release changes"
  else
    fail "catalog-certification-scope: must skip unrelated release changes"
  fi

  mkdir -p "$scope_fixture/internal/moved"
  git -C "$scope_fixture" mv internal/benchmark/readiness.go internal/moved/readiness.go
  git -C "$scope_fixture" commit -q -m move-benchmark-outside-critical-scope
  scope_output="$(cd "$scope_fixture" && ./scripts/catalog-certification-scope.sh 2>scope-error)"
  if [[ "$scope_output" == "true" ]] && grep -q 'internal/benchmark/readiness.go changed' "$scope_fixture/scope-error"; then
    pass "catalog-certification-scope: requires full certification when a benchmark file moves outside critical scope"
  else
    fail "catalog-certification-scope: must treat moving a benchmark file outside critical scope as critical"
  fi

  git -C "$scope_fixture" mv internal/moved/readiness.go internal/benchmark/readiness.go
  git -C "$scope_fixture" commit -q -m restore-benchmark-path
  printf '// changed\n' >>"$scope_fixture/internal/benchmark/readiness.go"
  git -C "$scope_fixture" add internal/benchmark/readiness.go
  git -C "$scope_fixture" commit -q -m benchmark
  scope_output="$(cd "$scope_fixture" && ./scripts/catalog-certification-scope.sh 2>scope-error)"
  if [[ "$scope_output" == "true" ]] && grep -q 'internal/benchmark/readiness.go changed' "$scope_fixture/scope-error"; then
    pass "catalog-certification-scope: requires full certification for benchmark changes"
  else
    fail "catalog-certification-scope: must require benchmark changes"
  fi

  rm -rf "$scope_fixture"
}
