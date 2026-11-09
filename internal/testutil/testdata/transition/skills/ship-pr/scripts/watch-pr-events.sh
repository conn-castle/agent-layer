#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: watch-pr-events.sh --repo <owner/name> --pr <number> --log-file <path> [--interval-seconds <seconds>] [--review-deadline-seconds <seconds>] [--exit-on-change]

Poll authoritative GitHub state for one pull request. Changed snapshots are
appended as JSON lines to the log file and printed to stdout (default interval:
60 seconds). --review-deadline-seconds also emits a review-deadline event that
many seconds after PR creation, even when state is unchanged. The same deadline
is not emitted again when restarting with the same log. This is a wakeup to
check for reviewer feedback, not a readiness verdict.

The watcher runs until stopped, or exits after its first event when
--exit-on-change is set. Use one watcher per log file.
EOF
}

repo=""
pr_number=""
log_file=""
interval_seconds="60"
review_deadline_seconds=""
exit_on_change="false"

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
    --review-deadline-seconds)
      review_deadline_seconds="${2:-}"
      if [[ ! "$review_deadline_seconds" =~ ^[1-9][0-9]*$ ]]; then
        printf 'watch-pr-events: --review-deadline-seconds must be a positive integer\n' >&2
        exit 2
      fi
      shift 2
      ;;
    --exit-on-change)
      exit_on_change="true"
      shift
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

# Agent harnesses can force terminal/color output even in JSON pipelines.
unset GH_FORCE_TTY CLICOLOR_FORCE
export NO_COLOR=1 GH_PAGER=cat

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
    --json createdAt,headRefOid,mergeable,reviewDecision,updatedAt,comments,reviews,statusCheckRollup \
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

review_deadline_epoch=""
review_deadline_emitted="false"
if [[ -n "$review_deadline_seconds" ]]; then
  review_deadline_epoch="$(
    jq -er --argjson seconds "$review_deadline_seconds" \
      '.pull_request.createdAt | fromdateiso8601 + $seconds | floor' \
      "$workdir/previous.json"
  )"
  # The append-only log deduplicates notifications, never authorizes a merge.
  review_deadline_emitted="$(
    jq -nr --arg repo "$repo" --argjson pr "$pr_number" \
      --argjson deadline "$review_deadline_epoch" '
      reduce inputs as $event (false;
        . or ($event.repo == $repo and $event.pr == $pr
          and $event.reason == "review-deadline"
          and $event.review_deadline_epoch == $deadline))
      ' "$log_file"
  )"
fi

emit_event() {
  local reason="$1"
  local observed_at
  observed_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  jq -c \
    --arg observed_at "$observed_at" \
    --arg repo "$repo" \
    --argjson pr "$pr_number" \
    --arg reason "$reason" \
    --argjson deadline "${review_deadline_epoch:-null}" \
    '{
      observed_at: $observed_at,
      repo: $repo,
      pr: $pr,
      reason: $reason,
      state: .
    } + (if $reason == "review-deadline" then
      {review_deadline_epoch: $deadline} else {} end)' \
    "$workdir/previous.json" |
    tee -a "$log_file"
}

# A foreground wait may restart after a change happened while no watcher was
# running. Compare fresh state with the last notification instead of losing
# that change in a new baseline. Never use the saved snapshot as current state.
jq -nSc --arg repo "$repo" --argjson pr "$pr_number" '
  reduce inputs as $event (null;
    if $event.repo == $repo and $event.pr == $pr then $event.state else . end)
  | select(. != null)
  ' "$log_file" >"$workdir/last_notified.json"
if [[ -s "$workdir/last_notified.json" ]] && ! cmp -s "$workdir/last_notified.json" "$workdir/previous.json"; then
  emit_event "state-change"
  if [[ "$exit_on_change" == "true" ]]; then
    exit 0
  fi
fi

while [[ "$stopping" != "true" ]]; do
  sleep_seconds="$interval_seconds"
  if [[ -n "$review_deadline_epoch" && "$review_deadline_emitted" != "true" ]]; then
    seconds_until_deadline="$((review_deadline_epoch - $(date -u '+%s')))"
    if [[ "$seconds_until_deadline" -le 0 ]]; then
      emit_event "review-deadline"
      review_deadline_emitted="true"
      if [[ "$exit_on_change" == "true" ]]; then
        break
      fi
      continue
    fi
    if [[ "$seconds_until_deadline" -lt "$sleep_seconds" ]]; then
      sleep_seconds="$seconds_until_deadline"
    fi
  fi

  sleep "$sleep_seconds" &
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
  mv -f "$workdir/current.json" "$workdir/previous.json"
  emit_event "state-change"
  if [[ "$exit_on_change" == "true" ]]; then
    break
  fi
done
