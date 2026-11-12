package herdr

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Execute the reported and persisted argv, including generation annotation,
// through the real env binary. Comparing argv alone misses env option parsing.
func TestReportedRecoveryEnvironmentRecipesExecute(t *testing.T) {
	envPath, err := exec.LookPath(commandEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, provider string
		dev            bool
		wantArgs       []string
	}{
		{"Claude unset home and generation", providerClaude, false, []string{"claude", "--resume", "fixture-id"}},
		{"Codex absolute home and generation", providerCodex, false, []string{"codex", "resume", "fixture-id"}},
		{"Claude unset home with dev bypass", providerClaude, true, []string{"claude", "--resume", "fixture-id"}},
		{"Codex absolute home with dev bypass", providerCodex, true, []string{"codex", "resume", "fixture-id"}},
		{"Muse annotation", providerMuse, false, []string{"muse", "resume", "fixture-id"}},
		{"Muse annotation with dev bypass", providerMuse, true, []string{"muse", "resume", "fixture-id"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket, reports, done := fakeHerdR(t, fakeHerdROptions{})
			binDir := t.TempDir()
			devExecutable := filepath.Join(binDir, "dev-al")
			script := `printf 'CODEX_HOME=%s\nCLAUDE_CONFIG_DIR=%s\nGENERATION=%s\nDEV_BYPASS=%s\n' "${CODEX_HOME-<unset>}" "${CLAUDE_CONFIG_DIR-<unset>}" "${AL_HERDR_RECOVERY_GENERATION-<unset>}" "${AL_DEV_BYPASS_VERSION_DISPATCH-<unset>}"
printf 'arg=%s\n' "$@"
`
			// The prefix distinguishes a release al from the selected dev executable.
			for name, launcher := range map[string]string{"al": "release", "dev-al": "dev"} {
				content := "#!/bin/sh\nprintf 'launcher=" + launcher + "\\n'\n" + script
				if err := os.WriteFile(filepath.Join(binDir, name), []byte(content), 0o700); err != nil { // #nosec G306 -- executable fixture in a test-owned directory.
					t.Fatal(err)
				}
			}
			home := ""
			launch := launchContext{Provider: tc.provider}
			if tc.provider == providerCodex {
				home = filepath.Join(t.TempDir(), "selected-home")
			}
			if tc.provider != providerMuse {
				launch.ProviderHome = &home
			}
			values := map[string]string{EnvSocketPath: socket, EnvPaneID: "w1:p1"}
			if tc.dev {
				values[EnvDevBypass], values[EnvDevExecutable] = "1", devExecutable
			}
			spec := providers[tc.provider]
			argv, err := resumeArgv(spec, "fixture-id", values)
			if err != nil {
				t.Fatal(err)
			}
			argv = withRecoveryGeneration(withLaunchHome(argv, launch), "fixture-generation")
			if err := reportAndVerify(spec, "fixture-id", argv, values, time.Now().Add(3*time.Second), "", nil); err != nil {
				t.Fatal(err)
			}
			waitFakeHerdR(t, done)
			params := (<-reports)["params"].(map[string]any)
			reported := interfaceStrings(params[requestResumeArgvKey])
			if !slices.Equal(reported, argv) {
				t.Fatalf("reported=%v, want %v", reported, argv)
			}
			path, err := sessionPath(socket)
			if err != nil {
				t.Fatal(err)
			}
			if stored, err := storedResumeAt(path, "w1:p1", spec, reported); err != nil || !stored {
				t.Fatalf("reported recipe did not match persisted recipe: %t, %v", stored, err)
			}
			changed := slices.Clone(reported)
			changed[len(changed)-1] = "different-session"
			if stored, err := storedResumeAt(path, "w1:p1", spec, changed); err != nil || stored {
				t.Fatalf("changed recipe matched persisted recipe: %t, %v", stored, err)
			}
			if len(reported) == 0 || reported[0] != commandEnv {
				t.Fatalf("expected env recipe: %v", reported)
			}
			command := exec.Command(envPath, reported[1:]...) // #nosec G702 -- test-generated recovery argv and real env executable.
			command.Dir = t.TempDir()
			command.Env = []string{"HOME=" + t.TempDir(), "PATH=" + binDir + ":/usr/bin:/bin", "CODEX_HOME=conflicting-codex", "CLAUDE_CONFIG_DIR=conflicting-claude"}
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("execute reported recipe %v: %v\n%s", reported, err, output)
			}
			wantCodex, wantClaude := "conflicting-codex", "conflicting-claude"
			switch tc.provider {
			case providerClaude:
				wantClaude = "<unset>"
			case providerCodex:
				wantCodex = home
			}
			launcher, bypass := "release", "<unset>"
			if tc.dev {
				launcher, bypass = "dev", "1"
			}
			want := "launcher=" + launcher + "\nCODEX_HOME=" + wantCodex + "\nCLAUDE_CONFIG_DIR=" + wantClaude + "\nGENERATION=fixture-generation\nDEV_BYPASS=" + bypass + "\narg=" + strings.Join(tc.wantArgs, "\narg=") + "\n"
			if string(output) != want {
				t.Fatalf("recovered environment and argv:\n%s\nwant:\n%s", output, want)
			}
		})
	}
}
