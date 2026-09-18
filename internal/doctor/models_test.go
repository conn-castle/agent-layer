package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/config"
)

func TestCheckModelsDistinguishesDiscoveryFailureAndUnlistedModel(t *testing.T) {
	enabled := true
	for _, tc := range []struct {
		name, script, model string
		status              Status
		message             string
	}{
		{"listed", "printf 'Available models:\\n  - future-model\\n'", "future-model", StatusOK, "1 selectable"},
		{"custom", "printf 'Available models:\\n  - future-model\\n'", "private-model", StatusWarn, "not in the harness"},
		{"unauthenticated", "printf 'You are not authenticated.\\n'", "future-model", StatusWarn, "not authenticated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "grok")
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+tc.script+"\n"), 0o700); err != nil {
				t.Fatal(err)
			} // #nosec G306 -- executable harness fixture.
			project := &config.ProjectConfig{Root: root, Config: config.Config{Agents: config.AgentsConfig{Grok: config.GrokConfig{Enabled: &enabled, Model: tc.model}}}}
			results := CheckModels(project, agentoptions.DiscoveryRequest{Env: []string{}, LookPath: func(string) (string, error) { return path, nil }})
			if len(results) != 1 || results[0].Status != tc.status || !strings.Contains(results[0].Message, tc.message) {
				t.Fatalf("results=%+v", results)
			}
		})
	}
}

func TestCheckModelsSkipsDisabledHarnesses(t *testing.T) {
	results := CheckModels(&config.ProjectConfig{}, agentoptions.DiscoveryRequest{LookPath: func(string) (string, error) { t.Fatal("disabled harness queried"); return "", nil }})
	if len(results) != 0 {
		t.Fatalf("results=%+v", results)
	}
}

func TestCheckModelsSkipsClientDefaults(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{Config: config.Config{Agents: config.AgentsConfig{Antigravity: config.AntigravityConfig{Enabled: &enabled}}}}
	results := CheckModels(project, agentoptions.DiscoveryRequest{LookPath: func(string) (string, error) {
		t.Fatal("client default caused model discovery")
		return "", nil
	}})
	if len(results) != 0 {
		t.Fatalf("unexpected model checks: %+v", results)
	}
}

func TestCheckModelsReportsCopilotDiscoveryFailure(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{Config: config.Config{Agents: config.AgentsConfig{
		CopilotCLI: config.AgentConfig{Enabled: &enabled, Model: "configured-model"},
	}}}
	results := CheckModels(project, agentoptions.DiscoveryRequest{Env: []string{}, LookPath: func(name string) (string, error) {
		if name != "copilot" {
			t.Errorf("unexpected executable %q", name)
		}
		return "", os.ErrNotExist
	}})
	if len(results) != 1 || results[0].CheckName != "copilot_cli models" || results[0].Status != StatusWarn || !strings.Contains(results[0].Message, "model discovery") {
		t.Fatalf("missing actionable Copilot discovery warning: %+v", results)
	}
}
