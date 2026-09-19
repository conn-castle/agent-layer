#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: watch-pr-events.sh --repo <owner/name> --pr <number> --log-file <path> [--interval-seconds <seconds>]

Poll authoritative GitHub state for one pull request. Changed snapshots are
appended as JSON lines to the log file and printed to stdout. The watcher runs
until intentionally stopped.
EOF
}

repo=""
pr_number=""
log_file=""
interval_seconds="300"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --pr)
      pr_number="${2:-}"
      shift 2
      ;;
    --log-file)
      log_file="${2:-}"
      shift 2
      ;;
    --interval-seconds)
      interval_seconds="${2:-}"
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      printf 'watch-pr-events: unknown argument: %s\n' "$1" >&2
      usage
      exit 2
      ;;
  esac
done

if [[ ! "$repo" =~ ^[^/]+/[^/]+$ ]]; then
  printf 'watch-pr-events: --repo must be in owner/name form\n' >&2
  exit 2
fi
if [[ ! "$pr_number" =~ ^[0-9]+$ ]]; then
  printf 'watch-pr-events: --pr must be a numeric PR number\n' >&2
  exit 2
fi
if [[ -z "$log_file" ]]; then
  printf 'watch-pr-events: --log-file is required\n' >&2
  exit 2
fi
if [[ ! "$interval_seconds" =~ ^[1-9][0-9]*$ ]]; then
  printf 'watch-pr-events: --interval-seconds must be a positive integer\n' >&2
  exit 2
fi
for command in gh jq tee cmp; do
  if ! command -v "$command" >/dev/null 2>&1; then
    printf 'watch-pr-events: required command not found: %s\n' "$command" >&2
    exit 2
  fi
done

mkdir -p "$(dirname "$log_file")"
touch "$log_file"

# GitHub PR snapshots can exceed ARG_MAX. Pass them to jq as files; --argjson
# puts the payload on argv and kills the watcher.
workdir="$(mktemp -d "${TMPDIR:-/tmp}/watch-pr-events.XXXXXX")"
stopping="false"
sleep_pid=""
stop_watcher() {
  stopping="true"
  if [[ -n "$sleep_pid" ]]; then
    kill "$sleep_pid" 2>/dev/null || true
  fi
}
cleanup() {
  stop_watcher
  rm -rf "$workdir"
}
trap stop_watcher INT TERM
trap cleanup EXIT

fetch_state() {
  local dest="$1"

  gh pr view "$pr_number" \
    --repo "$repo" \
    --json headRefOid,mergeable,reviewDecision,updatedAt,comments,reviews,statusCheckRollup \
    >"$workdir/pr_state.json"
  gh api --paginate "repos/${repo}/pulls/${pr_number}/comments?per_page=100" |
    jq -sc 'add // []' \
    >"$workdir/inline_comments.json"

  jq -S -c -s '{pull_request: .[0], inline_comments: .[1]}' \
    "$workdir/pr_state.json" \
    "$workdir/inline_comments.json" \
    >"$dest"
}

printf 'watch-pr-events: polling %s PR #%s every %ss; appending state changes to %s\n' \
  "$repo" "$pr_number" "$interval_seconds" "$log_file" >&2

fetch_state "$workdir/previous.json"

while [[ "$stopping" != "true" ]]; do
  sleep "$interval_seconds" &
  sleep_pid="$!"
  wait "$sleep_pid" || true
  sleep_pid=""
  if [[ "$stopping" == "true" ]]; then
    break
  fi

  fetch_state "$workdir/current.json"
  if cmp -s "$workdir/current.json" "$workdir/previous.json"; then
    continue
  fi

  observed_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  jq -c \
    --arg observed_at "$observed_at" \
    --arg repo "$repo" \
    --argjson pr "$pr_number" \
    '{
      observed_at: $observed_at,
      repo: $repo,
      pr: $pr,
      state: .
    }' \
    "$workdir/current.json" |
    tee -a "$log_file"
  mv -f "$workdir/current.json" "$workdir/previous.json"
done
