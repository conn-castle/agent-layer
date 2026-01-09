#!/usr/bin/env bash
set -euo pipefail

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

usage() {
  cat << 'EOF'
Usage: agent-layer-install.sh [--force] [--upgrade] [--latest-branch <branch>] [--repo-url <url>]

Installs/updates agent-layer in the current working repo and sets up a local launcher.

Options:
  --force, -f       Overwrite ./al if it already exists
  --upgrade, -u     Upgrade .agent-layer to the latest tagged release
  --latest-branch   Update .agent-layer to the latest commit of a branch (detached)
  --repo-url <url>  Override the agent-layer repo URL
  --help, -h        Show this help
EOF
}

FORCE="0"
UPGRADE="0"
LATEST_BRANCH=""
LATEST_BRANCH_SET="0"
REPO_URL_DEFAULT="https://github.com/nicholasjconn/agent-layer.git"
REPO_URL_OVERRIDE="0"
REPO_URL="${AGENTLAYER_REPO_URL:-$REPO_URL_DEFAULT}"
if [[ -n "${AGENTLAYER_REPO_URL:-}" ]]; then
  REPO_URL_OVERRIDE="1"
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --force | -f)
      FORCE="1"
      ;;
    --upgrade | -u)
      UPGRADE="1"
      ;;
    --latest-branch)
      [[ $# -ge 2 ]] || die "--latest-branch requires a value"
      LATEST_BRANCH="$2"
      LATEST_BRANCH_SET="1"
      shift
      ;;
    --latest-branch=*)
      LATEST_BRANCH="${1#*=}"
      LATEST_BRANCH_SET="1"
      ;;
    --repo-url)
      [[ $# -ge 2 ]] || die "--repo-url requires a value"
      REPO_URL="$2"
      REPO_URL_OVERRIDE="1"
      shift
      ;;
    --repo-url=*)
      REPO_URL="${1#*=}"
      REPO_URL_OVERRIDE="1"
      ;;
    --help | -h)
      usage
      exit 0
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
  shift
done

if [[ "$UPGRADE" == "1" && -n "$LATEST_BRANCH" ]]; then
  die "Choose only one: --upgrade or --latest-branch <branch>"
fi
if [[ "$LATEST_BRANCH_SET" == "1" && -z "$LATEST_BRANCH" ]]; then
  die "--latest-branch requires a value"
fi

command -v git > /dev/null 2>&1 || die "git is required (not found)."

WORKING_ROOT="$(pwd -P)"
if [[ "$(basename "$WORKING_ROOT")" == ".agent-layer" ]]; then
  die "Run this from the working repo root (parent of .agent-layer/), not inside .agent-layer/."
fi

if git rev-parse --show-toplevel > /dev/null 2>&1; then
  GIT_ROOT="$(git rev-parse --show-toplevel)"
  if [[ "$GIT_ROOT" != "$WORKING_ROOT" ]]; then
    die "Run this from the repo root: $GIT_ROOT"
  fi
else
  say "WARNING: This directory does not appear to be a git repo."
  say "If you meant a different folder, stop now."
  say "You can continue, but hooks won't be enabled until you init git."
  if [[ -t 0 ]]; then
    read -r -p "Continue anyway? [y/N] " reply
    case "$reply" in
      y | Y | yes | YES) ;;
      *)
        die "Aborted."
        ;;
    esac
  else
    die "Not a git repo and no TTY available to confirm. Re-run from a TTY or after init."
  fi
fi

AGENTLAYER_DIR="$WORKING_ROOT/.agent-layer"

resolve_fetch_target() {
  if [[ "$REPO_URL_OVERRIDE" == "1" ]]; then
    printf "%s" "$REPO_URL"
    return 0
  fi
  if git -C "$AGENTLAYER_DIR" remote get-url origin > /dev/null 2>&1; then
    printf "%s" "origin"
    return 0
  fi
  die "No origin remote found. Use --repo-url <url> or set AGENTLAYER_REPO_URL."
}

upgrade_agent_layer() {
  local fetch_target latest_tag current_commit current_tag changes

  if [[ -n "$(git -C "$AGENTLAYER_DIR" status --porcelain)" ]]; then
    die ".agent-layer has uncommitted changes. Commit or stash before upgrading."
  fi

  fetch_target="$(resolve_fetch_target)"

  say "==> Fetching tags for .agent-layer"
  git -C "$AGENTLAYER_DIR" fetch --tags "$fetch_target"

  latest_tag="$(git -C "$AGENTLAYER_DIR" tag --list --sort=-v:refname | head -n 1)"
  [[ -n "$latest_tag" ]] || die "No tags found after fetching; cannot upgrade."

  current_commit="$(git -C "$AGENTLAYER_DIR" rev-parse --short HEAD)"
  current_tag="$(git -C "$AGENTLAYER_DIR" describe --tags --exact-match 2> /dev/null || true)"

  say "==> Current version: ${current_tag:-$current_commit}"
  say "==> Latest tag: $latest_tag"

  if [[ "$current_tag" == "$latest_tag" ]]; then
    say "==> .agent-layer is already up to date."
    return 0
  fi

  say "==> Checking out $latest_tag"
  git -C "$AGENTLAYER_DIR" checkout -q "$latest_tag"

  say "==> Changes since ${current_tag:-$current_commit}:"
  changes="$(git -C "$AGENTLAYER_DIR" --no-pager log --oneline "$current_commit..$latest_tag" || true)"
  if [[ -n "$changes" ]]; then
    printf "%s\n" "$changes"
  else
    say "  (no commits listed)"
  fi

  say "==> Note: .agent-layer is now on a detached HEAD at $latest_tag."
}

latest_branch_agent_layer() {
  local branch="$1"
  local fetch_target current_commit latest_commit changes

  if [[ -n "$(git -C "$AGENTLAYER_DIR" status --porcelain)" ]]; then
    die ".agent-layer has uncommitted changes. Commit or stash before updating."
  fi

  fetch_target="$(resolve_fetch_target)"

  say "==> Fetching latest commit for branch '$branch'"
  git -C "$AGENTLAYER_DIR" fetch "$fetch_target" "$branch"

  latest_commit="$(git -C "$AGENTLAYER_DIR" rev-parse --short FETCH_HEAD)"
  current_commit="$(git -C "$AGENTLAYER_DIR" rev-parse --short HEAD)"

  if [[ "$current_commit" == "$latest_commit" ]]; then
    say "==> .agent-layer is already at latest $branch ($latest_commit)."
  else
    say "==> Current commit: $current_commit"
    say "==> Latest $branch commit: $latest_commit"
  fi
  say "==> Checking out latest $branch commit"
  git -C "$AGENTLAYER_DIR" checkout -q --detach FETCH_HEAD

  if [[ "$current_commit" != "$latest_commit" ]]; then
    changes="$(git -C "$AGENTLAYER_DIR" --no-pager log --oneline -n 5 FETCH_HEAD || true)"
    if [[ -n "$changes" ]]; then
      say "==> Recent commits:"
      printf "%s\n" "$changes"
    fi
  fi

  say "==> Note: .agent-layer is now on a detached HEAD at $latest_commit."
  say "==> To update again, re-run the installer with: --latest-branch $branch"
}

if [[ ! -e "$AGENTLAYER_DIR" ]]; then
  [[ -n "$REPO_URL" ]] || die "Missing repo URL (set AGENTLAYER_REPO_URL or use --repo-url)."
  say "==> Cloning agent-layer into .agent-layer/"
  git clone "$REPO_URL" "$AGENTLAYER_DIR"
  if [[ "$UPGRADE" == "1" ]]; then
    upgrade_agent_layer
  elif [[ -n "$LATEST_BRANCH" ]]; then
    latest_branch_agent_layer "$LATEST_BRANCH"
  fi
else
  if [[ -d "$AGENTLAYER_DIR" ]]; then
    if git -C "$AGENTLAYER_DIR" rev-parse --is-inside-work-tree > /dev/null 2>&1; then
      if [[ "$UPGRADE" == "1" ]]; then
        upgrade_agent_layer
      elif [[ -n "$LATEST_BRANCH" ]]; then
        latest_branch_agent_layer "$LATEST_BRANCH"
      else
        say "==> .agent-layer exists and is a git repo; leaving as-is"
      fi
    else
      die ".agent-layer exists but is not a git repo. Move it aside or remove it, then re-run."
    fi
  else
    die ".agent-layer exists but is not a directory. Move it aside or remove it, then re-run."
  fi
fi

if [[ ! -f "$AGENTLAYER_DIR/.env" ]]; then
  if [[ -f "$AGENTLAYER_DIR/.env.example" ]]; then
    cp "$AGENTLAYER_DIR/.env.example" "$AGENTLAYER_DIR/.env"
    say "==> Created .agent-layer/.env from .env.example"
  else
    die "Missing .agent-layer/.env.example; cannot create .agent-layer/.env"
  fi
else
  say "==> .agent-layer/.env already exists; leaving as-is"
fi

DOCS_DIR="$WORKING_ROOT/docs"
TEMPLATES_DIR="$AGENTLAYER_DIR/templates/docs"
ensure_memory_file() {
  local file_path="$1"
  local template_path="$2"
  local rel_path

  rel_path="${file_path#$WORKING_ROOT/}"

  if [[ ! -f "$template_path" ]]; then
    die "Missing template: ${template_path#$AGENTLAYER_DIR/}"
  fi

  if [[ -f "$file_path" ]]; then
    if [[ -t 0 ]]; then
      read -r -p "$rel_path exists. Keep it? [Y/n] " reply
      case "$reply" in
        n | N | no | NO)
          mkdir -p "$(dirname "$file_path")"
          cp "$template_path" "$file_path"
          say "==> Replaced $rel_path with template"
          ;;
        *)
          say "==> Keeping existing $rel_path"
          ;;
      esac
    else
      say "==> $rel_path exists; leaving as-is (no TTY to confirm)"
    fi
  else
    mkdir -p "$(dirname "$file_path")"
    cp "$template_path" "$file_path"
    say "==> Created $rel_path from template"
  fi
}

say "==> Ensuring project memory files exist"
ensure_memory_file "$DOCS_DIR/ISSUES.md" "$TEMPLATES_DIR/ISSUES.md"
ensure_memory_file "$DOCS_DIR/FEATURES.md" "$TEMPLATES_DIR/FEATURES.md"
ensure_memory_file "$DOCS_DIR/ROADMAP.md" "$TEMPLATES_DIR/ROADMAP.md"
ensure_memory_file "$DOCS_DIR/DECISIONS.md" "$TEMPLATES_DIR/DECISIONS.md"

AL_PATH="$WORKING_ROOT/al"
write_launcher() {
  cat > "$AL_PATH" << 'EOF'
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$SCRIPT_DIR/.agent-layer/al" "$@"
EOF
  chmod +x "$AL_PATH"
}

if [[ -e "$AL_PATH" ]]; then
  if [[ "$FORCE" == "1" ]]; then
    say "==> Overwriting ./al"
    write_launcher
  else
    say "==> ./al exists; leaving as-is (use --force to overwrite)"
  fi
else
  say "==> Creating ./al"
  write_launcher
fi

GITIGNORE_PATH="$WORKING_ROOT/.gitignore"
GITIGNORE_BLOCK="$(
  cat << 'EOF'
# >>> agent-layer
.agent-layer/

# Agent Layer launcher
al

# Agent Layer-generated instruction shims
AGENTS.md
CLAUDE.md
GEMINI.md
.github/copilot-instructions.md

# Agent Layer-generated client configs + artifacts
.mcp.json
.codex/
.gemini/
.claude/
.vscode/mcp.json
# <<< agent-layer
EOF
)"

update_gitignore() {
  local tmp last_char last_line found inblock line
  tmp="$(mktemp)" || die "Failed to create temp file."
  found="0"
  inblock="0"
  if [[ -f "$GITIGNORE_PATH" ]]; then
    while IFS= read -r line || [[ -n "$line" ]]; do
      if [[ "$line" == "# >>> agent-layer" ]]; then
        if [[ "$found" == "0" ]]; then
          printf "%s\n" "$GITIGNORE_BLOCK" >> "$tmp"
          found="1"
        fi
        inblock="1"
        continue
      fi
      if [[ "$inblock" == "1" ]]; then
        if [[ "$line" == "# <<< agent-layer" ]]; then
          inblock="0"
        fi
        continue
      fi
      printf "%s\n" "$line" >> "$tmp"
    done < "$GITIGNORE_PATH"
  else
    : > "$tmp"
  fi

  if [[ "$found" == "0" ]]; then
    if [[ -s "$tmp" ]]; then
      last_char="$(tail -c 1 "$tmp" || true)"
      if [[ "$last_char" != $'\n' ]]; then
        printf '\n' >> "$tmp"
      fi
      last_line="$(tail -n 1 "$tmp" || true)"
      if [[ -n "$last_line" ]]; then
        printf '\n' >> "$tmp"
      fi
    fi
    printf "%s\n" "$GITIGNORE_BLOCK" >> "$tmp"
  fi

  mv "$tmp" "$GITIGNORE_PATH"
}

say "==> Updating .gitignore (agent-layer block)"
update_gitignore

if [[ -f "$AGENTLAYER_DIR/setup.sh" ]]; then
  say "==> Running setup"
  bash "$AGENTLAYER_DIR/setup.sh"
else
  die "Missing .agent-layer/setup.sh"
fi

say ""
say "After completing the required manual steps above, run one of:"
say "  ./al gemini"
say "  ./al claude"
say "  ./al codex"
