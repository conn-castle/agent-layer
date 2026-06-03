#!/usr/bin/env bash
# Claude Code status line — agentic coding view
# Receives JSON on stdin; outputs a single colored line.

# Force a dot decimal separator so printf parses/formats the floats Claude sends
# (e.g. 43.5, 1.50) regardless of the user's locale (comma-locale shells would
# otherwise fail with "invalid number").
export LC_NUMERIC=C

# ── Color codes ───────────────────────────────────────────────────────────────
RESET='\033[0m'
BOLD='\033[1m'
DIM='\033[2m'
CYAN='\033[36m'
GREEN='\033[32m'
YELLOW='\033[33m'
MAGENTA='\033[35m'
RED='\033[31m'
BLUE='\033[34m'

SEP="${DIM}│${RESET}"

# ── Read stdin once ───────────────────────────────────────────────────────────
input=$(cat)

# Locate jq (common Homebrew path on Apple Silicon, then PATH)
JQ=/opt/homebrew/bin/jq
command -v "$JQ" >/dev/null 2>&1 || JQ=$(command -v jq 2>/dev/null)
if [ -z "$JQ" ]; then
  printf "statusline: jq not found; install jq and ensure it is on PATH\n"
  exit 0
fi

# ── Parse fields ──────────────────────────────────────────────────────────────
model=$("$JQ" -r '.model.display_name // "unknown"' <<<"$input")
cwd=$("$JQ" -r '.workspace.current_dir // .cwd // "?"' <<<"$input")
project_dir=$("$JQ" -r '.workspace.project_dir // ""' <<<"$input")
session_id=$("$JQ" -r '.session_id // ""' <<<"$input")
git_worktree=$("$JQ" -r '.workspace.git_worktree // ""' <<<"$input")
used_pct=$("$JQ" -r '.context_window.used_percentage // empty' <<<"$input")
weekly_pct=$("$JQ" -r '.rate_limits.seven_day.used_percentage // empty' <<<"$input")
weekly_reset=$("$JQ" -r '.rate_limits.seven_day.resets_at // empty' <<<"$input")
total_input=$("$JQ" -r '.context_window.total_input_tokens // 0' <<<"$input")
ctx_size=$("$JQ" -r '.context_window.context_window_size // 0' <<<"$input")
cost_usd=$("$JQ" -r '.cost.total_cost_usd // empty' <<<"$input")
effort=$("$JQ" -r '.effort.level // ""' <<<"$input")

# ── Model (with reasoning effort, when present) ───────────────────────────────
model_str="${CYAN}${model}${RESET}"
if [ -n "$effort" ]; then
  model_str="${model_str} ${BLUE}(${effort})${RESET}"
fi

# ── Session ───────────────────────────────────────────────────────────────────
sess_str=""
if [ -n "$session_id" ]; then
  sess_str="${DIM}#${session_id}${RESET}"
fi

# ── Directory: show path relative to project root, or full cwd ───────────────
# Only use the relative form when cwd is genuinely under project_dir; otherwise
# (unrelated paths, symlink/case differences) fall back to the cwd basename so we
# never render "<project>/<full-cwd>".
if [ -n "$project_dir" ] && [ "${cwd#"$project_dir"/}" != "$cwd" ]; then
  rel="${cwd#"$project_dir"/}"
  [ -z "$rel" ] && rel="."
  dir_str="${DIM}${project_dir##*/}${RESET}/${BOLD}${rel}${RESET}"
else
  dir_str="${BOLD}${cwd##*/}${RESET}"
fi

# ── Worktree indicator ────────────────────────────────────────────────────────
worktree_str=""
if [ -n "$git_worktree" ]; then
  # cwd is inside a linked worktree
  worktree_str="${YELLOW}wt:${git_worktree}${RESET}"
fi

# ── Git: branch + dirty indicator (from the filesystem, not JSON) ─────────────
git_str=""
if command -v git >/dev/null 2>&1; then
  branch=$(git -C "$cwd" rev-parse --abbrev-ref HEAD 2>/dev/null)
  if [ -n "$branch" ] && [ "$branch" != "HEAD" ]; then
    # --porcelain is fast and lock-free
    dirty=$(git -C "$cwd" status --porcelain 2>/dev/null)
    if [ -n "$dirty" ]; then
      git_str="${MAGENTA}${branch}${RESET}${RED}*${RESET}"
    else
      git_str="${MAGENTA}${branch}${RESET}${GREEN}✓${RESET}"
    fi
  elif [ -n "$branch" ]; then
    # detached HEAD
    git_str="${MAGENTA}(detached)${RESET}"
  fi
fi

# ── Context window (always shown, even at 0%) ─────────────────────────────────
if [ -n "$used_pct" ]; then
  used_int=$(printf "%.0f" "$used_pct")
  prefix="ctx:"
elif [ "$ctx_size" -gt 0 ] && [ "$total_input" -gt 0 ]; then
  # Fallback: compute from raw tokens
  used_int=$(( total_input * 100 / ctx_size ))
  prefix="ctx:~"
else
  # No usage data yet — show 0% rather than hiding the segment
  used_int=0
  prefix="ctx:"
fi
# Color by pressure: green < 50%, yellow < 80%, red >= 80%
if [ "$used_int" -ge 80 ]; then
  ctx_color="${RED}"
elif [ "$used_int" -ge 50 ]; then
  ctx_color="${YELLOW}"
else
  ctx_color="${GREEN}"
fi
ctx_str="${ctx_color}${prefix}${used_int}%${RESET}"

# ── Weekly usage limit: time-to-reset + remaining headroom ────────────────────
# Renders "lim:5d/40% left" — days until the 7-day window resets (hours once
# under a day) and the percentage of the weekly allowance still available. The
# time segment is dropped when the reset timestamp is absent (e.g. before the
# first API response), leaving "lim:40% left".
weekly_str=""
if [ -n "$weekly_pct" ]; then
  weekly_int=$(printf "%.0f" "$weekly_pct")
  remaining_pct=$(( 100 - weekly_int ))
  [ "$remaining_pct" -lt 0 ] && remaining_pct=0
  # Color by pressure on the *used* fraction so red still means "almost out".
  if [ "$weekly_int" -ge 80 ]; then
    weekly_color="${RED}"
  elif [ "$weekly_int" -ge 50 ]; then
    weekly_color="${YELLOW}"
  else
    weekly_color="${GREEN}"
  fi

  # resets_at is Unix epoch seconds; compute whole days left, or whole hours
  # once under 24h. Clamp negatives to 0 in case the window has already reset.
  time_left=""
  if [ -n "$weekly_reset" ]; then
    reset_epoch=$(printf "%.0f" "$weekly_reset" 2>/dev/null)
    now=$(date +%s 2>/dev/null)
    if [ -n "$reset_epoch" ] && [ -n "$now" ] && [ "$reset_epoch" -gt 0 ] 2>/dev/null; then
      secs_left=$(( reset_epoch - now ))
      [ "$secs_left" -lt 0 ] && secs_left=0
      if [ "$secs_left" -ge 86400 ]; then
        time_left="$(( secs_left / 86400 ))d"
      elif [ "$secs_left" -ge 3600 ]; then
        time_left="$(( secs_left / 3600 ))h"
      else
        # Under an hour: avoid the misleading "0h".
        time_left="<1h"
      fi
    fi
  fi

  if [ -n "$time_left" ]; then
    weekly_str="${weekly_color}lim:${time_left}/${remaining_pct}% left${RESET}"
  else
    weekly_str="${weekly_color}lim:${remaining_pct}% left${RESET}"
  fi
fi

# ── Lines changed + session cost ──────────────────────────────────────────────
# Rendered as two independent segments (lines_str, dollar_str) so they can sit
# at different positions on the line.
lines_str=""
lines_added=0
lines_removed=0
files_changed=0
if command -v git >/dev/null 2>&1; then
  git_root=$(git -C "$cwd" rev-parse --show-toplevel 2>/dev/null)
  if [ -n "$git_root" ]; then
    # Count tracked staged and unstaged changes against HEAD.
    while IFS=$'\t' read -r added removed _path; do
      files_changed=$(( files_changed + 1 ))
      case "$added:$removed" in
        *-*) continue ;;
      esac
      lines_added=$(( lines_added + added ))
      lines_removed=$(( lines_removed + removed ))
    done < <(git -C "$git_root" diff --numstat HEAD -- 2>/dev/null)

    # Count untracked files without spawning one git diff per file.
    untracked_paths=()
    while IFS= read -r -d '' path; do
      untracked_paths+=("$git_root/$path")
    done < <(git -C "$git_root" ls-files -z -o --exclude-standard -- 2>/dev/null)
    if [ "${#untracked_paths[@]}" -gt 0 ]; then
      files_changed=$(( files_changed + ${#untracked_paths[@]} ))
      while IFS= read -r wc_line; do
        read -r count label _ <<< "$wc_line"
        case "$count" in
          ''|*[!0-9]*) continue ;;
        esac
        [ "$label" = "total" ] && continue
        lines_added=$(( lines_added + count ))
      done < <(printf '%s\0' "${untracked_paths[@]}" | xargs -0 wc -l 2>/dev/null)
    fi
  fi
fi
if [ "${files_changed:-0}" -gt 0 ] 2>/dev/null; then
  lines_str="${GREEN}+${lines_added}${RESET}${DIM}/${RESET}${RED}-${lines_removed}${RESET} ${BLUE}Δ${files_changed}${RESET}"
fi
dollar_str=""
if [ -n "$cost_usd" ]; then
  usd_fmt=$(printf "%.2f" "$cost_usd" 2>/dev/null)
  if [ -n "$usd_fmt" ] && [ "$usd_fmt" != "0.00" ]; then
    dollar_str="${DIM}\$${usd_fmt}${RESET}"
  fi
fi

# ── Assemble line ─────────────────────────────────────────────────────────────
parts=()
parts+=("${model_str}")
parts+=("${SEP}" "${ctx_str}")
[ -n "$weekly_str" ]   && parts+=("${SEP}" "${weekly_str}")
[ -n "$sess_str" ]     && parts+=("${SEP}" "${sess_str}")
[ -n "$lines_str" ]    && parts+=("${SEP}" "${lines_str}")
[ -n "$worktree_str" ] && parts+=("${SEP}" "${worktree_str}")
[ -n "$git_str" ]      && parts+=("${SEP}" "${git_str}")
[ -n "$dollar_str" ]   && parts+=("${SEP}" "${dollar_str}")
parts+=("${SEP}" "${dir_str}")

printf "%b\n" "${parts[*]}"
