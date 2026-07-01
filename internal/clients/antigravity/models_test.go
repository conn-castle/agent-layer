package antigravity

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestLiveModelOptionsParsesAgyModels(t *testing.T) {
	binDir := t.TempDir()
	agyPath := filepath.Join(binDir, "agy")
	script := `#!/bin/sh
if [ "$1" != "models" ]; then
  exit 2
fi
if [ "$AGY_CLI_DISABLE_AUTO_UPDATE" != "1" ]; then
  exit 3
fi
printf '\nGemini 3.5 Flash (Medium)\nGemini 3.5 Flash (High)\n'
`
	if err := os.WriteFile(agyPath, []byte(script), 0o700); err != nil { // #nosec G306 -- test writes an executable mock agy stub; the executable bit is required.
		t.Fatalf("write agy stub: %v", err)
	}
	models, err := liveModelOptions(ModelOptionsRequest{
		Env: []string{"PATH=/bin"},
		LookPath: func(name string) (string, error) {
			if name != "agy" {
				t.Fatalf("LookPath name = %q, want agy", name)
			}
			return agyPath, nil
		},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("liveModelOptions error: %v", err)
	}
	want := []string{"Gemini 3.5 Flash (Medium)", "Gemini 3.5 Flash (High)"}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
}

func TestModelOptionsFallsBackToFieldCatalogWhenAgyUnavailable(t *testing.T) {
	models := ModelOptions(ModelOptionsRequest{
		LookPath: func(string) (string, error) {
			return "", os.ErrNotExist
		},
	})
	want := config.FieldOptionValues(config.AntigravityModelFieldKey)
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want catalog %v", models, want)
	}
}

func TestModelOptionsFallsBackToFieldCatalogWhenLiveCommandFails(t *testing.T) {
	binDir := t.TempDir()
	agyPath := filepath.Join(binDir, "agy")
	if err := os.WriteFile(agyPath, []byte("#!/bin/sh\nexit 42\n"), 0o700); err != nil { // #nosec G306 -- test writes an executable mock agy stub; the executable bit is required.
		t.Fatalf("write agy stub: %v", err)
	}
	models := ModelOptions(ModelOptionsRequest{
		LookPath: func(string) (string, error) {
			return agyPath, nil
		},
	})
	want := config.FieldOptionValues(config.AntigravityModelFieldKey)
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want catalog %v", models, want)
	}
}
