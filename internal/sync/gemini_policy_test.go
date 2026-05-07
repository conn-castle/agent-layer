package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildGeminiPoliciesEmitsRulePerCommand(t *testing.T) {
	t.Parallel()
	got := buildGeminiPolicies([]string{"git status", "ls"})

	if !strings.HasPrefix(got, "# GENERATED FILE\n") {
		t.Fatalf("expected GENERATED FILE header, got %q", got)
	}
	for _, want := range []string{
		"[[rule]]\ntoolName = \"run_shell_command\"\ncommandPrefix = \"git status\"\ndecision = \"allow\"\npriority = 100\nallowRedirection = true\n",
		"[[rule]]\ntoolName = \"run_shell_command\"\ncommandPrefix = \"ls\"\ndecision = \"allow\"\npriority = 100\nallowRedirection = true\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing rule block %q in output:\n%s", want, got)
		}
	}
}

func TestBuildGeminiPoliciesQuotesSpecialChars(t *testing.T) {
	t.Parallel()
	got := buildGeminiPolicies([]string{`echo "hello"`})
	if !strings.Contains(got, `commandPrefix = "echo \"hello\""`) {
		t.Fatalf("expected quoted commandPrefix in output:\n%s", got)
	}
}

func TestBuildGeminiPoliciesEscapesControlBytesAsTOML(t *testing.T) {
	t.Parallel()
	// 0x07 (BEL) and 0x0b (VT) are escaped by Go's %q as backslash-a and
	// backslash-v, which TOML 1.0 rejects. tomlBasicString must emit the
	// 6-char Unicode escape (backslash-u0007) instead.
	got := buildGeminiPolicies([]string{"bell\x07tab\x0b"})
	want := "commandPrefix = \"bell\\u0007tab\\u000B\""
	if !strings.Contains(got, want) {
		t.Fatalf("expected TOML-escaped control bytes %q, got:\n%s", want, got)
	}
	if strings.Contains(got, `\a`) || strings.Contains(got, `\v`) {
		t.Fatalf("output contains TOML-invalid \\a or \\v escape:\n%s", got)
	}
}

func TestWriteGeminiPoliciesWritesFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{Fallback: RealSystem{}}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
		},
		CommandsAllow: []string{"git status"},
		Root:          root,
	}

	if err := WriteGeminiPolicies(sys, root, project); err != nil {
		t.Fatalf("WriteGeminiPolicies error: %v", err)
	}

	policyPath := filepath.Join(root, ".gemini", "policies", "agent-layer.toml")
	data, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("read policy file: %v", err)
	}
	if !strings.Contains(string(data), "commandPrefix = \"git status\"") {
		t.Fatalf("policy missing rule for git status:\n%s", data)
	}
}

func TestWriteGeminiPoliciesRemovesStaleFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	policyPath := filepath.Join(root, ".gemini", "policies", "agent-layer.toml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte("# stale\n"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	sys := &MockSystem{Fallback: RealSystem{}}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
		},
		Root: root,
	}

	if err := WriteGeminiPolicies(sys, root, project); err != nil {
		t.Fatalf("WriteGeminiPolicies error: %v", err)
	}
	if _, err := os.Stat(policyPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale policy file to be removed, stat err=%v", err)
	}
}

func TestWriteGeminiPoliciesEmptyCommandsRemoveError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		RemoveFunc: func(name string) error {
			return errors.New("remove boom")
		},
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
		},
		Root: root,
	}

	err := WriteGeminiPolicies(sys, root, project)
	if err == nil || !strings.Contains(err.Error(), "remove boom") {
		t.Fatalf("expected remove error, got %v", err)
	}
}

func TestWriteGeminiPoliciesMkdirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		MkdirAllFunc: func(path string, perm os.FileMode) error {
			return errors.New("mkdir boom")
		},
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
		},
		CommandsAllow: []string{"git status"},
		Root:          root,
	}

	err := WriteGeminiPolicies(sys, root, project)
	if err == nil || !strings.Contains(err.Error(), "mkdir boom") {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}

func TestWriteGeminiPoliciesWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			return errors.New("write boom")
		},
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
		},
		CommandsAllow: []string{"git status"},
		Root:          root,
	}

	err := WriteGeminiPolicies(sys, root, project)
	if err == nil || !strings.Contains(err.Error(), "write boom") {
		t.Fatalf("expected write error, got %v", err)
	}
}
