package sync

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectedChimeCommandsFailOpen(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, provider, command, successOutput string
	}{
		{"claude", "claude", agentLayerClaudeChimeCommand, ""},
		{"codex", "codex", agentLayerCodexChimeCommand, "{}\n"},
		{"antigravity", "antigravity", agentLayerAntigravityChimeCommand, "{\"decision\":\"allow\"}\n"},
		{"grok", "grok", agentLayerGrokChimeCommand, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, mode := range []string{"success", "failure", "missing"} {
				t.Run(mode, func(t *testing.T) {
					t.Parallel()
					dir := t.TempDir()
					argsPath := filepath.Join(dir, "args")
					inputPath := filepath.Join(dir, "input")
					command := tc.command
					if mode != "missing" {
						// A shell function avoids executing a freshly written file,
						// which can fail with ETXTBSY when parallel forks inherit
						// its writable descriptor. The subshell keeps exit local
						// to the stub so the projected fallback still runs.
						script := "al() (\nprintf '%s\\n' \"$*\" > \"$CAPTURE_ARGS\"\n/bin/cat > \"$CAPTURE_INPUT\"\n"
						if mode == "failure" {
							script += "echo 'stub handler failed' >&2\nexit 1\n"
						} else if tc.successOutput != "" {
							script += "printf '%s' '" + strings.ReplaceAll(tc.successOutput, "'", "'\\''") + "'\n"
						}
						command = script + ")\n" + command
					}
					cmd := exec.Command("/bin/sh", "-c", command) // #nosec G204 -- repository-owned fixed command and test stub.
					cmd.Env = append(os.Environ(), "PATH=", "CAPTURE_ARGS="+argsPath, "CAPTURE_INPUT="+inputPath)
					cmd.Stdin = strings.NewReader(`{"fixture":true}`)
					var stdout, stderr bytes.Buffer
					cmd.Stdout = &stdout
					cmd.Stderr = &stderr
					if err := cmd.Run(); err != nil {
						t.Fatalf("projected command must exit 0: %v; stderr=%q", err, stderr.String())
					}
					if got := stdout.String(); got != tc.successOutput {
						t.Fatalf("stdout = %q, want %q", got, tc.successOutput)
					}
					if mode == "success" {
						args, err := os.ReadFile(argsPath) // #nosec G304 -- test-owned path.
						if err != nil || string(args) != "hook chime "+tc.provider+"\n" {
							t.Fatalf("stub args = %q, err=%v; stderr=%q", args, err, stderr.String())
						}
						input, err := os.ReadFile(inputPath) // #nosec G304 -- test-owned path.
						if err != nil || string(input) != `{"fixture":true}` {
							t.Fatalf("stub input = %q, err=%v", input, err)
						}
					} else if !strings.Contains(stderr.String(), "handler unavailable") {
						t.Fatalf("expected actionable fallback stderr, got %q", stderr.String())
					}
				})
			}
		})
	}
}
