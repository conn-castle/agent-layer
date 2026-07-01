package wizard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestMain(m *testing.M) {
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		return agentoptions.DiscoveryRequest{}
	}
	os.Exit(m.Run())
}

func TestPromptModelsAntigravityUsesLiveSharedOptions(t *testing.T) {
	orig := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = orig })

	binDir := t.TempDir()
	agyPath := filepath.Join(binDir, "agy")
	script := "#!/bin/sh\nif [ \"$1\" = \"models\" ]; then\n  printf 'Live Wizard Model\\nFallback Wizard Model\\n'\nfi\n"
	if err := os.WriteFile(agyPath, []byte(script), 0o700); err != nil { // #nosec G306 -- test writes an executable mock agy stub; the executable bit is required.
		t.Fatalf("write agy stub: %v", err)
	}
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		return agentoptions.DiscoveryRequest{
			LookPath: func(name string) (string, error) {
				if name != "agy" {
					t.Fatalf("LookPath name = %q, want agy", name)
				}
				return agyPath, nil
			},
			Live:    true,
			Timeout: 30 * time.Second,
		}
	}

	choices := NewChoices()
	choices.EnabledAgents[AgentAntigravity] = true
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			if title != messages.WizardAntigravityModelTitle {
				t.Fatalf("unexpected select title %q", title)
			}
			want := []string{messages.WizardLeaveBlankOption, "Live Wizard Model", "Fallback Wizard Model", messages.WizardCustomOption}
			if len(options) != len(want) {
				t.Fatalf("options = %v, want %v", options, want)
			}
			for i := range want {
				if options[i] != want[i] {
					t.Fatalf("options = %v, want %v", options, want)
				}
			}
			*current = "Live Wizard Model"
			return nil
		},
	}

	if err := promptModels(ui, choices); err != nil {
		t.Fatalf("promptModels error: %v", err)
	}
	if choices.AntigravityModel != "Live Wizard Model" {
		t.Fatalf("AntigravityModel = %q, want live selection", choices.AntigravityModel)
	}
}
