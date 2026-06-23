package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projection"
)

func TestTomlHelpers_Empty(t *testing.T) {
	t.Parallel()
	if s := tomlStringArray([]string{}); s != "[]" {
		t.Fatalf("expected [], got %q", s)
	}
	if s := tomlInlineTable(map[string]string{}); s != "{}" {
		t.Fatalf("expected {}, got %q", s)
	}
}

func TestSplitCodexHeaders_Empty(t *testing.T) {
	t.Parallel()
	spec, err := splitCodexHeaders(map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.BearerTokenEnvVar != "" {
		t.Fatalf("expected empty bearer token env var, got %q", spec.BearerTokenEnvVar)
	}
	if spec.EnvHeaders != nil {
		t.Fatalf("expected nil env headers, got %v", spec.EnvHeaders)
	}
	if spec.HTTPHeaders != nil {
		t.Fatalf("expected nil http headers, got %v", spec.HTTPHeaders)
	}
}

func TestWriteCodexConfig_MkdirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Create .codex as a file to force MkdirAll to fail
	if err := os.WriteFile(filepath.Join(root, ".codex"), []byte("file"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	project := &config.ProjectConfig{}
	if err := WriteCodexConfig(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error from MkdirAll")
	}
}

func TestBuildCodexRules_EmptyCommand(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
		},
		CommandsAllow: []string{"   ", "git status"}, // One empty/whitespace command
	}

	content := buildCodexRules(project)
	if !strings.Contains(content, "\"git\", \"status\"") {
		t.Fatalf("expected git status in rules:\n%s", content)
	}
	// The empty command should be skipped, so no empty pattern
	if strings.Contains(content, "pattern=[]") {
		t.Fatalf("unexpected empty pattern")
	}
}

func TestWriteCodexConfig_BuildError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "http",
						Enabled:   &enabled,
						Clients:   []string{"codex"},
						Transport: "http",
						URL:       "https://example.com?token=${MISSING}",
					},
				},
			},
		},
		Env: map[string]string{},
	}

	if err := WriteCodexConfig(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error from buildCodexConfig")
	}
}

func TestWriteCodexHTTPServer_MissingEnv(t *testing.T) {
	t.Parallel()
	var builder strings.Builder
	server := projection.ResolvedMCPServer{
		ID:        "http",
		Transport: config.TransportHTTP,
		URL:       "https://example.com?token=${MISSING}",
	}
	err := writeCodexHTTPServer(&builder, server, map[string]string{})
	if err == nil {
		t.Fatalf("expected error for missing URL env")
	}
}

func TestWriteCodexStdioServer_SubstitutionErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		server projection.ResolvedMCPServer
	}{
		{
			name: "command env missing",
			server: projection.ResolvedMCPServer{
				ID:        "srv",
				Transport: config.TransportStdio,
				Command:   "${MISSING}",
			},
		},
		{
			name: "arg env missing",
			server: projection.ResolvedMCPServer{
				ID:        "srv",
				Transport: config.TransportStdio,
				Command:   "tool",
				Args:      []string{"--token", "${MISSING}"},
			},
		},
		{
			name: "env var missing",
			server: projection.ResolvedMCPServer{
				ID:        "srv",
				Transport: config.TransportStdio,
				Command:   "tool",
				Env:       map[string]string{"TOKEN": "${MISSING}"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var builder strings.Builder
			if err := writeCodexStdioServer(&builder, tt.server, map[string]string{}); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestSplitCodexHeaders_AuthorizationStatic(t *testing.T) {
	t.Parallel()
	headers := map[string]string{
		"Authorization": "Bearer abc",
	}
	spec, err := splitCodexHeaders(headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.HTTPHeaders["Authorization"] != "Bearer abc" {
		t.Fatalf("expected Authorization in HTTP headers, got %v", spec.HTTPHeaders)
	}
}

func TestCodexTrustedProjectRoot_RejectsEmptyRoot(t *testing.T) {
	t.Parallel()
	if _, err := codexTrustedProjectRoot("   "); err == nil {
		t.Fatalf("expected error for blank repo root")
	}
}

// TestCodexTrustedProjectRoot_RejectsControlChars asserts the fail-loud guard:
// a repo root containing a control character that fmt %q would escape into an
// invalid TOML basic-string escape (e.g. \x00, \a, \v, \x1b) must abort with an
// actionable error instead of silently emitting a .codex/config.toml that Codex
// rejects in its entirety. A regression that dropped the guard would let the
// corrupt key reach the TOML writer and break the whole Codex config.
func TestCodexTrustedProjectRoot_RejectsControlChars(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	for _, ctrl := range []rune{0x00, '\a', '\v', 0x1b, 0x85} {
		root := filepath.Join(base, "repo"+string(ctrl)+"x")
		if _, err := codexTrustedProjectRoot(root); err == nil {
			t.Fatalf("expected error for repo root containing control char U+%04X", ctrl)
		}
	}
}

// TestCodexTrustedProjectRoot_AcceptsPrintablePath confirms the guard does not
// over-reject: a normal absolute path (including printable special characters
// that fmt %q escapes safely, like a double quote) resolves without error and
// round-trips through the TOML key writer producing a single parseable key.
func TestCodexTrustedProjectRoot_AcceptsPrintablePath(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), `repo"quote`)
	resolved, err := codexTrustedProjectRoot(root)
	if err != nil {
		t.Fatalf("unexpected error for printable path: %v", err)
	}
	if !filepath.IsAbs(resolved) {
		t.Fatalf("expected absolute resolved root, got %q", resolved)
	}

	var builder strings.Builder
	appendCodexTrustedProject(&builder, resolved, nil)
	out := builder.String()
	if !strings.Contains(out, "trust_level = \"trusted\"") {
		t.Fatalf("expected managed trust block, got:\n%s", out)
	}
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("emitted trust block is not valid TOML: %v\n%s", err, out)
	}
}

func TestExtractBearerEnvPlaceholder_EdgeCases(t *testing.T) {
	t.Parallel()
	if _, ok := extractBearerEnvPlaceholder("Bearer"); ok {
		t.Fatalf("expected false for short bearer value")
	}
	if _, ok := extractBearerEnvPlaceholder("Token ${TOKEN}"); ok {
		t.Fatalf("expected false for non-bearer prefix")
	}
}
