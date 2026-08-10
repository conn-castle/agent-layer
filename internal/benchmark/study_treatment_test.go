package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
