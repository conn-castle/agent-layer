package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestDisabledArtifactsExcludeNativeCodexCache(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".system/native", "legacy"} {
		path := filepath.Join(root, ".codex", "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(generatedFileMarker), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inst := &installer{root: root, sys: RealSystem{}}
	check, err := detectDisabledAgentArtifacts(inst, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Details) != 1 || check.Details[0] != "legacy-skills: .codex/skills/legacy/SKILL.md" {
		t.Fatalf("readiness inspected native cache: %#v", check)
	}
}
