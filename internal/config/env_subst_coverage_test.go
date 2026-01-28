package config

import (
	"path/filepath"
	"testing"
)

func TestExtractEnvVarNames(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "no vars",
			input:    "plain text",
			expected: nil,
		},
		{
			name:     "one var",
			input:    "Hello ${NAME}",
			expected: []string{"NAME"},
		},
		{
			name:     "multiple vars",
			input:    "Host: ${HOST}, Port: ${PORT}",
			expected: []string{"HOST", "PORT"},
		},
		{
			name:     "duplicates",
			input:    "${VAR} and ${VAR}",
			expected: []string{"VAR", "VAR"},
		},
		{
			name:     "empty placeholder",
			input:    "Hello ${}",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEnvVarNames(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected length %d, got %d", len(tt.expected), len(got))
			}
			for i, v := range got {
				if v != tt.expected[i] {
					t.Errorf("expected at %d: %s, got: %s", i, tt.expected[i], v)
				}
			}
		})
	}
}

func TestWithBuiltInEnv(t *testing.T) {
	env := map[string]string{
		"KEY": "VALUE",
	}
	repoRoot := "/path/to/repo"
	got := WithBuiltInEnv(env, repoRoot)

	if got["KEY"] != "VALUE" {
		t.Errorf("lost existing key")
	}
	if got[BuiltinRepoRootEnvVar] != repoRoot {
		t.Errorf("missing repo root env var")
	}
	// Verify copy
	env["NEW"] = "VALUE"
	if _, ok := got["NEW"]; ok {
		t.Errorf("WithBuiltInEnv did not return a copy")
	}
}

func TestIsBuiltInEnvVar(t *testing.T) {
	if !IsBuiltInEnvVar(BuiltinRepoRootEnvVar) {
		t.Errorf("expected true for builtin")
	}
	if IsBuiltInEnvVar("OTHER") {
		t.Errorf("expected false for other")
	}
}

func TestShouldExpandPath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"~", true},
		{"~/file", true},
		{"${AL_REPO_ROOT}", true},
		{"${AL_REPO_ROOT}/file", true},
		{" /path ", false},
		{"relative", false},
	}

	for _, tt := range tests {
		if got := ShouldExpandPath(tt.input); got != tt.want {
			t.Errorf("ShouldExpandPath(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestExpandPath(t *testing.T) {
	repoRoot := "/my/repo"

	// Test relative path expansion
	got, err := ExpandPath("file.txt", repoRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(repoRoot, "file.txt")
	if got != expected {
		t.Errorf("ExpandPath(file.txt) = %q, want %q", got, expected)
	}

	// Test absolute path (should remain absolute)
	absPath := "/abs/path"
	got, err = ExpandPath(absPath, repoRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != absPath {
		t.Errorf("ExpandPath(%q) = %q, want %q", absPath, got, absPath)
	}

	// Test missing repo root for relative path
	_, err = ExpandPath("file.txt", "")
	if err == nil {
		t.Errorf("expected error for missing repo root")
	}

	// Test tilde expansion (basic check)
	got, err = ExpandPath("~", repoRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("expected absolute path for ~, got %q", got)
	}
}

func TestExpandPathIfNeeded(t *testing.T) {
	repoRoot := "/my/repo"

	// Should expand
	got, err := ExpandPathIfNeeded("~/file", "expanded", repoRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("expected expanded path, got %q", got)
	}

	// Should not expand
	got, err = ExpandPathIfNeeded("/absolute", "/absolute", repoRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/absolute" {
		t.Errorf("got %q, want /absolute", got)
	}
}
