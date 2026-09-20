package install

import (
	"bytes"
	"os"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestMuseUpgradeEnablement(t *testing.T) {
	for _, tc := range []struct {
		name, existing            string
		interactive, choose, want bool
		prompts                   int
	}{
		{name: "noninteractive defaults disabled"},
		{name: "enable", interactive: true, choose: true, want: true, prompts: 1},
		{name: "decline", interactive: true, prompts: 1},
		{name: "preserve enabled", existing: "[agents.muse]\nenabled = true\n", interactive: true, want: true},
		{name: "preserve disabled", existing: "[agents.muse]\nenabled = false\n", interactive: true, choose: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			base := `[approvals]
mode = "all"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false
[agents.grok]
enabled = false
`
			path := writeMigrationConfigForTest(t, root, base+tc.existing)
			writePinForTest(t, root, "0.21.1")
			var warn bytes.Buffer
			inst := &installer{root: root, pinVersion: "0.22.0", sys: RealSystem{}, warnWriter: &warn}
			prompts := 0
			if tc.interactive {
				inst.prompter = PromptFuncs{ConfigSetDefaultFunc: func(key string, value any, _ string, field *config.FieldDef) (any, error) {
					prompts++
					if key != "agents.muse.enabled" || value != false || field == nil {
						t.Fatalf("unexpected choice: %s %v %v", key, value, field)
					}
					return tc.choose, nil
				}}
			}
			if err := inst.prepareUpgradeMigrations(); err != nil {
				t.Fatal(err)
			}
			if err := inst.runMigrations(); err != nil {
				t.Fatal(err)
			}
			if prompts != tc.prompts {
				t.Fatalf("prompts = %d, want %d", prompts, tc.prompts)
			}
			data, err := os.ReadFile(path) // #nosec G304 -- test-controlled configuration path.
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.ParseConfig(data, path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Agents.Muse.Enabled == nil || *cfg.Agents.Muse.Enabled != tc.want {
				t.Fatalf("Muse enabled = %v, want %v", cfg.Agents.Muse.Enabled, tc.want)
			}
			// Reapplying the migration must preserve the persisted choice without prompting.
			if err := inst.runMigrations(); err != nil {
				t.Fatal(err)
			}
			if prompts != tc.prompts {
				t.Fatalf("repeat migration prompted again: %d", prompts)
			}
		})
	}
}
