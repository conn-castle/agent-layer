package benchmark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestStudyMCPContractRejectsLiteralCookieAndCredentialValues(t *testing.T) {
	for _, server := range []struct {
		name   string
		server map[string]string
	}{
		{"cookie header", map[string]string{"Cookie": "session=literal"}},
		{"credential header", map[string]string{"X-Credential": "literal"}},
		{"cookie environment", map[string]string{"MCP_COOKIE": "literal"}},
		{"credential environment", map[string]string{"SERVICE_CREDENTIAL": "literal"}},
	} {
		t.Run(server.name, func(t *testing.T) {
			for key, value := range server.server {
				candidate := config.MCPServer{Headers: map[string]string{}, Env: map[string]string{}}
				if strings.Contains(server.name, "header") {
					candidate.Headers[key] = value
				} else {
					candidate.Env[key] = value
				}
				if err := rejectLiteralMCPSecrets(candidate); err == nil {
					t.Fatalf("literal %s was accepted", server.name)
				}
			}
		})
	}
	if err := rejectLiteralMCPSecrets(config.MCPServer{
		Headers: map[string]string{"Cookie": "${AL_COOKIE}"},
		Env:     map[string]string{"SERVICE_CREDENTIAL": "${AL_CREDENTIAL}"}, // #nosec G101 -- placeholder reference in a contract test.
	}); err != nil {
		t.Fatalf("placeholder credentials rejected: %v", err)
	}
}

func TestStudyMCPContractEncodesNoServersAsArray(t *testing.T) {
	root := t.TempDir()
	configPath := writeStudyTreatmentConfig(t, root)
	data, err := os.ReadFile(configPath) // #nosec G304 -- configPath is a test-owned temporary file.
	if err != nil {
		t.Fatal(err)
	}
	contract, _, err := studyMCPContract(data, configPath)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"servers":[]}` {
		t.Fatalf("empty MCP preflight contract = %s, want servers array", encoded)
	}
}

func TestStudyMCPContractRejectsLiteralProviderNativeCredentials(t *testing.T) {
	root := t.TempDir()
	configPath := writeStudyTreatmentConfig(t, root)
	base, err := os.ReadFile(configPath) // #nosec G304 -- configPath is a test-owned temporary file.
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []struct {
		name string
		body string
	}{
		{"api key", "\n[agents.codex.agent_specific.provider]\napi-key = \"literal\"\n"},
		{"authorization header", "\n[agents.codex.agent_specific.provider.headers]\nAuthorization = \"Bearer literal\"\n"},
		{"endpoint query", "\n[agents.codex.agent_specific.provider]\nendpoint = \"https://provider.example/mcp?access_token=literal\"\n"},
		{"disabled agent", "\n[agents.claude.agent_specific.provider]\napi_key = \"literal\"\n"},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			if _, _, err := studyMCPContract(append(append([]byte(nil), base...), []byte(candidate.body)...), configPath); err == nil || !strings.Contains(err.Error(), "agents.") {
				t.Fatalf("literal provider-native credential was accepted: %v", err)
			}
		})
	}
}

func TestStudyMCPContractTracksNestedCredentialContextAndCredentialFlags(t *testing.T) {
	root := t.TempDir()
	configPath := writeStudyTreatmentConfig(t, root)
	base, err := os.ReadFile(configPath) // #nosec G304 -- configPath is a test-owned temporary file.
	if err != nil {
		t.Fatal(err)
	}
	privateKeyPEM := "\n[agents.codex.agent_specific.provider]\nprivate-key = '''" +
		"-----BEGIN " + "PRIVATE KEY-----\nmaterial\n-----END " + "PRIVATE KEY-----" + "'''\n"
	for _, candidate := range []struct {
		name string
		body string
	}{
		{"nested credential value", "\n[agents.codex.agent_specific.provider.credentials.nested]\nvalue = \"literal\"\n"},
		{"URL username", "\n[agents.codex.agent_specific.provider]\nendpoint = \"https://literal@provider.example/mcp\"\n"},
		{"credential flag value", "\n[agents.codex.agent_specific.provider]\nargs = [\"--api-key\", \"literal\"]\n"},
		{"inline credential flag", "\n[agents.codex.agent_specific.provider]\nargs = [\"--api-key=literal\"]\n"},
		{"private key", "\n[agents.codex.agent_specific.provider]\nprivate_key = \"literal-private-key\"\n"},
		{"private key PEM", privateKeyPEM},
		{"compact private key", "\n[agents.codex.agent_specific.provider]\nprivatekey = \"literal-private-key\"\n"},
		{"private key flag value", "\n[agents.codex.agent_specific.provider]\nargs = [\"--private-key\", \"literal-private-key\"]\n"},
		{"inline private key flag", "\n[agents.codex.agent_specific.provider]\nargs = [\"--private-key=literal-private-key\"]\n"},
		{"inline compact private key flag", "\n[agents.codex.agent_specific.provider]\nargs = [\"--privatekey=literal-private-key\"]\n"},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			if _, _, err := studyMCPContract(append(append([]byte(nil), base...), []byte(candidate.body)...), configPath); err == nil {
				t.Fatal("literal credential was accepted")
			}
		})
	}
	for _, candidate := range []string{
		"\n[agents.codex.agent_specific.provider]\ntokenizer = \"cl100k_base\"\nargs = [\"--tokenizer\", \"cl100k_base\"]\n",
		"\n[agents.codex.agent_specific.provider.credentials]\nvalue = \"${AL_PROVIDER_CREDENTIAL}\"\n",
		"\n[agents.codex.agent_specific.provider]\nprivate-key = \"${AL_PROVIDER_PRIVATE_KEY}\"\nprivate_key_id = \"abc123\"\nargs = [\"--private-key\", \"${AL_PROVIDER_PRIVATE_KEY}\"]\n",
	} {
		if _, _, err := studyMCPContract(append(append([]byte(nil), base...), []byte(candidate)...), configPath); err != nil {
			t.Fatalf("non-literal or non-credential setting rejected: %v", err)
		}
	}
}

func TestStudyMCPContractAllowsAndCarriesProviderNativeCredentialReferences(t *testing.T) {
	root := t.TempDir()
	configPath := writeStudyTreatmentConfig(t, root)
	base, err := os.ReadFile(configPath) // #nosec G304 -- configPath is a test-owned temporary file.
	if err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), base...)
	data = append(data, []byte(`
[agents.codex.agent_specific.provider]
api_key = "${AL_PROVIDER_API_KEY}"
endpoint = "https://provider.example/mcp?access_token=${AL_PROVIDER_QUERY_TOKEN}"
token_endpoint = "https://provider.example/oauth/token"
auth_mode = "oauth"

[agents.codex.agent_specific.provider.headers]
Authorization = "Bearer ${AL_PROVIDER_BEARER_TOKEN}"
`)...)
	_, names, err := studyMCPContract(data, configPath)
	if err != nil {
		t.Fatalf("provider-native credential references rejected: %v", err)
	}
	if got, want := strings.Join(names, ","), "AL_PROVIDER_API_KEY,AL_PROVIDER_BEARER_TOKEN,AL_PROVIDER_QUERY_TOKEN"; got != want {
		t.Fatalf("provider-native credential names = %q, want %q", got, want)
	}
}

func TestAdapterHTTPMCPPreflightRequiresCompleteMCPInitializeResult(t *testing.T) {
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(adapter)
	start := strings.Index(contents, "def validate_mcp_initialize_response")
	if start < 0 {
		t.Fatal("adapter no longer exposes the HTTP MCP initialize response validator")
	}
	end := strings.Index(contents[start:], "\n\nclass _AgentLayerTreatment")
	if end < 0 {
		t.Fatal("adapter HTTP MCP initialize response validator is not bounded before the treatment class")
	}
	validator := contents[start : start+end]
	script := "import json\n" + validator + `
class Response:
    def __init__(self, *, payload=b"", lines=()):
        self.payload = payload
        self.lines = list(lines)
    def read(self, limit):
        return self.payload
    def readline(self, limit):
        return self.lines.pop(0) if self.lines else b""

valid = '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test","version":"1"}}}'
requested_protocol_version = "2025-03-26"
validate_mcp_initialize_response(valid, "application/json", requested_protocol_version)
read_mcp_initialize_response(Response(payload=valid.encode()), "application/json", requested_protocol_version, monotonic=lambda: 0)
read_mcp_initialize_response(Response(lines=[b"event: message\n", b"data: " + valid.encode() + b"\n", b"\n"]), "text/event-stream; charset=utf-8", requested_protocol_version, monotonic=lambda: 0)
for payload, content_type in [
    ('{"jsonrpc":"2.0","id":2,"result":{}}', "application/json"),
    ('{"jsonrpc":"2.0","id":1,"error":{"code":-32000}}', "application/json"),
    ('{"jsonrpc":"2.0","id":1,"error":{"code":-32000},"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test","version":"1"}}}', "application/json"),
    ('{"jsonrpc":"2.0","id":1,"result":null}', "application/json"),
    ('{"jsonrpc":"2.0","id":1,"result":[]}', "application/json"),
    ('{"jsonrpc":"2.0","id":1,"result":{}}', "application/json"),
    ('{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1"}}}', "application/json"),
    ('ok', "text/plain"),
]:
    try:
        validate_mcp_initialize_response(payload, content_type, requested_protocol_version)
    except RuntimeError:
        continue
    raise SystemExit(f"accepted invalid response: {payload!r}")
for response, kwargs in [
    (Response(lines=[b": one\n", b"\n", b": two\n", b"\n", b": three\n", b"\n"]), {"event_cap": 2, "monotonic": lambda: 0}),
    (Response(lines=[b"data: too-large\n"]), {"byte_cap": 4, "monotonic": lambda: 0}),
    (Response(payload=valid.encode()), {"deadline_seconds": 1, "monotonic": iter([0, 0, 2]).__next__}),
]:
    try:
        read_mcp_initialize_response(response, "text/event-stream" if response.lines else "application/json", requested_protocol_version, **kwargs)
    except RuntimeError:
        continue
    raise SystemExit("accepted an unbounded HTTP/SSE response")
for payload in [
    '{"jsonrpc":"2.0","id":1,"error":{"code":-32000},"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test","version":"1"}}}',
    '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1"}}}',
]:
    try:
        read_mcp_initialize_response(Response(payload=payload.encode()), "application/json", requested_protocol_version, monotonic=lambda: 0)
    except RuntimeError:
        continue
    raise SystemExit("response reader accepted an invalid protocol result")
`
	command := exec.CommandContext(t.Context(), "python3", "-c", script) // #nosec G204 -- test-owned adapter source and fixed interpreter.
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("offline HTTP MCP response validation failed: %v\n%s", err, output)
	}
}

func TestStreamAdapterContractsRequireBoundedSuccessfulTerminalEvidence(t *testing.T) {
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(adapter)
	start := strings.Index(contents, "def _bounded_json_lines")
	end := strings.Index(contents[start:], "\n\nclass _AgentLayerStreamAgent")
	if start < 0 || end < 0 {
		t.Fatal("adapter stream parsers are not available for contract testing")
	}
	parsers := contents[start : start+end]
	script := "import json\nSTREAM_BYTE_CAP = 16 * 1024 * 1024\n" + parsers + `
antigravity = '{"event":"result","result":{"status":"SUCCESS","conversation_id":"coordinator","response":"answer","usage":{"input_tokens":1}}}\n'
terminal, usage = parse_antigravity_stream(antigravity)
assert terminal["conversation_id"] == "coordinator" and usage["input_tokens"] == 1
grok = '{"type":"usage","usage":{"input_tokens":1,"output_tokens":1}}\n{"type":"end","sessionId":"11111111-1111-4111-8111-111111111111","stopReason":"end_turn"}\n'
terminal, usage = parse_grok_stream(grok, "11111111-1111-4111-8111-111111111111")
assert len(usage) == 1 and terminal["stopReason"] == "end_turn"
for thunk in [
    lambda: parse_antigravity_stream('{"event":"result","result":{"status":"ERROR","usage":{}}}\n'),
    lambda: parse_antigravity_stream('{not json}\n'),
    lambda: parse_grok_stream('{"type":"end","sessionId":"other","stopReason":"end_turn"}\n', "11111111-1111-4111-8111-111111111111"),
    lambda: parse_grok_stream('{"type":"error","message":"denied"}\n', "11111111-1111-4111-8111-111111111111"),
    lambda: parse_grok_stream('{"type":"tool_call_update","status":"failed","message":"Denied by permission policy"}\n', "11111111-1111-4111-8111-111111111111"),
    lambda: parse_grok_stream('{"type":"usage","usage":{}}\n', "11111111-1111-4111-8111-111111111111"),
    lambda: parse_grok_stream('{"type":"usage","usage":{"input_tokens":1}}\n{"type":"end","sessionId":"11111111-1111-4111-8111-111111111111","stopReason":"end_turn"}\n{"type":"usage","usage":{"input_tokens":1}}\n', "11111111-1111-4111-8111-111111111111"),
]:
    try: thunk()
    except RuntimeError: continue
    raise SystemExit("stream parser accepted malformed/unsuccessful evidence")
try:
    parse_antigravity_stream("x" * (16 * 1024 * 1024 + 1))
except RuntimeError:
    pass
else:
    raise SystemExit("stream parser accepted oversized evidence")
`
	command := exec.CommandContext(t.Context(), "python3", "-c", script) // #nosec G204 -- test-owned embedded adapter source.
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("adapter stream parser contract failed: %v\n%s", err, output)
	}
	for _, required := range []string{
		"ANTIGRAVITY_LINUX_AMD64_SHA512", "GROK_LINUX_AMD64_SHA256", "network_allowlist", "_bounded_provider_capture", "uuid.uuid4()",
		`grok_home = f"{REMOTE_WORKSPACE}/.grok-config"`, "--trust", "antigravity-mcp-preflight.json", "grok-mcp-preflight.json",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("adapter omitted pinned/runtime contract %q", required)
		}
	}
	if strings.Contains(contents, "--sandbox workspace --print-timeout") {
		t.Fatal("Antigravity adapter passes Grok-style sandbox profile to boolean --sandbox flag")
	}
	if !strings.Contains(contents, "await self._snapshot_workspace(environment)") || strings.Contains(contents, `if self._treatment_mode != "bare":\n            await self.exec_as_agent`) {
		t.Fatal("stream adapter does not snapshot and restore adapter-owned paths for bare and treatment arms")
	}
	if strings.Contains(contents, "try:\n            effective = await self._prepare") ||
		!strings.Contains(contents, "effective = await self._prepare(self.render_instruction(instruction), environment)\n        try:") {
		t.Fatal("stream adapter restores projected paths before snapshot creation succeeds")
	}
}

func TestPinnedStreamAdaptersImplementPierInstallAndEgressContracts(t *testing.T) {
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pier_agent_layer.py")
	if err := os.WriteFile(path, adapter, 0o600); err != nil {
		t.Fatal(err)
	}
	script := `
import asyncio
import importlib.util
import subprocess
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("pier_agent_layer", sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
for cls, model, domains in [
    (module.AgentLayerAntigravity, "gemini-3.5-flash-low", {"storage.googleapis.com", ".googleapis.com"}),
    (module.AgentLayerGrok, "grok-4.5", {"storage.googleapis.com", "api.x.ai", "auth.x.ai"}),
]:
    agent = cls(
        logs_dir=Path("/tmp/pier-adapter-contract"), model_name=model, version=cls.PINNED_VERSION,
        treatment_agent=agent_name if (agent_name := cls.name()) else "", treatment_model=model,
        treatment_reasoning_effort="low", treatment_mode="bare",
    )
    install = agent.install_spec()
    assert install.agent_name == cls.name() and install.version == cls.PINNED_VERSION
    assert install.verification_command == f"{cls.BINARY} --version"
    assert install.metadata["network_allowlist"] == ["storage.googleapis.com"]
    assert set(agent.network_allowlist().domains) == domains
    command = install.steps[0].run
    assert "urllib.request.urlretrieve" in command and "sha" in command and "latest" not in command
    with tempfile.TemporaryDirectory() as directory:
        out, err = Path(directory) / "stdout", Path(directory) / "stderr"
        capture = agent._bounded_provider_capture("printf stream; printf diagnostic >&2", str(out), str(err))
        completed = subprocess.run(capture, shell=True, text=True, capture_output=True)
        assert completed.returncode == 0, completed.stderr
        assert out.read_text() == "stream" and err.read_text() == "diagnostic"

        async def execute_validator(environment, command, **kwargs):
            validated = subprocess.run(command, shell=True, text=True, capture_output=True)
            assert validated.returncode == 0, validated.stderr
        agent.exec_as_agent = execute_validator
        for provider in ("grok", "antigravity"):
            asyncio.run(agent._preflight_retained_stream_validator(object(), provider))
            assert not Path(f"/tmp/agent-layer-{provider}-stream-validator-preflight.jsonl").exists()

commands = []
async def capture_command(environment, command, **kwargs):
    commands.append(command)
agent.exec_as_agent = capture_command
asyncio.run(agent._snapshot_workspace(object()))
assert len(commands) == 1
assert "git ls-files --others -z" in commands[0]
assert "git config user.email" in commands[0]
assert "agent-layer-original" in commands[0]
commands.clear()
asyncio.run(agent._collect_evidence(object()))
assert any("for path in" in command and "agent-layer-original" in command for command in commands)
assert any("git reset -q --pathspec-from-file" in command and "git commit -m" in command for command in commands)
`
	command := exec.CommandContext(t.Context(), "uvx", "--from", "datacurve-pier=="+PierVersion, "python", "-c", script, path) // #nosec G204 -- embedded test loads the checked-in adapter against its pinned runtime.
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pinned Pier adapter contract failed: %v\n%s", err, output)
	}
}

func TestStudyTreatmentBundleConfigOnlyStagesRuntimeWithoutWorkflow(t *testing.T) {
	root := t.TempDir()
	configPath := writeStudyTreatmentConfig(t, root)
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildStudyTreatmentBundle(root, preparedStudyExperiment{
		studyExperiment: studyExperiment{Config: filepath.Base(configPath)}, model: model, effort: effort,
		inputs: studyExperimentInputs{Config: configPath},
	})
	if err != nil {
		t.Fatalf("build config-only study bundle: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(bundle.Root) })
	if bundle.Manifest.Mode != TreatmentInstructionsOnly || bundle.LinuxBinary == "" || bundle.LinuxBinarySHA256 == "" || bundle.LinuxArchitecture != benchmarkTaskContainerArchitecture {
		t.Fatalf("config-only bundle = %#v", bundle)
	}
	for _, path := range []string{
		".agent-layer/config.toml", ".agent-layer/.env", "mcp-preflight.json", "adapter/pier_agent_layer.py",
	} {
		if _, statErr := os.Stat(filepath.Join(bundle.Root, path)); statErr != nil {
			t.Fatalf("config-only bundle omitted %s: %v", path, statErr)
		}
	}
	for _, path := range []string{"workflow-prompt.md", "dispatch-targets.json", "adapter/al_dispatch_gate.py"} {
		if _, statErr := os.Stat(filepath.Join(bundle.Root, path)); !os.IsNotExist(statErr) {
			t.Fatalf("config-only bundle unexpectedly staged %s: %v", path, statErr)
		}
	}
	env, err := os.ReadFile(filepath.Join(bundle.Root, ".agent-layer", ".env"))
	if err != nil || string(env) != "# Intentionally empty; provider authentication is injected separately.\n" {
		t.Fatalf("config-only bundle env = %q, %v", env, err)
	}
}

func TestStudyTreatmentBundleIdentityIsDeterministic(t *testing.T) {
	root := t.TempDir()
	configPath := writeStudyTreatmentConfig(t, root)
	configData, err := os.ReadFile(configPath) // #nosec G304 -- configPath is created inside this test's private temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	configData = []byte(strings.ReplaceAll(string(configData), "[agents.grok]\nenabled = false", "[agents.grok]\nenabled = true"))
	if err := os.WriteFile(configPath, configData, 0o600); err != nil { // #nosec G703 -- configPath is the test-owned fixture returned above.
		t.Fatal(err)
	}
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	experiment := preparedStudyExperiment{
		studyExperiment: studyExperiment{Config: filepath.Base(configPath)},
		model:           model,
		effort:          effort,
		inputs:          studyExperimentInputs{Config: configPath},
	}
	first, err := BuildStudyTreatmentBundle(root, experiment)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(first.Root) })
	second, err := BuildStudyTreatmentBundle(root, experiment)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(second.Root) })
	if first.ManifestHash != second.ManifestHash ||
		first.AdapterSHA256 != second.AdapterSHA256 ||
		first.LinuxBinarySHA256 != second.LinuxBinarySHA256 {
		t.Fatalf("identical study snapshots produced different bundle identities:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	trust, err := os.ReadFile(filepath.Join(first.Root, ".grok-config", "trusted_folders.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(trust), "[folders.\"/app\"]\ntrusted = true\ndecided_at = 0\n") {
		t.Fatalf("benchmark Grok trust projection is not deterministic:\n%s", trust)
	}
}

func TestTreatmentStudyDryRunIdentityIsDeterministicAcrossInvocations(t *testing.T) {
	root := t.TempDir()
	writeParsedTreatmentStudy(t, root, "luna:low")
	stubStudyInfrastructure(t, root)

	originalAuthentication := validateBenchmarkAuthentication
	originalRuntimePreflight := preflightTreatmentRuntime
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error { return nil }
	t.Cleanup(func() {
		validateBenchmarkAuthentication = originalAuthentication
		preflightTreatmentRuntime = originalRuntimePreflight
	})

	first, err := RunStudy(context.Background(), StudyOptions{
		RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), DryRun: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RunStudy(context.Background(), StudyOptions{
		RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), DryRun: true, TaskConcurrency: 4,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.StudyID != first.StudyID || second.Experiments[0].Identity != first.Experiments[0].Identity {
		t.Fatalf("identical dry-run preparations changed identity:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	studies, err := os.ReadDir(filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies"))
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 || studies[0].Name() != first.StudyID {
		t.Fatalf("identical dry runs created distinct study directories: %#v", studies)
	}
}

func TestStudyIdentityIgnoresVolatileOverlayBuildMetadata(t *testing.T) {
	root, checkout := t.TempDir(), t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	tasks := []string{"first-task", "second-task"}
	writeAuditCatalogFixture(t, checkout, tasks...)
	installAuditCheckout(t, checkout)
	overlayBuildSequence := 1
	installReadinessTestBoundaries(t, studyOverlayContractsFixture(tasks...), func(_ context.Context, arguments ...string) ([]byte, error) {
		switch arguments[0] {
		case "build":
			for index, argument := range arguments {
				if argument == "--iidfile" && index+1 < len(arguments) {
					buildID := fmt.Sprintf("sha256:%064x", overlayBuildSequence)
					overlayBuildSequence++
					return nil, os.WriteFile(arguments[index+1], []byte(buildID+"\n"), 0o600)
				}
			}
			return nil, errors.New("overlay build omitted image ID file")
		case commandRun, "image":
			return nil, nil
		default:
			return nil, fmt.Errorf("unexpected Docker command: %#v", arguments)
		}
	})

	originalPreflight, originalVerify := preflightBenchmark, verifyBenchmarkPier
	originalAuth, originalRuntime := validateBenchmarkAuthentication, preflightTreatmentRuntime
	preflightBenchmark = func([]parsedSelection) error { return nil }
	verifyBenchmarkPier = func(context.Context) error { return nil }
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error { return nil }
	t.Cleanup(func() {
		preflightBenchmark, verifyBenchmarkPier = originalPreflight, originalVerify
		validateBenchmarkAuthentication, preflightTreatmentRuntime = originalAuth, originalRuntime
	})

	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), DryRun: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), DryRun: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if overlayBuildSequence != 5 {
		t.Fatalf("overlay builds = %d, want four byte-distinct rebuilds", overlayBuildSequence-1)
	}
	if second.StudyID != first.StudyID || second.Experiments[0].Identity != first.Experiments[0].Identity {
		t.Fatalf("Docker build metadata changed immutable identity:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func studyOverlayContractsFixture(tasks ...string) fs.FS {
	contracts := fstest.MapFS{}
	for _, task := range tasks {
		root := "readiness/" + DeepSWECommit + "/" + task + "/"
		contract := fmt.Sprintf(
			`{"schema":%q,"task":%q,"image":%q,"image_digest":%q,"check":"check.sh","agent_image_overlay":"agent.Dockerfile"}`,
			readinessContractSchema, task, auditTaskImage(task), testReadinessDigest,
		)
		contracts[root+"contract.json"] = &fstest.MapFile{Data: []byte(contract)}
		contracts[root+"check.sh"] = &fstest.MapFile{Data: []byte("#!/bin/bash\ncheck-tools\n")}
		contracts[root+"agent.Dockerfile"] = &fstest.MapFile{Data: []byte("FROM " + auditTaskImage(task) + "@" + testReadinessDigest + "\nRUN install-tools\n")}
	}
	return contracts
}

func TestGrokDryRunAndPaidExecutionUseDisposableContainerSandbox(t *testing.T) {
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pier_agent_layer.py")
	if err := os.WriteFile(path, adapter, 0o600); err != nil {
		t.Fatal(err)
	}
	script := `
import asyncio
import importlib.util
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("pier_agent_layer", sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class Environment:
    async def upload_file(self, source, destination):
        pass

async def rendered_commands(preflight):
    with tempfile.NamedTemporaryFile() as credential:
        agent = module.AgentLayerGrok(
            logs_dir=Path("/tmp/pier-grok-sandbox"), model_name="grok-4.5",
            version=module.AgentLayerGrok.PINNED_VERSION,
            treatment_agent="grok", treatment_model="grok-4.5",
            treatment_reasoning_effort="low", treatment_mode="bare",
            grok_credentials_path=credential.name, preflight_only=preflight,
        )
        commands = []
        async def capture_exec(environment, command, **kwargs):
            commands.append(command)
        async def capture_run(environment, command, env):
            commands.append(command)
        async def prepare(instruction, environment):
            return instruction
        validations = []
        async def capture_validation(environment, remote_path, provider, session_id=""):
            validations.append((remote_path, provider, session_id))
        async def no_op(*args, **kwargs):
            pass
        agent.exec_as_agent = capture_exec
        agent._run_command = capture_run
        agent._prepare = prepare
        agent._validate_retained_stream = capture_validation
        agent._collect_evidence = no_op
        await agent.run("task", Environment(), None)
        return commands, validations

dry_run, dry_validations = asyncio.run(rendered_commands(True))
paid, paid_validations = asyncio.run(rendered_commands(False))
assert any("grok --no-auto-update --sandbox devbox models" in command for command in dry_run), dry_run
assert any("--trust --sandbox devbox --permission-mode bypassPermissions" in command for command in paid), paid
assert not any("--sandbox workspace" in command for command in dry_run + paid), dry_run + paid
assert len(dry_validations) == 1 and dry_validations[0][1] == "grok", dry_validations
assert len(paid_validations) == 1 and paid_validations[0][1] == "grok", paid_validations
`
	command := exec.CommandContext(t.Context(), "uvx", "--from", "datacurve-pier=="+PierVersion, "python", "-c", script, path) // #nosec G204 -- embedded test loads the checked-in adapter against its pinned runtime.
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("render Grok sandbox commands: %v\n%s", err, output)
	}
}

func TestStudyTreatmentBundleUsesCertifiedAMD64Target(t *testing.T) {
	if benchmarkTaskContainerPlatform != "linux/amd64" || benchmarkTaskContainerArchitecture != "amd64" {
		t.Fatalf("certified task platform = %q/%q", benchmarkTaskContainerPlatform, benchmarkTaskContainerArchitecture)
	}
	// BuildStudyTreatmentBundle has no host-architecture parameter: this value
	// is deliberately derived only from the certified task container contract.
	root := t.TempDir()
	configPath := writeStudyTreatmentConfig(t, root)
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildStudyTreatmentBundle(root, preparedStudyExperiment{studyExperiment: studyExperiment{Config: "config.toml"}, model: model, effort: effort, inputs: studyExperimentInputs{Config: configPath}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(bundle.Root) })
	if bundle.LinuxArchitecture != "amd64" || !strings.HasSuffix(bundle.LinuxBinary, "al-linux-amd64") {
		t.Fatalf("runtime target follows something other than certified task architecture: %#v", bundle)
	}
}

func TestStudySkillsRequireTheDeclaredEntryPrompt(t *testing.T) {
	root := t.TempDir()
	configPath := writeStudyTreatmentConfig(t, root)
	skills := filepath.Join(root, "skills")
	if err := os.MkdirAll(skills, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skills, "SKILL.md"), []byte("---\nname: benchmark\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	_, err = BuildStudyTreatmentBundle(root, preparedStudyExperiment{
		studyExperiment: studyExperiment{Config: filepath.Base(configPath), Skills: "skills"}, model: model, effort: effort,
		inputs: studyExperimentInputs{Config: configPath, Skills: skills},
	})
	if err == nil || !strings.Contains(err.Error(), "requires entry_prompt") {
		t.Fatalf("skills bundle silently fell back without an entry prompt: %v", err)
	}
}

func TestStudyTreatmentBundlePropagatesRequiredDispatchRoles(t *testing.T) {
	root := t.TempDir()
	configPath := writeStudyTreatmentConfig(t, root)
	skills := filepath.Join(root, "skills", "benchmark")
	if err := os.MkdirAll(skills, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skills, "SKILL.md"), []byte("---\nname: benchmark\ndescription: Benchmark skill.\n---\n\nDo the task.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.Join(root, "prompt.md")
	if err := os.WriteFile(prompt, []byte("Use {{task}}."), 0o600); err != nil {
		t.Fatal(err)
	}
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	experiment := preparedStudyExperiment{
		studyExperiment: studyExperiment{
			Config: filepath.Base(configPath), Skills: "skills", EntryPrompt: "prompt.md",
			RequiredDispatchRoles: []string{requiredRolePlanReviewer, requiredRoleImplementer},
		},
		model: model, effort: effort,
		inputs: studyExperimentInputs{Config: configPath, Skills: filepath.Dir(skills), EntryPrompt: prompt},
	}
	bundle, err := BuildStudyTreatmentBundle(root, experiment)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(bundle.Root) })
	if got := bundle.Manifest.RequiredRoles; len(got) != 2 || got[0] != requiredRoleImplementer || got[1] != requiredRolePlanReviewer {
		t.Fatalf("manifest required roles = %#v", got)
	}
	encoded, err := json.Marshal(bundle.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"required_dispatch_roles":["implementer","plan-reviewer"]`) {
		t.Fatalf("constrained manifest encoding = %s", encoded)
	}
	experiment.RequiredDispatchRoles = nil
	unconstrained, err := BuildStudyTreatmentBundle(root, experiment)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(unconstrained.Root) })
	if unconstrained.Manifest.RequiredRoles != nil {
		t.Fatalf("unconstrained manifest roles = %#v", unconstrained.Manifest.RequiredRoles)
	}
	encoded, err = json.Marshal(unconstrained.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"required_dispatch_roles":null`) {
		t.Fatalf("unconstrained manifest encoding = %s", encoded)
	}
	if unconstrained.ManifestHash == bundle.ManifestHash {
		t.Fatal("nonempty required roles did not change treatment manifest identity")
	}
	experiment.RequiredDispatchRoles = []string{}
	empty, err := BuildStudyTreatmentBundle(root, experiment)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(empty.Root) })
	if empty.Manifest.RequiredRoles != nil || empty.ManifestHash != unconstrained.ManifestHash {
		t.Fatalf("explicit empty roles changed unconstrained treatment identity: roles=%#v", empty.Manifest.RequiredRoles)
	}
}

func TestTreatmentManifestKeepsExplicitEmptyRolesByteCompatible(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	dispatch := defaultTreatmentDispatchConfig(model, effort)
	omitted, err := treatmentManifest(root, TreatmentInstructionsAndSkills, nil, dispatch)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := treatmentManifest(root, TreatmentInstructionsAndSkills, []string{}, dispatch)
	if err != nil {
		t.Fatal(err)
	}
	left, err := hashCanonical(omitted)
	if err != nil {
		t.Fatal(err)
	}
	right, err := hashCanonical(empty)
	if err != nil {
		t.Fatal(err)
	}
	if left != right || omitted.RequiredRoles != nil || empty.RequiredRoles != nil {
		t.Fatalf("explicit empty roles changed unconstrained manifest identity: omitted=%#v empty=%#v", omitted, empty)
	}
	encoded, err := json.Marshal(omitted)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"required_dispatch_roles":null`) {
		t.Fatalf("unconstrained manifest encoding = %s", encoded)
	}
	constrained, err := treatmentManifest(root, TreatmentInstructionsAndSkills, []string{requiredRoleImplementer}, dispatch)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := hashCanonical(constrained)
	if err != nil {
		t.Fatal(err)
	}
	if changed == left {
		t.Fatal("nonempty required roles did not change treatment manifest identity")
	}
}

func TestStudyTreatmentPinRejectsMutatedPinMetadata(t *testing.T) {
	repoRoot := t.TempDir()
	bundle := minimalStudyTreatmentBundle(t)
	pinned, err := pinStudyTreatmentBundle(repoRoot, bundle)
	if err != nil {
		t.Fatal(err)
	}
	pinPath := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "study-pins", pinned.ManifestHash, "pin.json")
	var pin studyTreatmentPin
	if err := readStudyJSON(pinPath, &pin); err != nil {
		t.Fatal(err)
	}
	if pin.SchemaVersion != studyTreatmentPinSchema || pin.PinID != pinned.ManifestHash || pin.Architecture != benchmarkTaskContainerArchitecture {
		t.Fatalf("study pin has unexpected schema/identity: %#v", pin)
	}
	pin.SchemaVersion = "deepswe-matrix-treatment-pin-v1"
	data, err := json.Marshal(pin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pinPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	candidate := copyStudyTreatmentBundle(t, pinned)
	if _, err := pinStudyTreatmentBundle(repoRoot, candidate); err == nil || !strings.Contains(err.Error(), "conflicts with immutable content") {
		t.Fatalf("mutated study pin was accepted: %v", err)
	}
}

func TestStudyTreatmentPinRejectsConcurrentPublisherMetadataConflict(t *testing.T) {
	repoRoot := t.TempDir()
	candidate := minimalStudyTreatmentBundle(t)
	originalRename := renameStudyTreatmentPin
	renameStudyTreatmentPin = func(stage, destination string) error {
		t.Helper()
		if err := copyRequiredTree(filepath.Join(stage, "bundle"), filepath.Join(destination, "bundle")); err != nil {
			t.Fatal(err)
		}
		var winner studyTreatmentPin
		if err := readStudyJSON(filepath.Join(stage, "pin.json"), &winner); err != nil {
			t.Fatal(err)
		}
		// The competing bundle has identical bytes, so validating it with the
		// caller's metadata would accept it. Its persisted architecture is not
		// the certified task architecture and must be rejected from the pin.
		winner.Architecture = "arm64"
		if err := writeJSON(filepath.Join(destination, "pin.json"), winner); err != nil {
			t.Fatal(err)
		}
		return os.ErrExist
	}
	t.Cleanup(func() { renameStudyTreatmentPin = originalRename })

	if _, err := pinStudyTreatmentBundle(repoRoot, candidate); err == nil || !strings.Contains(err.Error(), "corrupt immutable metadata") {
		t.Fatalf("concurrent conflicting study pin was accepted: %v", err)
	}
}

func TestBenchmarkCredentialBoundaryLoadsOnlyDeclaredNames(t *testing.T) {
	repoRoot := t.TempDir()
	envPath := filepath.Join(repoRoot, ".agent-layer", ".env")
	if err := os.MkdirAll(filepath.Dir(envPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("AL_USED=from-file\nAL_UNUSED=must-not-cross\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AL_FALLBACK", "from-process")
	values, err := benchmarkCredentialValues(repoRoot, &TreatmentBundle{CredentialNames: []string{"AL_FALLBACK", "AL_USED"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values["AL_USED"] != "from-file" || values["AL_FALLBACK"] != "from-process" {
		t.Fatalf("selected credential values = %#v", values)
	}
	if _, found := values["AL_UNUSED"]; found {
		t.Fatalf("undeclared credential crossed boundary: %#v", values)
	}
	for _, item := range benchmarkProcessEnvironment(values) {
		if strings.Contains(item, "AL_UNUSED=") {
			t.Fatalf("undeclared credential reached Pier environment: %q", item)
		}
	}
}

func TestStudyCredentialInjectionUsesEnvironmentAndNormalEnvFileBoundary(t *testing.T) {
	repoRoot := t.TempDir()
	authentication := filepath.Join(repoRoot, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authentication), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authentication, []byte(`{"token":"provider-auth"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	const sentinel = "credential-value-must-not-reach-argv" // #nosec G101 -- deliberately non-secret test sentinel.
	t.Setenv("AL_MCP_TOKEN", sentinel)
	arguments, err := treatmentPierArguments(ExecutionRequest{
		RepoRoot: repoRoot, Model: model, Effort: effort, Arm: ArmTreatment,
		Bundle: &TreatmentBundle{Root: "/immutable", CredentialNames: []string{"AL_MCP_TOKEN"}, Manifest: TreatmentManifest{
			Mode: TreatmentInstructionsOnly, AgentTimeoutMultiplier: skillsAgentTimeoutFactor,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(arguments, " ")
	if strings.Contains(joined, sentinel) || !strings.Contains(joined, "credential_names=AL_MCP_TOKEN") {
		t.Fatalf("Pier arguments leaked or omitted configured credential boundary: %s", joined)
	}
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(adapter)
	materialize := strings.Index(contents, "REMOTE_WORKSPACE}/.agent-layer/.env")
	sync := strings.Index(contents, "/usr/local/bin/al sync")
	if materialize < 0 || sync < 0 || materialize > sync || !strings.Contains(contents, "self._credential_env") {
		t.Fatal("adapter does not materialize declared credentials into .agent-layer/.env before sync")
	}
}

func TestReleaseChecksumParsingRejectsMalformedOrMissingEntries(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if got, err := checksumForReleaseAsset([]byte(digest+"  al-linux-amd64\n"), "al-linux-amd64"); err != nil || got != digest {
		t.Fatalf("release checksum = %q, %v", got, err)
	}
	for _, sums := range [][]byte{
		[]byte("not-a-checksum  al-linux-amd64\n"),
		[]byte(digest + "  different-asset\n"),
	} {
		if _, err := checksumForReleaseAsset(sums, "al-linux-amd64"); err == nil {
			t.Fatalf("invalid release checksums were accepted: %q", sums)
		}
	}
}

func writeStudyTreatmentConfig(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "config.toml")
	data := []byte(`[approvals]
mode = "all"

[agents.antigravity]
enabled = false

[agents.claude]
enabled = false

[agents.claude_vscode]
enabled = false

[agents.codex]
enabled = true
local_config_dir = true

[agents.vscode]
enabled = false

[agents.copilot_cli]
enabled = false

[agents.grok]
enabled = false
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func minimalStudyTreatmentBundle(t *testing.T) *TreatmentBundle {
	t.Helper()
	root := t.TempDir()
	adapter := filepath.Join(root, "adapter", "pier_agent_layer.py")
	if err := os.MkdirAll(filepath.Dir(adapter), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adapter, []byte("adapter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := treatmentManifest(root, TreatmentInstructionsOnly, nil, TreatmentDispatchConfig{})
	if err != nil {
		t.Fatal(err)
	}
	manifestHash, err := hashCanonical(manifest)
	if err != nil {
		t.Fatal(err)
	}
	adapterHash, err := studyTestFileSHA256(adapter)
	if err != nil {
		t.Fatal(err)
	}
	return &TreatmentBundle{Root: root, Manifest: manifest, ManifestHash: manifestHash, LinuxArchitecture: benchmarkTaskContainerArchitecture, AdapterPath: adapter, AdapterSHA256: adapterHash}
}

func studyTestFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a test-owned temporary file.
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func copyStudyTreatmentBundle(t *testing.T, source *TreatmentBundle) *TreatmentBundle {
	t.Helper()
	root := t.TempDir()
	if err := copyRequiredTree(source.Root, root); err != nil {
		t.Fatal(err)
	}
	copy := *source
	copy.Root = root
	copy.AdapterPath = filepath.Join(root, "adapter", "pier_agent_layer.py")
	return &copy
}
