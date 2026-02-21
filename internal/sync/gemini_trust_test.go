package sync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

// Tests that stub UserHomeDir must NOT use t.Parallel().

func TestEnsureGeminiTrustedFolderNewFile(t *testing.T) {
	home := t.TempDir()
	orig := UserHomeDir
	UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { UserHomeDir = orig })

	sys := &MockSystem{Fallback: RealSystem{}}
	root := "/fake/repo"

	w := EnsureGeminiTrustedFolder(sys, root)
	if w != nil {
		t.Fatalf("unexpected warning: %v", w)
	}

	path := filepath.Join(home, geminiDir, geminiTrustFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trust file: %v", err)
	}

	// Verify content.
	if got := string(data); got != "{\n  \"/fake/repo\": \"TRUST_FOLDER\"\n}\n" {
		t.Fatalf("unexpected content:\n%s", got)
	}

	// Verify permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat trust file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != geminiTrustPerm {
		t.Fatalf("expected perm %o, got %o", geminiTrustPerm, perm)
	}
}

func TestEnsureGeminiTrustedFolderExistingEntries(t *testing.T) {
	home := t.TempDir()
	orig := UserHomeDir
	UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { UserHomeDir = orig })

	// Seed an existing entry.
	dir := filepath.Join(home, geminiDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := []byte("{\n  \"/other/project\": \"TRUST_FOLDER\"\n}\n")
	if err := os.WriteFile(filepath.Join(dir, geminiTrustFile), existing, 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	sys := &MockSystem{Fallback: RealSystem{}}
	root := "/new/repo"

	w := EnsureGeminiTrustedFolder(sys, root)
	if w != nil {
		t.Fatalf("unexpected warning: %v", w)
	}

	data, err := os.ReadFile(filepath.Join(dir, geminiTrustFile))
	if err != nil {
		t.Fatalf("read trust file: %v", err)
	}

	// Both entries must be present.
	var folders map[string]string
	if err := jsonUnmarshal(data, &folders); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if folders["/other/project"] != "TRUST_FOLDER" {
		t.Fatalf("existing entry lost")
	}
	if folders["/new/repo"] != "TRUST_FOLDER" {
		t.Fatalf("new entry missing")
	}
}

func TestEnsureGeminiTrustedFolderAlreadyPresent(t *testing.T) {
	home := t.TempDir()
	orig := UserHomeDir
	UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { UserHomeDir = orig })

	dir := filepath.Join(home, geminiDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	root := "/already/trusted"
	existing := []byte("{\n  \"/already/trusted\": \"TRUST_FOLDER\"\n}\n")
	trustPath := filepath.Join(dir, geminiTrustFile)
	if err := os.WriteFile(trustPath, existing, 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	info, err := os.Stat(trustPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	modBefore := info.ModTime()

	writeCalled := false
	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			writeCalled = true
			return RealSystem{}.WriteFileAtomic(filename, data, perm)
		},
	}

	w := EnsureGeminiTrustedFolder(sys, root)
	if w != nil {
		t.Fatalf("unexpected warning: %v", w)
	}
	if writeCalled {
		t.Fatalf("expected no write when entry already present")
	}

	info2, err := os.Stat(trustPath)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if info2.ModTime() != modBefore {
		t.Fatalf("file was modified despite entry already present")
	}
}

func TestEnsureGeminiTrustedFolderEmptyFile(t *testing.T) {
	home := t.TempDir()
	orig := UserHomeDir
	UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { UserHomeDir = orig })

	dir := filepath.Join(home, geminiDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, geminiTrustFile), []byte{}, 0o600); err != nil {
		t.Fatalf("write empty: %v", err)
	}

	sys := &MockSystem{Fallback: RealSystem{}}
	root := "/empty/test"

	w := EnsureGeminiTrustedFolder(sys, root)
	if w != nil {
		t.Fatalf("unexpected warning: %v", w)
	}

	data, err := os.ReadFile(filepath.Join(dir, geminiTrustFile))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := string(data); got != "{\n  \"/empty/test\": \"TRUST_FOLDER\"\n}\n" {
		t.Fatalf("unexpected content:\n%s", got)
	}
}

func TestEnsureGeminiTrustedFolderCorruptJSON(t *testing.T) {
	home := t.TempDir()
	orig := UserHomeDir
	UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { UserHomeDir = orig })

	dir := filepath.Join(home, geminiDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	trustPath := filepath.Join(dir, geminiTrustFile)
	if err := os.WriteFile(trustPath, []byte("{corrupt"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	sys := &MockSystem{Fallback: RealSystem{}}

	w := EnsureGeminiTrustedFolder(sys, "/any/repo")
	if w == nil {
		t.Fatalf("expected warning for corrupt JSON")
	}
	if w.Code != warnings.CodeGeminiTrustFolderFailed {
		t.Fatalf("unexpected code: %s", w.Code)
	}

	// Verify file was NOT overwritten.
	data, err := os.ReadFile(trustPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "{corrupt" {
		t.Fatalf("corrupt file was overwritten")
	}
}

func TestEnsureGeminiTrustedFolderHomeDirError(t *testing.T) {
	orig := UserHomeDir
	UserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { UserHomeDir = orig })

	sys := &MockSystem{Fallback: RealSystem{}}
	w := EnsureGeminiTrustedFolder(sys, "/any/repo")
	if w == nil {
		t.Fatalf("expected warning for home dir error")
	}
	if w.Code != warnings.CodeGeminiTrustFolderFailed {
		t.Fatalf("unexpected code: %s", w.Code)
	}
}

func TestEnsureGeminiTrustedFolderReadError(t *testing.T) {
	home := t.TempDir()
	orig := UserHomeDir
	UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { UserHomeDir = orig })

	dir := filepath.Join(home, geminiDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sys := &MockSystem{
		Fallback: RealSystem{},
		ReadFileFunc: func(name string) ([]byte, error) {
			if filepath.Base(name) == geminiTrustFile {
				return nil, errors.New("permission denied")
			}
			return RealSystem{}.ReadFile(name)
		},
	}

	w := EnsureGeminiTrustedFolder(sys, "/any/repo")
	if w == nil {
		t.Fatalf("expected warning for read error")
	}
	if w.Code != warnings.CodeGeminiTrustFolderFailed {
		t.Fatalf("unexpected code: %s", w.Code)
	}
	// Read errors must use the "failed to read" message, not "corrupt".
	if strings.Contains(w.Message, "corrupt") {
		t.Fatalf("read error mislabeled as corrupt: %s", w.Message)
	}
	if !strings.Contains(w.Message, "failed to read") {
		t.Fatalf("expected 'failed to read' in message: %s", w.Message)
	}
}

func TestEnsureGeminiTrustedFolderMkdirError(t *testing.T) {
	home := t.TempDir()
	orig := UserHomeDir
	UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { UserHomeDir = orig })

	sys := &MockSystem{
		Fallback: RealSystem{},
		MkdirAllFunc: func(path string, perm os.FileMode) error {
			return errors.New("mkdir failed")
		},
	}

	w := EnsureGeminiTrustedFolder(sys, "/any/repo")
	if w == nil {
		t.Fatalf("expected warning for mkdir error")
	}
	if w.Code != warnings.CodeGeminiTrustFolderFailed {
		t.Fatalf("unexpected code: %s", w.Code)
	}
}

func TestEnsureGeminiTrustedFolderMarshalError(t *testing.T) {
	home := t.TempDir()
	orig := UserHomeDir
	UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { UserHomeDir = orig })

	sys := &MockSystem{
		Fallback: RealSystem{},
		MarshalIndentFunc: func(v any, prefix, indent string) ([]byte, error) {
			return nil, errors.New("marshal failed")
		},
	}

	w := EnsureGeminiTrustedFolder(sys, "/any/repo")
	if w == nil {
		t.Fatalf("expected warning for marshal error")
	}
	if w.Code != warnings.CodeGeminiTrustFolderFailed {
		t.Fatalf("unexpected code: %s", w.Code)
	}
}

func TestEnsureGeminiTrustedFolderWriteError(t *testing.T) {
	home := t.TempDir()
	orig := UserHomeDir
	UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { UserHomeDir = orig })

	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			return errors.New("write failed")
		},
	}

	w := EnsureGeminiTrustedFolder(sys, "/any/repo")
	if w == nil {
		t.Fatalf("expected warning for write error")
	}
	if w.Code != warnings.CodeGeminiTrustFolderFailed {
		t.Fatalf("unexpected code: %s", w.Code)
	}
}

// TestRunWithProjectGeminiTrustWarningNoiseControlled verifies that a Gemini
// trust warning produced during RunWithProject is included in the result under
// default noise mode and suppressed under reduce mode.
func TestRunWithProjectGeminiTrustWarningNoiseControlled(t *testing.T) {
	// Stub UserHomeDir to force a warning.
	orig := UserHomeDir
	UserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { UserHomeDir = orig })

	root := t.TempDir()

	// Write minimal gitignore.block so updateGitignore succeeds.
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	block := ".agent-layer/\n"
	if err := os.WriteFile(filepath.Join(alDir, "gitignore.block"), []byte(block), 0o644); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
	}

	sys := &MockSystem{
		Fallback: RealSystem{},
		LookPathFunc: func(file string) (string, error) {
			if file == "al" {
				return "/usr/local/bin/al", nil
			}
			return "", os.ErrNotExist
		},
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Enabled: boolPtr(true)},
			},
			Warnings: config.WarningsConfig{NoiseMode: "default"},
		},
		Root: root,
	}

	result, err := RunWithProject(sys, root, project)
	if err != nil {
		t.Fatalf("RunWithProject error: %v", err)
	}

	// The trust warning should appear in the result.
	found := false
	for _, w := range result.Warnings {
		if w.Code == warnings.CodeGeminiTrustFolderFailed {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected GEMINI_TRUST_FOLDER_FAILED warning in result, got %d warnings", len(result.Warnings))
	}

	// With noise_mode=reduce, the suppressible trust warning should be suppressed.
	project.Config.Warnings.NoiseMode = "reduce"
	result2, err := RunWithProject(sys, root, project)
	if err != nil {
		t.Fatalf("RunWithProject error (reduce): %v", err)
	}
	for _, w := range result2.Warnings {
		if w.Code == warnings.CodeGeminiTrustFolderFailed {
			t.Fatalf("trust warning should be suppressed in reduce mode")
		}
	}
}

// boolPtr is duplicated here because gemini_trust_test.go is in the same
// package as sync_extra_test.go which also defines it — but only one
// definition is needed per package. This helper is provided in sync_extra_test.go.

// jsonUnmarshal is a test helper to decode JSON.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
