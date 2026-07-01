package agentoptions

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestResolveUsesLiveAntigravityModels(t *testing.T) {
	binDir := t.TempDir()
	agyPath := filepath.Join(binDir, "agy")
	script := "#!/bin/sh\nif [ \"$1\" = \"models\" ]; then\n  printf 'Live Model\\nBackup Model\\n'\nfi\n"
	if err := os.WriteFile(agyPath, []byte(script), 0o700); err != nil { // #nosec G306 -- test writes an executable mock agy stub; the executable bit is required.
		t.Fatalf("write agy stub: %v", err)
	}
	cfg := config.Config{
		Agents: config.AgentsConfig{
			Antigravity: config.AntigravityConfig{Model: "  Configured Model  "},
		},
	}

	options := Resolve(cfg, "antigravity", KindModel, DiscoveryRequest{
		LookPath: func(name string) (string, error) {
			if name != "agy" {
				t.Fatalf("LookPath name = %q, want agy", name)
			}
			return agyPath, nil
		},
		Live:    true,
		Timeout: 30 * time.Second,
	})

	if !options.OverrideSupported {
		t.Fatal("Antigravity model override should be supported")
	}
	if options.Configured != "Configured Model" {
		t.Fatalf("configured = %q, want trimmed configured model", options.Configured)
	}
	want := []string{"Live Model", "Backup Model"}
	if !reflect.DeepEqual(options.Suggestions, want) {
		t.Fatalf("suggestions = %v, want %v", options.Suggestions, want)
	}
	if !options.AllowCustom {
		t.Fatal("Antigravity model should allow custom values")
	}
}

func TestValuesFallsBackToCatalogWhenLiveDisabled(t *testing.T) {
	got := Values("antigravity", KindModel, DiscoveryRequest{})
	want := config.FieldOptionValues(config.AntigravityModelFieldKey)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("values = %v, want catalog %v", got, want)
	}
}

func TestConfiguredValueTrimsKnownAgentFields(t *testing.T) {
	cfg := config.Config{
		Agents: config.AgentsConfig{
			Claude:      config.ClaudeConfig{Model: "  sonnet  ", ReasoningEffort: " high "},
			Codex:       config.CodexConfig{Model: " gpt-5.4 ", ReasoningEffort: " medium "},
			CopilotCLI:  config.AgentConfig{Model: " claude-sonnet-4.6 "},
			Antigravity: config.AntigravityConfig{Model: " Gemini 3.5 Flash (High) "},
		},
	}
	tests := []struct {
		agent string
		kind  Kind
		want  string
	}{
		{agent: "claude", kind: KindModel, want: "sonnet"},
		{agent: "claude", kind: KindReasoningEffort, want: "high"},
		{agent: "codex", kind: KindModel, want: "gpt-5.4"},
		{agent: "codex", kind: KindReasoningEffort, want: "medium"},
		{agent: "copilot_cli", kind: KindModel, want: "claude-sonnet-4.6"},
		{agent: "antigravity", kind: KindModel, want: "Gemini 3.5 Flash (High)"},
	}
	for _, tt := range tests {
		t.Run(tt.agent+"/"+string(tt.kind), func(t *testing.T) {
			if got := ConfiguredValue(cfg, tt.agent, tt.kind); got != tt.want {
				t.Fatalf("ConfiguredValue = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnsupportedFieldReturnsEmptyOptionSet(t *testing.T) {
	options := Resolve(config.Config{}, "antigravity", KindReasoningEffort, DiscoveryRequest{Live: true})
	if options.OverrideSupported {
		t.Fatal("Antigravity reasoning effort must not be override-supported")
	}
	if options.Configured != "" {
		t.Fatalf("configured = %q, want empty", options.Configured)
	}
	if len(options.Suggestions) != 0 {
		t.Fatalf("suggestions = %v, want empty", options.Suggestions)
	}
	if options.AllowCustom {
		t.Fatal("unsupported field must not allow custom values")
	}
}
