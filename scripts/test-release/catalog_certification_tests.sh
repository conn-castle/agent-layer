run_catalog_certification_script_tests() {
  section "Release Catalog Certification Helper Tests"

  local fixture mock_bin log count_file head_sha
  fixture="$(mktemp -d)"
  mock_bin="$fixture/bin"
  log="$fixture/gh.log"
  count_file="$fixture/list-count"
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
  cat >"$mock_bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${TEST_GH_LOG}"
case "$*" in
  "run list "*"--status success"*) ;;
  "run list "*"--json databaseId,status"*) ;;
  "run list "*"--json databaseId"*)
    count=0
    [[ -f "${TEST_LIST_COUNT}" ]] && count="$(<"${TEST_LIST_COUNT}")"
    count=$((count + 1))
    printf '%s' "${count}" >"${TEST_LIST_COUNT}"
    if [[ "${count}" -eq 1 ]]; then echo 41; else echo 42; fi
    ;;
  "workflow run release-catalog-certification.yml --ref main") ;;
  "run watch 42 --exit-status") ;;
  "run view 42 --json conclusion --jq .conclusion") echo success ;;
  *) echo "unexpected gh invocation: $*" >&2; exit 2 ;;
esac
EOF
  chmod +x "$mock_bin/git" "$mock_bin/gh"

  if PATH="$mock_bin:$PATH" TEST_HEAD_SHA="$head_sha" TEST_REMOTE_SHA="$head_sha" \
      TEST_GH_LOG="$log" TEST_LIST_COUNT="$count_file" \
      bash "$fixture/scripts/certify-release-catalog.sh" >"$fixture/output" 2>"$fixture/error" && \
     grep -q 'workflow run release-catalog-certification.yml --ref main' "$log" && \
     grep -q -- "--commit $head_sha" "$log" && \
     grep -q 'run watch 42 --exit-status' "$log" && \
     grep -q "Release catalog certified for $head_sha" "$fixture/output"; then
    pass "catalog-certification: dispatches main and waits for the newly created exact-SHA run"
  else
    fail "catalog-certification: must dispatch main and wait for the newly created exact-SHA run"
  fi

  : >"$log"
  if PATH="$mock_bin:$PATH" TEST_HEAD_SHA="$head_sha" TEST_REMOTE_SHA="2222222222222222222222222222222222222222" \
      TEST_GH_LOG="$log" TEST_LIST_COUNT="$count_file" \
      bash "$fixture/scripts/certify-release-catalog.sh" >"$fixture/mismatch-output" 2>"$fixture/mismatch-error"; then
    fail "catalog-certification: accepted an unpushed main commit"
  elif [[ -s "$log" ]] || ! grep -q 'is not the pushed origin/main commit' "$fixture/mismatch-error"; then
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
