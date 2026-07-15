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
					pathValue := dir
					if mode != "missing" {
						script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$CAPTURE_ARGS\"\n/bin/cat > \"$CAPTURE_INPUT\"\n"
						if mode == "failure" {
							script += "echo 'stub handler failed' >&2\nexit 1\n"
						} else if tc.successOutput != "" {
							script += "printf '%s' '" + strings.ReplaceAll(tc.successOutput, "'", "'\\''") + "'\n"
						}
						if err := os.WriteFile(filepath.Join(dir, "al"), []byte(script), 0o700); err != nil { // #nosec G306 -- executable test stub requires owner execute permission.
							t.Fatalf("write al stub: %v", err)
						}
					} else {
						pathValue = ""
					}
					cmd := exec.Command("/bin/sh", "-c", tc.command) // #nosec G204 -- repository-owned fixed command under test.
					cmd.Env = append(os.Environ(), "PATH="+pathValue, "CAPTURE_ARGS="+argsPath, "CAPTURE_INPUT="+inputPath)
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
							t.Fatalf("stub args = %q, err=%v", args, err)
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
