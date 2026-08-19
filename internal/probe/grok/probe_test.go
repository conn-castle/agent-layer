package grok

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	clientgrok "github.com/conn-castle/agent-layer/internal/clients/grok"
	probeantigravity "github.com/conn-castle/agent-layer/internal/probe/antigravity"
	"github.com/conn-castle/agent-layer/internal/tomlpatch"
)

func TestProbeRequiresATemporaryRootAndAnInstalledClient(t *testing.T) {
	if _, err := Probe(context.Background(), "", ""); err == nil || !strings.Contains(err.Error(), "temporary root") {
		t.Fatalf("expected tmp root error, got %v", err)
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := Probe(context.Background(), t.TempDir(), ""); err == nil || !strings.Contains(err.Error(), "grok on PATH") {
		t.Fatalf("expected missing grok error, got %v", err)
	}
}

func TestProbeReportsObservedCapabilitiesFromMockClient(t *testing.T) {
	authHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(authHome, "auth.json"), []byte(`{"token":"test-only"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	stub := filepath.Join(binDir, "grok")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 'grok 1.0.5 (deadbeef) [stable]'; exit 0; fi\n" +
		"args=\" $* \"\n" +
		"for required in ' --no-auto-update ' ' --trust ' ' --sandbox workspace ' ' --always-approve ' ' --no-memory ' ' --no-subagents ' ' --output-format streaming-json ' ' --prompt-file '; do\n" +
		"  case \"$args\" in *\"$required\"*) ;; *) exit 3 ;; esac\n" +
		"done\n" +
		"case \"$args\" in *' --max-turns '*) exit 3 ;; esac\n" +
		"test -f \"$GROK_HOME/auth.json\" || exit 4\n" +
		"echo '{\"type\":\"text\",\"data\":\"ok\"}'\n" +
		"echo '{\"type\":\"end\",\"stopReason\":\"end_turn\",\"sessionId\":\"probe-session\",\"requestId\":\"probe-request\"}'\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil { // #nosec G306 -- test stub.
		t.Fatalf("write grok stub: %v", err)
	}
	t.Setenv("PATH", binDir)

	tmpRoot := t.TempDir()
	result, err := Probe(context.Background(), tmpRoot, authHome)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit = %d error=%q", result.ExitCode, result.Error)
	}
	if !strings.Contains(result.GrokVersion, "1.0.5") {
		t.Fatalf("version = %q", result.GrokVersion)
	}
	if result.GrokHomeDir == "" || !strings.HasPrefix(result.GrokHomeDir, result.ProbeDir) {
		t.Fatalf("grok home = %q probe dir = %q", result.GrokHomeDir, result.ProbeDir)
	}
	if !result.Capabilities.StreamingJSONUsed {
		t.Fatalf("capabilities = %+v", result.Capabilities)
	}
	if result.Capabilities.MCPToolInvoked {
		t.Fatal("mock grok must not claim MCP tool invocation")
	}
	if _, err := os.Stat(filepath.Join(result.GrokHomeDir, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("disposable credentials remain after probe: %v", err)
	}
}

func TestPreferredAuthHomeUsesExplicitThenRepoThenUserHome(t *testing.T) {
	repo := t.TempDir()
	userHome := t.TempDir()
	explicit := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv(clientgrok.EnvHome, "")

	writeAuth := func(home string) {
		t.Helper()
		if err := os.MkdirAll(home, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"token":"test-only"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	userGrokHome := filepath.Join(userHome, ".grok")
	writeAuth(userGrokHome)
	if got := PreferredAuthHome(repo); got != userGrokHome {
		t.Fatalf("user auth home = %q, want %q", got, userGrokHome)
	}
	repoHome := clientgrok.HomeDir(repo)
	writeAuth(repoHome)
	if got := PreferredAuthHome(repo); got != repoHome {
		t.Fatalf("repo auth home = %q, want %q", got, repoHome)
	}
	writeAuth(explicit)
	t.Setenv(clientgrok.EnvHome, explicit)
	if got := PreferredAuthHome(repo); got != explicit {
		t.Fatalf("explicit auth home = %q, want %q", got, explicit)
	}
}

func TestValidateStreamingJSONRejectsInvalidContracts(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		want   string
	}{
		{name: "substring only", stream: `not json but has "type"`, want: "decode streaming-json"},
		{name: "malformed JSON", stream: `{"type":"text"`, want: "decode streaming-json"},
		{name: "missing terminal", stream: `{"type":"text","data":"partial"}`, want: "omitted a terminal end event"},
		{name: "incompatible terminal", stream: `{"type":"end","stopReason":"refusal","sessionId":"probe"}`, want: `incompatible stop reason "refusal"`},
		{name: "missing session", stream: `{"type":"end","stopReason":"end_turn"}`, want: "omitted sessionId"},
		{name: "empty", stream: "\n", want: "output was empty"},
		{name: "missing type", stream: `{}`, want: "has no string type"},
		{name: "error event", stream: `{"type":"error","message":"failed"}`, want: "reported an error event"},
		{name: "after terminal", stream: "{\"type\":\"end\",\"stopReason\":\"end_turn\",\"sessionId\":\"probe\"}\n{\"type\":\"text\"}", want: "event found after terminal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStreamingJSON(strings.NewReader(tt.stream))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateStreamingJSON() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateStreamingJSONRejectsUndocumentedPascalCaseReason(t *testing.T) {
	stream := `{"type":"end","stopReason":"EndTurn","sessionId":"probe"}`
	if err := validateStreamingJSON(strings.NewReader(stream)); err == nil {
		t.Fatal("undocumented completed-turn spelling accepted")
	}
}

func TestValidateStreamingJSONAllowsNonExhaustiveProgressEvents(t *testing.T) {
	stream := "{\"type\":\"future_progress_event\",\"data\":{}}\n" +
		"{\"type\":\"end\",\"stopReason\":\"end_turn\",\"sessionId\":\"probe\"}\n"
	if err := validateStreamingJSON(strings.NewReader(stream)); err != nil {
		t.Fatalf("valid forward-compatible stream rejected: %v", err)
	}
}

func TestValidateStreamingJSONAcceptsLargeEvents(t *testing.T) {
	stream := `{"type":"text","data":"` + strings.Repeat("x", 128*1024) + `"}` + "\n" +
		`{"type":"end","stopReason":"end_turn","sessionId":"probe"}` + "\n"
	if err := validateStreamingJSON(strings.NewReader(stream)); err != nil {
		t.Fatalf("large valid event rejected: %v", err)
	}
}

func TestSeedGrokProbeWorkspaceWritesTrustAndMCP(t *testing.T) {
	probeDir := t.TempDir()
	workspace, home, promptPath, markerPath, err := seedGrokProbeWorkspace(probeDir)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".grok", "config.toml")); err != nil {
		t.Fatalf("missing project config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "trusted_folders.toml")); err != nil {
		t.Fatalf("missing trust file: %v", err)
	}
	if _, err := os.Stat(promptPath); err != nil {
		t.Fatalf("missing prompt: %v", err)
	}
	if inspectFixtureMarker(markerPath) {
		t.Fatal("marker must start absent")
	}
	if err := os.WriteFile(markerPath, []byte(probeantigravity.FixtureToolReply+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !inspectFixtureMarker(markerPath) {
		t.Fatal("valid marker was not recognized")
	}
}

func TestSeedGrokProbeWorkspaceTrustsCanonicalPath(t *testing.T) {
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedRoot := filepath.Join(parent, "linked")
	if err := os.Symlink(realRoot, linkedRoot); err != nil {
		t.Fatal(err)
	}
	probeDir := filepath.Join(linkedRoot, "probe")
	if err := os.Mkdir(probeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, home, _, _, err := seedGrokProbeWorkspace(probeDir)
	if err != nil {
		t.Fatal(err)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "trusted_folders.toml")) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), tomlpatch.FormatKey(canonicalWorkspace)) {
		t.Fatalf("trust file does not contain canonical workspace %q:\n%s", canonicalWorkspace, data)
	}
}

func TestProbeReportsInvalidSuccessfulStream(t *testing.T) {
	binDir := t.TempDir()
	stub := filepath.Join(binDir, "grok")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then exit 1; fi\n" +
		"echo 'not streaming json'\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil { // #nosec G306 -- test stub.
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	result, err := Probe(context.Background(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Error == "" || result.Capabilities.StreamingJSONUsed || result.GrokVersion != "" {
		t.Fatalf("invalid stream result = %+v", result)
	}
}

func TestCommandExitCode(t *testing.T) {
	if got := commandExitCode(context.Background(), nil); got != 0 {
		t.Fatalf("success exit code = %d", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := commandExitCode(ctx, errors.New("cancelled")); got != 124 {
		t.Fatalf("cancelled exit code = %d", got)
	}
	cmd := exec.Command("/bin/sh", "-c", "exit 7")
	err := cmd.Run()
	if got := commandExitCode(context.Background(), err); got != 7 {
		t.Fatalf("process exit code = %d, err %v", got, err)
	}
	if got := commandExitCode(context.Background(), errors.New("launch")); got != -1 {
		t.Fatalf("launch exit code = %d", got)
	}
}

func TestSeedGrokProbeWorkspaceReportsOwnedPathConflicts(t *testing.T) {
	tests := []struct {
		name string
		path string
		file bool
	}{
		{name: "workspace directory", path: "workspace", file: true},
		{name: "project config", path: filepath.Join("workspace", ".grok", "config.toml")},
		{name: "instructions", path: filepath.Join("workspace", "AGENTS.md")},
		{name: "prompt", path: "prompt.txt"},
		{name: "trust file", path: filepath.Join("grok-home", "trusted_folders.toml")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probeDir := t.TempDir()
			target := filepath.Join(probeDir, tt.path)
			if tt.file {
				if err := os.WriteFile(target, []byte("conflict"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.MkdirAll(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if _, _, _, _, err := seedGrokProbeWorkspace(probeDir); err == nil {
				t.Fatalf("expected conflict at %s", target)
			}
		})
	}
}

func TestProbeReportsProviderExitWithValidStream(t *testing.T) {
	binDir := t.TempDir()
	stub := filepath.Join(binDir, "grok")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 'grok 1.0.5'; exit 0; fi\n" +
		"echo '{\"type\":\"text\",\"data\":\"partial\"}'\n" +
		"echo '{\"type\":\"end\",\"stopReason\":\"end_turn\",\"sessionId\":\"probe\"}'\n" +
		"exit 5\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil { // #nosec G306 -- test stub.
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	result, err := Probe(context.Background(), t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 5 || result.Error == "" || !result.Capabilities.StreamingJSONUsed {
		t.Fatalf("provider failure result = %+v", result)
	}
}

func TestProbeBoundsProviderOutputWhileProcessRuns(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			binDir := t.TempDir()
			stub := filepath.Join(binDir, "grok")
			redirect := ""
			if stream == "stderr" {
				redirect = " >&2"
			}
			script := "#!/bin/sh\n" +
				"if [ \"$1\" = \"--version\" ]; then echo 'grok 1.0.5'; exit 0; fi\n" +
				"echo '{\"type\":\"text\",\"data\":\"partial\"}'\n" +
				"echo '{\"type\":\"end\",\"stopReason\":\"end_turn\",\"sessionId\":\"probe\"}'\n" +
				"i=0; while [ \"$i\" -lt 20 ]; do printf '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'" + redirect + "; i=$((i + 1)); done\n"
			if err := os.WriteFile(stub, []byte(script), 0o700); err != nil { // #nosec G306 -- test stub.
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir)

			const outputLimit = 256
			result, err := probeWithOutputLimit(context.Background(), t.TempDir(), "", outputLimit)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.Error, stream) {
				t.Fatalf("overflow result error = %q, want %s", result.Error, stream)
			}
			artifact, err := os.ReadFile(filepath.Join(result.ProbeDir, stream+".txt")) // #nosec G304 -- result path is probe-owned.
			if err != nil {
				t.Fatal(err)
			}
			if len(artifact) > outputLimit || !strings.Contains(string(artifact), probeOutputTruncatedMark) {
				t.Fatalf("bounded %s artifact length=%d content=%q", stream, len(artifact), artifact)
			}
		})
	}
}
