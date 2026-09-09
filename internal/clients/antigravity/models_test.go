package antigravity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDiscoverModelsPreservesContextErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agy")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil { // #nosec G306 -- executable harness fixture.
		t.Fatal(err)
	}
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := context.DeadlineExceeded
			if cancelled {
				cancel()
				want = context.Canceled
			}
			models, err := DiscoverModels(ModelOptionsRequest{
				Context: ctx, Timeout: 100 * time.Millisecond,
				LookPath: func(string) (string, error) { return path, nil },
			})
			if !errors.Is(err, want) || len(models) != 0 {
				t.Fatalf("models=%v err=%v, want %v", models, err, want)
			}
		})
	}
}

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
printf '\nmedium-id\tGemini 3.5 Flash (Medium)\nhigh-id\tGemini 3.5 Flash (High)\n'
`
	if err := os.WriteFile(agyPath, []byte(script), 0o700); err != nil { // #nosec G306 -- test writes an executable mock agy stub; the executable bit is required.
		t.Fatalf("write agy stub: %v", err)
	}
	models, err := DiscoverModels(ModelOptionsRequest{
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
	want := []string{"medium-id", "high-id"}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
}

func TestDiscoverModelsReportsMissingBinary(t *testing.T) {
	_, err := DiscoverModels(ModelOptionsRequest{
		LookPath: func(string) (string, error) {
			return "", os.ErrNotExist
		},
	})
	if err == nil {
		t.Fatal("missing binary was not reported")
	}
}

func TestDiscoverModelsReportsCommandFailure(t *testing.T) {
	binDir := t.TempDir()
	agyPath := filepath.Join(binDir, "agy")
	if err := os.WriteFile(agyPath, []byte("#!/bin/sh\nexit 42\n"), 0o700); err != nil { // #nosec G306 -- test writes an executable mock agy stub; the executable bit is required.
		t.Fatalf("write agy stub: %v", err)
	}
	_, err := DiscoverModels(ModelOptionsRequest{
		LookPath: func(string) (string, error) {
			return agyPath, nil
		},
	})
	if err == nil {
		t.Fatal("command failure was not reported")
	}
}

func TestParseModelRows(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		want         []string
		invalid      bool
	}{
		{"current", "slug\tDisplay Name\nsecond\tOther Model\n", []string{"slug", "second"}, false},
		{"unstructured output", "\nAuthentication failed\n", nil, true},
		{"missing label", "slug\t\n", nil, true},
		{"missing slug", "\tDisplay Name\n", nil, true},
		{"extra column", "slug\tDisplay\textra\n", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models, err := ParseModelOutput([]byte(tc.output))
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid row accepted")
				}
				return
			}
			if err != nil || !reflect.DeepEqual(models, tc.want) {
				t.Fatalf("models=%v err=%v", models, err)
			}
		})
	}
}
