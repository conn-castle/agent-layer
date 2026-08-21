package templates

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readPRCommentsScript(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "skills", "ship-pr", "scripts", "read-pr-comments.sh")
}

func requireJQ(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skipf("jq is required: %v", err)
	}
}

func runReadPRComments(t *testing.T, env []string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{readPRCommentsScript(t)}, args...)...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func ghStubEnv(t *testing.T, mode string) []string {
	t.Helper()
	bin := t.TempDir()
	stub := filepath.Join(bin, "gh")
	script := `#!/usr/bin/env bash
set -euo pipefail
args="$*"
mode="${GH_STUB_MODE:-ok}"
if [[ "$mode" == "fail" ]]; then
  printf 'gh: boom\n' >&2
  exit 1
fi
if [[ "$mode" == "malformed" ]]; then
  printf '{'
  exit 0
fi
if [[ "$args" == *graphql* ]]; then
  cat <<'JSON'
{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
  {"isResolved":true,"isOutdated":false,"comments":{"nodes":[{"databaseId":11}]}},
  {"isResolved":false,"isOutdated":true,"comments":{"nodes":[{"databaseId":13}]}}
]}}}}}
JSON
  exit 0
fi
if [[ "$args" == *"/issues/"*"/comments"* ]]; then
  if [[ "$args" != *"--paginate"* ]]; then
    printf 'missing --paginate\n' >&2
    exit 1
  fi
  printf '%s\n' '[{"id":1,"html_url":"https://example.test/issue/1","user":{"login":"alex"},"body":"Conversation $(whoami)\n> quoted"}]'
  printf '%s\n' '[{"id":2,"html_url":"https://example.test/issue/2","user":{"login":"blair"},"body":""}]'
  exit 0
fi
if [[ "$args" == *"/pulls/"*"/comments"* ]]; then
  if [[ "$args" != *"--paginate"* ]]; then
    printf 'missing --paginate\n' >&2
    exit 1
  fi
  printf '%s\n' '[{"id":11,"html_url":"https://example.test/inline/11","user":{"login":"riley"},"path":"internal/widget.go","line":42,"side":"RIGHT","pull_request_review_id":21,"body":"Please fix this"}]'
  printf '%s\n' '[{"id":12,"html_url":"https://example.test/inline/12","user":{"login":"sam"},"in_reply_to_id":11,"path":"internal/widget.go","original_line":42,"original_side":"RIGHT","pull_request_review_id":21,"body":"Done"},{"id":13,"html_url":"https://example.test/inline/13","user":{"login":"riley"},"path":"internal/old.go","original_line":9,"original_side":"LEFT","pull_request_review_id":22,"body":"Still open"}]'
  exit 0
fi
if [[ "$args" == *"/pulls/"*"/reviews"* ]]; then
  printf '%s\n' '[{"id":21,"html_url":"https://example.test/review/21","user":{"login":"casey"},"state":"CHANGES_REQUESTED","body":"Needs work"}]'
  exit 0
fi
printf 'unexpected gh invocation: %s\n' "$args" >&2
exit 1
`
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil { // #nosec G306 -- executable test stub requires owner execute permission.
		t.Fatalf("write gh stub: %v", err)
	}
	env := append([]string{}, os.Environ()...)
	env = append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_STUB_MODE="+mode)
	return env
}

func TestReadPRCommentsRequiresRepoAndPR(t *testing.T) {
	requireJQ(t)
	_, stderr, err := runReadPRComments(t, os.Environ())
	if err == nil || !strings.Contains(stderr, "--repo must be in owner/name form") {
		t.Fatalf("missing args error = %v, stderr %q", err, stderr)
	}
	_, stderr, err = runReadPRComments(t, os.Environ(), "--repo", "not-a-repo", "--pr", "1")
	if err == nil || !strings.Contains(stderr, "--repo must be in owner/name form") {
		t.Fatalf("bad repo error = %v, stderr %q", err, stderr)
	}
	_, stderr, err = runReadPRComments(t, os.Environ(), "--repo", "o/n", "--pr", "x")
	if err == nil || !strings.Contains(stderr, "--pr must be a numeric PR number") {
		t.Fatalf("bad pr error = %v, stderr %q", err, stderr)
	}
	_, stderr, err = runReadPRComments(t, os.Environ(), "--repo")
	if err == nil || !strings.Contains(stderr, "--repo requires a value") {
		t.Fatalf("missing repo value error = %v, stderr %q", err, stderr)
	}
}

func TestReadPRCommentsRendersKindsPaginationRepliesAndQuoting(t *testing.T) {
	requireJQ(t)
	out, stderr, err := runReadPRComments(t, ghStubEnv(t, "ok"), "--repo", "acme/widgets", "--pr", "7")
	if err != nil {
		t.Fatalf("read-pr-comments: %v\n%s", err, stderr)
	}
	for _, fragment := range []string{
		"# PR comments for acme/widgets #7",
		"## conversation",
		"- Author: alex",
		"- ID: 1",
		"- URL: https://example.test/issue/1",
		"> Conversation $(whoami)",
		"> > quoted",
		"- Author: blair",
		"> (empty)",
		"## review",
		"- Author: casey",
		"- Review state: CHANGES_REQUESTED",
		"> Needs work",
		"## inline",
		"- Author: riley",
		"- ID: 11",
		"- Thread: resolved=true; outdated=false",
		"- Path: internal/widget.go",
		"- Line: 42",
		"- Side: RIGHT",
		"- Review ID: 21",
		"> Please fix this",
		"## inline-reply",
		"- Author: sam",
		"- Reply to: 11",
		"> Done",
		"- ID: 13",
		"- Thread: resolved=false; outdated=true",
		"> Still open",
	} {
		if !strings.Contains(out, fragment) {
			t.Fatalf("output does not contain %q:\n%s", fragment, out)
		}
	}
	if strings.Count(out, "## inline\n") != 2 {
		t.Fatalf("expected two inline roots, got:\n%s", out)
	}
	if strings.Count(out, "## inline-reply\n") != 1 {
		t.Fatalf("expected one inline reply via in_reply_to_id, got:\n%s", out)
	}
}

func TestReadPRCommentsFailsOnAPIAndMalformedJSON(t *testing.T) {
	requireJQ(t)
	_, stderr, err := runReadPRComments(t, ghStubEnv(t, "fail"), "--repo", "acme/widgets", "--pr", "7")
	if err == nil || !strings.Contains(stderr, "boom") {
		t.Fatalf("api failure = %v, stderr %q", err, stderr)
	}
	_, _, err = runReadPRComments(t, ghStubEnv(t, "malformed"), "--repo", "acme/widgets", "--pr", "7")
	if err == nil {
		t.Fatal("malformed JSON succeeded")
	}
}
