package config

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

// requiredFieldMigrationBaselineVersion marks the last release where required
// fields were introduced before config_set_default migration enforcement.
const requiredFieldMigrationBaselineVersion = "0.8.1"

// requiredFieldMigrationBaselineAllowlist contains required fields that predate
// migration-manifest coverage. Any required field not in this allowlist must be
// covered by at least one config_set_default migration operation.
var requiredFieldMigrationBaselineAllowlist = map[string]struct{}{
	"approvals.mode":             {},
	"agents.gemini.enabled":      {},
	"agents.claude.enabled":      {},
	"agents.codex.enabled":       {},
	"agents.vscode.enabled":      {},
	"agents.antigravity.enabled": {},
}

type migrationManifestForGuardrail struct {
	Operations []migrationOperationForGuardrail `json:"operations"`
}

type migrationOperationForGuardrail struct {
	Kind string `json:"kind"`
	Key  string `json:"key"`
}

func TestRequiredFieldsAddedAfterBaselineHaveConfigSetDefaultMigration(t *testing.T) {
	required := requiredFieldKeys()
	covered, err := migrationConfigSetDefaultKeys()
	if err != nil {
		t.Fatalf("load migration keys: %v", err)
	}

	missing := missingRequiredFieldMigrations(required, covered, requiredFieldMigrationBaselineAllowlist)
	if len(missing) > 0 {
		t.Fatalf(
			"required fields missing config_set_default migration coverage (baseline <= %s): %s",
			requiredFieldMigrationBaselineVersion,
			strings.Join(missing, ", "),
		)
	}

	for key := range requiredFieldMigrationBaselineAllowlist {
		if _, ok := required[key]; !ok {
			t.Fatalf("baseline allowlist key %q is not currently a required field", key)
		}
	}
}

func TestMissingRequiredFieldMigrations(t *testing.T) {
	required := map[string]struct{}{
		"a.required": {},
		"b.required": {},
		"c.required": {},
	}
	covered := map[string]struct{}{
		"b.required": {},
	}
	allowlist := map[string]struct{}{
		"a.required": {},
	}

	missing := missingRequiredFieldMigrations(required, covered, allowlist)
	want := []string{"c.required"}
	if strings.Join(missing, ",") != strings.Join(want, ",") {
		t.Fatalf("missingRequiredFieldMigrations=%v want %v", missing, want)
	}
}

func requiredFieldKeys() map[string]struct{} {
	out := make(map[string]struct{})
	for _, field := range Fields() {
		if !field.Required {
			continue
		}
		out[field.Key] = struct{}{}
	}
	return out
}

func migrationConfigSetDefaultKeys() (map[string]struct{}, error) {
	keys := make(map[string]struct{})
	err := templates.Walk("migrations", func(walkPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}

		data, err := templates.Read(path.Clean(walkPath))
		if err != nil {
			return fmt.Errorf("read %s: %w", walkPath, err)
		}
		var manifest migrationManifestForGuardrail
		if err := json.Unmarshal(data, &manifest); err != nil {
			return fmt.Errorf("decode %s: %w", walkPath, err)
		}
		for _, op := range manifest.Operations {
			if op.Kind != "config_set_default" {
				continue
			}
			key := strings.TrimSpace(op.Key)
			if key != "" {
				keys[key] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func missingRequiredFieldMigrations(required map[string]struct{}, covered map[string]struct{}, allowlist map[string]struct{}) []string {
	missing := make([]string, 0)
	for key := range required {
		if _, exempt := allowlist[key]; exempt {
			continue
		}
		if _, ok := covered[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}
