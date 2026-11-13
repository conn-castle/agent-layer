package clients_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/claude"
	"github.com/conn-castle/agent-layer/internal/clients/codex"
	"github.com/conn-castle/agent-layer/internal/clients/grok"
	"github.com/conn-castle/agent-layer/internal/config"
)

func TestDanglingCallerHomeShorthand(t *testing.T) {
	for _, provider := range []struct {
		key, name string
		enabled   []bool
		configure func(string, []string, bool, io.Writer) []string
	}{
		{"CODEX_HOME", ".codex", []bool{true, false}, func(root string, env []string, enabled bool, warning io.Writer) []string {
			return codex.ConfigureEnvironment(root, env, config.CodexConfig{LocalConfigDir: &enabled}, warning)
		}},
		{"CLAUDE_CONFIG_DIR", ".claude-config", []bool{true, false}, func(root string, env []string, enabled bool, warning io.Writer) []string {
			return claude.ConfigureEnvironment(root, env, config.ClaudeConfig{LocalConfigDir: &enabled}, warning)
		}},
		{"GROK_HOME", ".grok-config", []bool{false}, func(root string, env []string, _ bool, _ io.Writer) []string {
			return grok.ClearStaleHome(root, env)
		}},
	} {
		for _, enabled := range provider.enabled {
			for _, spelling := range []struct{ value, suffix string }{
				{"%s", "%s"},
				{"./%s", "%s"},
				{"%s/", "%s"},
				{".//%s", "%s"},
				{"%s/.", "%s/."},
				{"././/%s//././", "%s/./."},
			} {
				value := fmt.Sprintf(spelling.value, provider.name)
				t.Run(fmt.Sprintf("%s/enabled=%t/%s", provider.key, enabled, value), func(t *testing.T) {
					root := t.TempDir()
					caller := filepath.Join(root, "caller")
					if err := os.Mkdir(caller, 0o700); err != nil {
						t.Fatal(err)
					}
					target := filepath.Join(root, "missing-home")
					entry := filepath.Join(caller, provider.name)
					if err := os.Symlink(target, entry); err != nil {
						t.Fatal(err)
					}
					physicalCaller, err := filepath.EvalSymlinks(caller)
					if err != nil {
						t.Fatal(err)
					}
					t.Chdir(caller)
					var warning strings.Builder
					env := provider.configure(root, []string{provider.key + "=" + value}, enabled, &warning)
					got, present := clients.GetEnv(env, provider.key)
					// Keep unresolved dot components after the dangling link; they
					// must not cause selection of the repository home instead.
					want := physicalCaller + "/" + fmt.Sprintf(spelling.suffix, provider.name)
					if !present || got != want {
						t.Fatalf("home=%q (present=%t), want %q", got, present, want)
					}
					if (warning.Len() > 0) != enabled {
						t.Fatalf("warning=%q, enabled=%t", warning.String(), enabled)
					}
					if provider.key == "CODEX_HOME" {
						if err := codex.EnsureSelectedHome(root, env, config.CodexConfig{LocalConfigDir: &enabled}); err != nil {
							t.Fatal(err)
						}
					}
					if link, err := os.Readlink(entry); err != nil || link != target {
						t.Fatalf("caller home symlink changed: target=%q err=%v", link, err)
					}
					for _, path := range []string{target, filepath.Join(root, provider.name)} {
						if _, err := os.Lstat(path); !os.IsNotExist(err) {
							t.Fatalf("unselected home %q was created: %v", path, err)
						}
					}
				})
			}
		}
	}
}
