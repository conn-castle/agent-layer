package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/probe/antigravity"
)

// TestProbeAntigravityCommandHonorsTmpRoot exercises the path-construction
// branch (.agent-layer/tmp/) and asserts the public JSON shape of the probe
// result. The previous test only round-tripped two bool fields through the
// encoder — it would have passed even if the stable JSON keys were renamed.
func TestProbeAntigravityCommandHonorsTmpRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalProbe := runAntigravityProbe
	runAntigravityProbe = func(ctx context.Context, tmpRoot string) (*antigravity.Result, error) {
		if !strings.HasSuffix(tmpRoot, ".agent-layer/tmp") {
			t.Fatalf("tmp root = %q, want .agent-layer/tmp", tmpRoot)
		}
		return &antigravity.Result{
			AgyVersion:   "1.0.0",
			ProbedAt:     time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
			ProbeDir:     tmpRoot + "/probe-antigravity-test",
			WorkspaceDir: tmpRoot + "/probe-antigravity-test/workspace",
			AgyConfigDir: tmpRoot + "/probe-antigravity-test/agycfg",
			ExitCode:     0,
			Capabilities: antigravity.CapabilityMatrix{
				PermissionsLoaded: true,
				MCPConfigMigrated: true,
			},
		}, nil
	}
	t.Cleanup(func() { runAntigravityProbe = originalProbe })

	cmd := newProbeAntigravityCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("probe command: %v", err)
	}

	// Wire-format assertion: every documented top-level JSON key appears so a
	// future field rename is caught. Per F-D-7, the agy-config dir is keyed
	// as `agy_config_dir` (the legacy `gemini_dir` name is intentionally not
	// part of the public contract).
	rendered := out.String()
	for _, key := range []string{
		`"agy_version"`, `"probed_at"`, `"probe_dir"`, `"workspace_dir"`,
		`"agy_config_dir"`, `"exit_code"`, `"wall_clock_seconds"`,
		`"capabilities"`,
	} {
		if !strings.Contains(rendered, key) {
			t.Fatalf("expected JSON key %s in output, got:\n%s", key, rendered)
		}
	}
	// The legacy field name must NOT appear in the public JSON contract.
	if strings.Contains(rendered, `"gemini_dir"`) {
		t.Fatalf("legacy gemini_dir field must not appear in probe JSON output:\n%s", rendered)
	}
	// Confirm the document is still valid JSON shaped as Result so any
	// schema-level breakage (e.g., key removed at the struct level) is
	// caught alongside the wire-format check.
	var result antigravity.Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode probe JSON: %v\n%s", err, rendered)
	}
	if result.AgyConfigDir == "" {
		t.Fatalf("expected AgyConfigDir set in decoded result, got: %+v", result)
	}
}

// TestProbeAntigravityCommandSurfacesNonZeroExit asserts F-A-12: a non-zero
// probe exit must cause the CLI to exit non-zero so callers piping into jq
// can detect failure. The JSON must still be written to stdout so the
// machine-readable forensic data is available to the caller.
func TestProbeAntigravityCommandSurfacesNonZeroExit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalProbe := runAntigravityProbe
	runAntigravityProbe = func(ctx context.Context, tmpRoot string) (*antigravity.Result, error) {
		return &antigravity.Result{
			AgyVersion: "1.0.0",
			ProbedAt:   time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
			ExitCode:   2,
			Error:      "agy refused the probe",
		}, nil
	}
	t.Cleanup(func() { runAntigravityProbe = originalProbe })

	cmd := newProbeAntigravityCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected non-zero probe to surface as CLI error")
	}
	if !strings.Contains(err.Error(), "agy refused") {
		t.Fatalf("expected error to include probe Error string, got: %v", err)
	}
	if !strings.Contains(out.String(), `"exit_code": 2`) {
		t.Fatalf("expected JSON still written to stdout, got:\n%s", out.String())
	}
	// F-A2-9: assert the JSON `error` field made it to stdout (not just the
	// CLI error path).
	if !strings.Contains(out.String(), `"error": "agy refused the probe"`) {
		t.Fatalf("expected JSON error field on stdout, got:\n%s", out.String())
	}
	// Round 2 F-B2-10: confirm the JSON is well-formed and the error field
	// round-trips through json.Encoder so callers piping into jq can rely on
	// the forensic payload on the failure path.
	var result antigravity.Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode non-zero JSON: %v\n%s", err, out.String())
	}
	if result.ExitCode != 2 || result.Error != "agy refused the probe" {
		t.Fatalf("non-zero JSON round-trip mismatch: %+v", result)
	}
}

// TestProbeCommandWiresAntigravitySubcommand asserts the subcommand is
// actually attached (not just that the word "agy" appears in help text). A
// help-substring test would still pass if AddCommand were removed — the
// parent Short already contains "Antigravity".
func TestProbeCommandWiresAntigravitySubcommand(t *testing.T) {
	cmd := newProbeCmd()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "agy" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected probe command to register an agy subcommand")
	}
}
