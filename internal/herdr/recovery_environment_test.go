package herdr

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func recoveryExecutables(t *testing.T) (binDir, devExecutable string) {
	t.Helper()
	binDir = t.TempDir()
	script := `printf 'CODEX_HOME=%s\nCLAUDE_CONFIG_DIR=%s\nGROK_HOME=%s\nAL_HERDR_RECOVERY_GENERATION=%s\nAL_DEV_BYPASS_VERSION_DISPATCH=%s\n' "${CODEX_HOME-<unset>}" "${CLAUDE_CONFIG_DIR-<unset>}" "${GROK_HOME-<unset>}" "${AL_HERDR_RECOVERY_GENERATION-<unset>}" "${AL_DEV_BYPASS_VERSION_DISPATCH-<unset>}"
printf 'arg=%s\n' "$@"
`
	for name, launcher := range map[string]string{"al": "release", "dev-al": "dev"} {
		content := "#!/bin/sh\nprintf 'launcher=" + launcher + "\\n'\n" + script
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(content), 0o700); err != nil { // #nosec G306 -- executable fixture in a test-owned directory.
			t.Fatal(err)
		}
	}
	return binDir, filepath.Join(binDir, "dev-al")
}

// Execute the argv actually reported by HandleForRoot, rather than rebuilding
// its helper composition. Recovery starts elsewhere with conflicting homes.
func assertRecoveryExecutes(t *testing.T, argv []string, binDir string, wantEnv map[string]string, wantArgs []string, dev bool) {
	t.Helper()
	path := argv[0]
	switch path {
	case commandEnv:
		var err error
		path, err = exec.LookPath(commandEnv)
		if err != nil {
			t.Fatal(err)
		}
	case "al":
		path = filepath.Join(binDir, "al")
	}
	command := exec.Command(path, argv[1:]...) // #nosec G702 -- production-reported argv and test-owned executable fixtures.
	command.Dir = t.TempDir()
	command.Env = []string{"HOME=" + t.TempDir(), "PATH=" + binDir + ":/usr/bin:/bin", "CODEX_HOME=conflicting-codex", "CLAUDE_CONFIG_DIR=conflicting-claude", "GROK_HOME=conflicting-grok"}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("execute reported recipe %v: %v\n%s", argv, err, output)
	}
	launcher := "release"
	if dev {
		launcher = "dev"
	}
	want := "launcher=" + launcher + "\n"
	for _, key := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR", "GROK_HOME", envRecoveryGeneration, EnvDevBypass} {
		want += key + "=" + wantEnv[key] + "\n"
	}
	want += "arg=" + strings.Join(wantArgs, "\narg=") + "\n"
	if string(output) != want {
		t.Fatalf("recovered environment and argv:\n%s\nwant:\n%s", output, want)
	}
}
