#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: watch-pr-events.sh --repo <owner/name> --pr <number> --log-file <path>

Stream relevant GitHub webhook events for one pull request. Matching payloads
are appended as JSON lines to the log file and printed to stdout. The watcher
runs until intentionally stopped.
EOF
}

repo=""
pr_number=""
log_file=""

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
for command in gh jq tee; do
  if ! command -v "$command" >/dev/null 2>&1; then
    printf 'watch-pr-events: required command not found: %s\n' "$command" >&2
    exit 2
  fi
done

if ! gh webhook --help >/dev/null 2>&1; then
  cat >&2 <<'EOF'
watch-pr-events: required GitHub CLI extension cli/gh-webhook is not installed.
Ask the user to approve installation, then run:
  gh extension install cli/gh-webhook
Do not install the extension or change authentication without user approval.
EOF
  exit 2
fi

mkdir -p "$(dirname "$log_file")"
touch "$log_file"

events="pull_request,pull_request_review,pull_request_review_comment,issue_comment,check_run"
stopping="false"
trap 'stopping="true"' INT TERM

printf 'watch-pr-events: watching %s PR #%s; appending events to %s\n' "$repo" "$pr_number" "$log_file" >&2

set +e
gh webhook forward --repo "$repo" --events "$events" |
  jq --unbuffered -c --argjson pr "$pr_number" '
    select(
      (.number? == $pr)
      or (.pull_request?.number? == $pr)
      or ((.issue?.pull_request? != null) and (.issue?.number? == $pr))
      or (([.check_run?.pull_requests[]?.number] | index($pr)) != null)
    )
  ' |
  tee -a "$log_file"
pipeline_status=("${PIPESTATUS[@]}")
set -e

if [[ "$stopping" == "true" ]]; then
  exit 0
fi

if [[ "${pipeline_status[0]}" -ne 0 ]]; then
  cat >&2 <<'EOF'
watch-pr-events: GitHub webhook forwarding failed. Review the preceding error.
Refetch authoritative pull-request state, then retry once with the same log if
this was a transient transport failure. The watcher requires repository webhook
administration permission and exclusive forwarder access for this repository;
ask the user to resolve a repeated failure or output proving missing permission,
authentication, or an existing forwarder. Do not fall back silently.
EOF
  exit "${pipeline_status[0]}"
fi
if [[ "${pipeline_status[1]}" -ne 0 || "${pipeline_status[2]}" -ne 0 ]]; then
  printf 'watch-pr-events: failed to filter or append webhook events\n' >&2
  exit 1
fi

printf 'watch-pr-events: webhook forwarding ended before it was intentionally stopped\n' >&2
exit 1
