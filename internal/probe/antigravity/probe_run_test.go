package antigravity

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const headlessPermissionDenial = `no output produced — a tool required the "command" permission that headless mode cannot prompt for`

const healthyFakeAgyBody = `mkdir -p "$log_dir"
cat > "$log_dir/cli.log" <<'LOG'
I0101 00:00:00.000000 1 cli_setting_manager.go:42] CLI settings initialized: permissions=allow[command(echo PROBEALLOWMARKER)]
I0101 00:00:00.000000 1 migrate.go:17] Migrating file /cfg/antigravity-cli/mcp_config.json to /cfg/config/mcp_config.json
I0101 00:00:00.000000 1 discovery.go:88] MCP server probe-mcp registered
LOG
printf '%s' "PROBEMCPTOOL33" > "$AL_PROBE_MCP_MARKER"
echo "INSTRUCTIONMARKER88 WORKSPACEALLOWMARKER"
echo "skills: probe-marker-skill, shared-tier-dup, global-only-skill"
echo "mcp servers: probe-mcp"
`

// installFakeAgy puts an `agy` stand-in on PATH whose run body is supplied by
// the caller, so a probe can be driven over the same command line, workspace,
// and log directory the real client is given. The stand-in reproduces the
// original empty-stdout permission denial unless both probe-only flags are
// present, and records the print-mode argv for process-boundary assertions.
func installFakeAgy(t *testing.T, runBody string) string {
	t.Helper()
	binDir := t.TempDir()
	argsFile := filepath.Join(binDir, "last-print-args")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "agy 1.2.3-fake"
  exit 0
fi
gemini_dir=""
has_skip=0
has_sandbox=0
: > "` + argsFile + `"
for arg in "$@"; do
  printf '%s\n' "$arg" >> "` + argsFile + `"
  case "$arg" in
    --gemini_dir=*) gemini_dir="${arg#--gemini_dir=}" ;;
    --dangerously-skip-permissions) has_skip=1 ;;
    --sandbox) has_sandbox=1 ;;
  esac
done
log_dir="$gemini_dir/antigravity-cli/log"
if [ "$has_skip" != 1 ] || [ "$has_sandbox" != 1 ]; then
  echo "no output produced — a tool required the \"command\" permission that headless mode cannot prompt for" >&2
  exit 0
fi
` + runBody
	path := filepath.Join(binDir, "agy")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { // #nosec G306 -- the fake client must be executable.
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

func readFakeAgyArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	data, err := os.ReadFile(argsFile) // #nosec G304 -- test-owned fake argv file.
	if err != nil {
		t.Fatalf("read fake agy args: %v", err)
	}
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func assertHeadlessPermissionDenial(t *testing.T, agyPath string, args []string) {
	t.Helper()
	cmd := exec.Command(agyPath, args...) // #nosec G204 -- test-owned fake client and argv.
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("denial run failed: %v\nstderr: %s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.Bytes())
	}
	if got := strings.TrimSpace(stderr.String()); got != headlessPermissionDenial {
		t.Fatalf("stderr = %q, want %q", got, headlessPermissionDenial)
	}
}

// TestProbeReportsObservedCapabilitiesFromRealArtifacts covers the probe's
// reason to exist: the capability matrix must be derived from artifacts the
// client actually produced in the seeded workspace — its log, its stdout, and
// the MCP fixture it invoked — never assumed. A run that produces those
// artifacts is the only thing that may report live MCP support.
func TestProbeReportsObservedCapabilitiesFromRealArtifacts(t *testing.T) {
	installFakeAgy(t, healthyFakeAgyBody)

	result, err := Probe(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("probe of a healthy client failed: %v", err)
	}
	if result.ExitCode != 0 || result.TimedOut || result.Error != "" {
		t.Fatalf("healthy run reported exit=%d timedOut=%t error=%q", result.ExitCode, result.TimedOut, result.Error)
	}
	if result.AgyVersion != "agy 1.2.3-fake" {
		t.Fatalf("agy version = %q, want the version the client reported", result.AgyVersion)
	}
	want := CapabilityMatrix{
		PermissionsLoaded:        true,
		MCPConfigMigrated:        true,
		MCPRuntimeDiscovery:      true,
		MCPToolInvoked:           true,
		WorkspacePermissionsRead: true,
		InstructionsLoaded:       true,
		SkillNamesVisible:        true,
		MCPConfigNamesVisible:    true,
		SharedSkillDedupObserved: true,
	}
	if result.Capabilities != want {
		t.Fatalf("capabilities = %#v, want %#v", result.Capabilities, want)
	}
	if len(result.Evidence) != 9 {
		t.Fatalf("evidence = %#v, want one entry per observed capability", result.Evidence)
	}

	// The probe must hand the client a workspace that can distinguish the
	// capabilities it reports; without these seeds the matrix means nothing.
	for _, seeded := range []string{
		filepath.Join(result.WorkspaceDir, "AGENTS.md"),
		filepath.Join(result.WorkspaceDir, ".agents", "mcp_config.json"),
		filepath.Join(result.WorkspaceDir, ".agents", "skills", "shared-tier-dup", "SKILL.md"),
		filepath.Join(result.AgyConfigDir, "skills", "shared-tier-dup", "SKILL.md"),
		filepath.Join(result.AgyConfigDir, "antigravity-cli", "mcp_config.json"),
		filepath.Join(result.AgyConfigDir, "antigravity-cli", "settings.json"),
	} {
		if _, err := os.Stat(seeded); err != nil {
			t.Fatalf("probe workspace is missing %s: %v", seeded, err)
		}
	}
	if result.LogPath != filepath.Join(result.AgyConfigDir, "antigravity-cli", "log", "cli.log") {
		t.Fatalf("log path = %q, want the client log the probe read", result.LogPath)
	}
	stdout, err := os.ReadFile(filepath.Join(result.ProbeDir, "stdout.txt"))
	if err != nil || !strings.Contains(string(stdout), "INSTRUCTIONMARKER88") {
		t.Fatalf("probe did not retain the client stdout it parsed: %v", err)
	}
	settings, err := os.ReadFile(filepath.Join(result.AgyConfigDir, "antigravity-cli", "settings.json")) // #nosec G304 -- probe-owned fixture path.
	if err != nil {
		t.Fatalf("read seeded settings: %v", err)
	}
	if !strings.Contains(string(settings), "PROBEALLOWMARKER") || !strings.Contains(string(settings), `"command(rm -rf)"`) {
		t.Fatalf("seeded allow/deny markers missing from settings.json:\n%s", settings)
	}
}

// TestProbeHeadlessRunRequiresAutoApprovalAndSandbox pins the process contract
// that makes the fixed prompt runnable without an interactive prompt: the
// mock reproduces agy 1.1.8's empty-stdout permission denial unless both
// probe-only flags are present, and Probe must pass those exact arguments.
func TestProbeHeadlessRunRequiresAutoApprovalAndSandbox(t *testing.T) {
	argsFile := installFakeAgy(t, healthyFakeAgyBody)
	agyPath, err := exec.LookPath("agy")
	if err != nil {
		t.Fatalf("look up fake agy: %v", err)
	}

	printArgs := []string{"--gemini_dir=" + t.TempDir(), "--print-timeout=30s", "--print", probePrompt}
	t.Run("missing both flags", func(t *testing.T) {
		assertHeadlessPermissionDenial(t, agyPath, printArgs)
	})
	t.Run("skip permissions without sandbox", func(t *testing.T) {
		assertHeadlessPermissionDenial(t, agyPath, append([]string{"--dangerously-skip-permissions"}, printArgs...))
	})
	t.Run("sandbox without skip permissions", func(t *testing.T) {
		assertHeadlessPermissionDenial(t, agyPath, append([]string{"--sandbox"}, printArgs...))
	})

	result, err := Probe(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("probe of a healthy client failed: %v", err)
	}
	if result.ExitCode != 0 || result.TimedOut || result.Error != "" {
		t.Fatalf("healthy run reported exit=%d timedOut=%t error=%q", result.ExitCode, result.TimedOut, result.Error)
	}
	if !result.Capabilities.InstructionsLoaded {
		t.Fatalf("probe with both flags still produced no stdout-derived capabilities: %#v", result.Capabilities)
	}

	args := readFakeAgyArgs(t, argsFile)
	for _, want := range []string{
		"--dangerously-skip-permissions",
		"--sandbox",
		"--print-timeout=30s",
		"--print",
		probePrompt,
		"--gemini_dir=" + result.AgyConfigDir,
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("probe argv %q missing %q", args, want)
		}
	}
}

// TestProbeReportsFailedRunsWithoutClaimingCapabilities covers the other half
// of the contract: a client that fails still yields a report, and that report
// must carry the failure rather than an empty success. Claiming capabilities
// from a failed run would be worse than reporting nothing.
func TestProbeReportsFailedRunsWithoutClaimingCapabilities(t *testing.T) {
	installFakeAgy(t, `echo "client exploded" >&2
exit 3
`)

	result, err := Probe(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("probe surfaced a client failure as a probe failure: %v", err)
	}
	if result.ExitCode != 3 {
		t.Fatalf("exit code = %d, want the client's own exit status", result.ExitCode)
	}
	if !strings.Contains(result.Error, "exit status 3") || !strings.Contains(result.Error, "no antigravity log") {
		t.Fatalf("error = %q, want both the run failure and the missing log recorded", result.Error)
	}
	if result.Capabilities != (CapabilityMatrix{}) {
		t.Fatalf("failed run claimed capabilities: %#v", result.Capabilities)
	}
	stderr, err := os.ReadFile(filepath.Join(result.ProbeDir, "stderr.txt"))
	if err != nil || !strings.Contains(string(stderr), "client exploded") {
		t.Fatalf("probe did not retain the failing client's stderr: %v", err)
	}
}

func TestProbeRequiresATemporaryRootAndAnInstalledClient(t *testing.T) {
	installFakeAgy(t, "exit 0\n")
	if _, err := Probe(context.Background(), ""); err == nil {
		t.Fatal("probe accepted an empty temporary root")
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := Probe(context.Background(), t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "requires agy on PATH") {
		t.Fatalf("probe without an installed client = %v", err)
	}
}
