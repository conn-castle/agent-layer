package antigravity

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandExitCode(t *testing.T) {
	t.Run("cancelled context returns 124", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		// Even with a non-nil non-exit error, a cancelled context wins (timeout).
		if got := commandExitCode(ctx, errors.New("boom")); got != 124 {
			t.Fatalf("expected 124 for cancelled context, got %d", got)
		}
	})

	t.Run("nil error returns 0", func(t *testing.T) {
		if got := commandExitCode(context.Background(), nil); got != 0 {
			t.Fatalf("expected 0 for nil error, got %d", got)
		}
	})

	t.Run("exit error returns its code", func(t *testing.T) {
		// `false` exits with code 1; produces a real *exec.ExitError.
		err := exec.Command("false").Run()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Skipf("could not produce an *exec.ExitError on this platform: %v", err)
		}
		if got := commandExitCode(context.Background(), err); got != 1 {
			t.Fatalf("expected 1 for exit-1 command, got %d", got)
		}
	})

	t.Run("generic error returns -1", func(t *testing.T) {
		if got := commandExitCode(context.Background(), errors.New("not an exit error")); got != -1 {
			t.Fatalf("expected -1 for generic error, got %d", got)
		}
	})
}

func TestParseCapabilities(t *testing.T) {
	tests := []struct {
		name string
		log  string
		want CapabilityMatrix
	}{
		{
			name: "v1.0.0 baseline",
			log:  "cli-log-v1.0.0.log",
			want: CapabilityMatrix{
				PermissionsLoaded:        true,
				MCPConfigMigrated:        true,
				MCPRuntimeDiscovery:      false,
				InstructionsLoaded:       true,
				SkillNamesVisible:        true,
				MCPConfigNamesVisible:    true,
				SharedSkillDedupObserved: true,
			},
		},
		{
			name: "mcp fixed",
			log:  "cli-log-hypothetical-mcp-fixed.log",
			want: CapabilityMatrix{
				PermissionsLoaded:        true,
				MCPConfigMigrated:        true,
				MCPRuntimeDiscovery:      true,
				InstructionsLoaded:       true,
				SkillNamesVisible:        true,
				MCPConfigNamesVisible:    true,
				SharedSkillDedupObserved: true,
			},
		},
		{
			name: "workspace allowlist",
			log:  "cli-log-hypothetical-workspace-allowlist.log",
			want: CapabilityMatrix{
				PermissionsLoaded:        true,
				MCPConfigMigrated:        true,
				MCPRuntimeDiscovery:      false,
				WorkspacePermissionsRead: true,
				InstructionsLoaded:       true,
				SkillNamesVisible:        true,
				MCPConfigNamesVisible:    true,
				SharedSkillDedupObserved: true,
			},
		},
	}

	stdout := fixture(t, "stdout-v1.0.0.txt")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, evidence := ParseCapabilities(fixture(t, tt.log), stdout)
			if got != tt.want {
				t.Fatalf("capabilities mismatch\nwant: %#v\n got: %#v", tt.want, got)
			}
			if len(evidence) == 0 {
				t.Fatal("expected evidence")
			}
		})
	}
}

// TestParseCapabilities_StdoutBitsAreIndependent exercises each stdout-driven
// capability flag with its own minimal stdout fixture so a parser regression
// that mis-extracts one marker cannot hide behind the all-true v1.0.0 fixture.
// Addresses F-C-1: the table-driven test above shared a single stdout file,
// so every flag was set by the same string and the matchers were never
// independently exercised.
func TestParseCapabilities_StdoutBitsAreIndependent(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		want   CapabilityMatrix
	}{
		{
			name:   "instructions marker alone",
			stdout: "INSTRUCTIONMARKER88",
			want:   CapabilityMatrix{InstructionsLoaded: true},
		},
		{
			name:   "skill name marker alone",
			stdout: "I can see: global-only-skill",
			want:   CapabilityMatrix{SkillNamesVisible: true},
		},
		{
			name:   "MCP name marker alone",
			stdout: "probe-mcp-antigravity-tier",
			want:   CapabilityMatrix{MCPConfigNamesVisible: true},
		},
		{
			name:   "duplicate-once dedup marker",
			stdout: "shared-tier-dup",
			// SkillNamesVisible also fires because shared-tier-dup is a
			// skill-name marker; this is documented in parser.go.
			want: CapabilityMatrix{SkillNamesVisible: true, SharedSkillDedupObserved: true},
		},
		{
			name:   "duplicated skill mentioned twice does NOT count as dedup",
			stdout: "shared-tier-dup shared-tier-dup",
			want:   CapabilityMatrix{SkillNamesVisible: true},
		},
		{
			name:   "skill dropped entirely does NOT count as dedup",
			stdout: "nothing relevant here",
			want:   CapabilityMatrix{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := ParseCapabilities("", tc.stdout)
			if got != tc.want {
				t.Fatalf("capabilities mismatch\nstdout=%q\nwant: %#v\n got: %#v", tc.stdout, tc.want, got)
			}
		})
	}
}

// TestParseCapabilities_NegativeAndEmptyInput pins the all-false branch of
// ParseCapabilities so a future broken regex (e.g. that matches the empty
// string) is caught. Addresses F-C-2.
func TestParseCapabilities_NegativeAndEmptyInput(t *testing.T) {
	t.Run("empty input produces zero capabilities", func(t *testing.T) {
		got, evidence := ParseCapabilities("", "")
		if got != (CapabilityMatrix{}) {
			t.Fatalf("expected zero CapabilityMatrix, got: %#v", got)
		}
		if len(evidence) != 0 {
			t.Fatalf("expected no evidence on empty input, got: %v", evidence)
		}
	})
	t.Run("malformed log without permission marker keeps PermissionsLoaded false", func(t *testing.T) {
		// cli_setting_manager line exists but without the PROBEALLOWMARKER
		// token. The matcher requires that token, so the bit must stay false.
		got, _ := ParseCapabilities("cli_setting_manager.go:65] CLI settings initialized: permissions=other-stuff\n", "INSTRUCTIONMARKER88")
		if got.PermissionsLoaded {
			t.Fatal("PermissionsLoaded must require the PROBEALLOWMARKER token")
		}
		if !got.InstructionsLoaded {
			t.Fatal("stdout-driven bits should still fire")
		}
	})
	t.Run("discovery log that records a failure does NOT count as runtime discovery", func(t *testing.T) {
		// F-A-15 regression guard: the tightened regex requires a
		// word-bounded "registered" or "connected" keyword. A failure
		// message must not satisfy it.
		got, _ := ParseCapabilities("discovery.go:42] mcp server foo failed to register\n", "")
		if got.MCPRuntimeDiscovery {
			t.Fatalf("MCPRuntimeDiscovery must not fire on failure messages: %#v", got)
		}
	})
}

func TestLatestLogText(t *testing.T) {
	root := t.TempDir()
	logDir := filepath.Join(root, "log")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}
	oldPath := filepath.Join(logDir, "cli-old.log")
	newPath := filepath.Join(logDir, "cli-new.log")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatalf("write old log: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0o600); err != nil {
		t.Fatalf("write new log: %v", err)
	}

	path, text, err := latestLogText(logDir)
	if err != nil {
		t.Fatalf("latestLogText: %v", err)
	}
	if path != newPath || text != "new" {
		t.Fatalf("expected latest log %s with text new, got %s %q", newPath, path, text)
	}
}

func TestLatestLogTextBoundsLargeLogToTail(t *testing.T) {
	root := t.TempDir()
	logDir := filepath.Join(root, "log")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}
	path := filepath.Join(logDir, "cli.log")
	head := bytes.Repeat([]byte("h"), maxProbeLogTailBytes+32)
	tail := []byte("TAIL_MARKER")
	if err := os.WriteFile(path, append(head, tail...), 0o600); err != nil {
		t.Fatalf("write large log: %v", err)
	}

	_, text, err := latestLogText(logDir)
	if err != nil {
		t.Fatalf("latestLogText: %v", err)
	}
	if len(text) != maxProbeLogTailBytes {
		t.Fatalf("expected bounded log text length %d, got %d", maxProbeLogTailBytes, len(text))
	}
	if !strings.HasSuffix(text, string(tail)) {
		t.Fatalf("expected bounded log text to preserve tail marker")
	}
}

func TestReadProbeLogTailMissingFile(t *testing.T) {
	_, err := readProbeLogTail(filepath.Join(t.TempDir(), "missing.log"))
	if err == nil {
		t.Fatal("expected missing log read to fail")
	}
}

func TestCreateProbeDirAvoidsSecondLevelCollisions(t *testing.T) {
	root := t.TempDir()
	probedAt := time.Date(2026, 5, 22, 3, 46, 18, 0, time.UTC)

	first, err := createProbeDir(root, probedAt)
	if err != nil {
		t.Fatalf("create first probe dir: %v", err)
	}
	second, err := createProbeDir(root, probedAt)
	if err != nil {
		t.Fatalf("create second probe dir: %v", err)
	}

	if first == second {
		t.Fatalf("expected unique probe dirs, got %s twice", first)
	}
	for _, path := range []string{first, second} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat probe dir %s: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected probe path to be a directory: %s", path)
		}
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name)) // #nosec G304 -- path is constructed from test-controlled fixture names.
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}
