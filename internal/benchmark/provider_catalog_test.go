package benchmark

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProviderCatalogParsesProviderSpecificSelections(t *testing.T) {
	for _, test := range []struct {
		selection string
		adapter   string
		wantErr   string
	}{
		{"luna:low", adapterCodex, ""},
		{"gemini-3.5-flash-low:low", adapterAntigravity, ""},
		{"gemini-3.5-flash-low:medium", "", "requires reasoning effort"},
		{"grok-4.5:minimal", adapterGrok, ""},
		{"grok-4.5:invalid", "", "unsupported Grok reasoning effort"},
		{"not-a-selection", "", "must be in the form"},
		{"historic:low", "", "unsupported model"},
		{"luna:invalid", "", "unsupported reasoning effort"},
	} {
		t.Run(test.selection, func(t *testing.T) {
			model, _, err := ParseModelSelection(test.selection)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("ParseModelSelection(%q) error = %v", test.selection, err)
				}
				return
			}
			if err != nil || model.Adapter != test.adapter {
				t.Fatalf("ParseModelSelection(%q) = %#v, %v", test.selection, model, err)
			}
		})
	}
}

func TestProviderCatalogPreservesSlugAndPublishedIdentityCompatibility(t *testing.T) {
	for _, test := range []struct {
		slug string
		want string
		ok   bool
	}{
		{"gemini-3.5-flash-low", effortLow, true},
		{"gemini-3.5-flash-medium", effortMedium, true},
		{"gemini-3.5-flash-high", effortHigh, true},
		{"gemini-3.5-flash", "", false},
	} {
		if effort, ok := antigravitySlugEffort(test.slug); effort != test.want || ok != test.ok {
			t.Fatalf("antigravitySlugEffort(%q) = %q, %t; want %q, %t", test.slug, effort, ok, test.want, test.ok)
		}
	}
	if modelNameForPublished(publishedLuna) != "luna" || modelNameForPublished("historic-provider-model") != "historic-provider-model" {
		t.Fatal("published model identity compatibility changed")
	}
	for task, valid := range map[string]bool{"task-123": true, "Task-123": false, "": false, strings.Repeat("a", 161): false} {
		if got := validTaskName(task); got != valid {
			t.Fatalf("validTaskName(%q) = %t, want %t", task, got, valid)
		}
	}
	// The published catalog is static, but malformed future catalog entries
	// must still fail as configuration errors instead of silently accepting a
	// model with an unprovable thinking tier.
	original := supportedModels
	supportedModels = append(append([]Model(nil), original...), Model{Name: "invalid-gemini", RuntimeIdentifier: "gemini-3.5-flash", Adapter: adapterAntigravity})
	t.Cleanup(func() { supportedModels = original })
	if _, _, err := ParseModelSelection("invalid-gemini:low"); err == nil || !strings.Contains(err.Error(), "must be an exact Gemini slug") {
		t.Fatalf("malformed Antigravity catalog entry was accepted: %v", err)
	}
}

func TestCachedAuthenticationPreflightAcceptsOnlyKnownProviderEvidence(t *testing.T) {
	now := time.Now().UTC()
	for _, test := range []struct {
		adapter  string
		evidence AuthenticationPreflight
		valid    bool
	}{
		{adapterCodex, AuthenticationPreflight{Provider: adapterCodex, Check: codexLoginStatusCheck, AuthenticationMethod: codexAuthMethodChatGPT, VerifiedAt: now}, true},
		{adapterAntigravity, AuthenticationPreflight{Provider: adapterAntigravity, Check: authCheckOAuthProfilePresence, AuthenticationMethod: authMethodGoogleOAuth, VerifiedAt: now}, true},
		{adapterGrok, AuthenticationPreflight{Provider: adapterGrok, Check: authCheckJSONFilePresence, AuthenticationMethod: authMethodJSONFile, VerifiedAt: now}, true},
		{adapterGrok, AuthenticationPreflight{Provider: adapterGrok, Check: authCheckEnvironmentPresence, AuthenticationMethod: authMethodJSONFile, VerifiedAt: now}, false},
		{adapterAntigravity, AuthenticationPreflight{Provider: adapterGrok, Check: authCheckOAuthProfilePresence, AuthenticationMethod: authMethodGoogleOAuth, VerifiedAt: now}, false},
		{"unknown", AuthenticationPreflight{Provider: "unknown", Check: "presence", AuthenticationMethod: "unknown", VerifiedAt: now}, false},
	} {
		if got := validCachedAuthenticationPreflight(test.adapter, test.evidence); got != test.valid {
			t.Fatalf("validCachedAuthenticationPreflight(%q, %#v) = %t, want %t", test.adapter, test.evidence, got, test.valid)
		}
	}
}

func TestAuthenticationPreflightEqualityIncludesEveryRecordedField(t *testing.T) {
	when := time.Date(2026, 8, 26, 4, 0, 0, 0, time.UTC)
	base := AuthenticationPreflight{Provider: adapterGrok, Check: authCheckJSONFilePresence, AuthenticationMethod: authMethodJSONFile, VerifiedAt: when}
	if !sameAuthenticationPreflight(base, base) {
		t.Fatal("identical authentication provenance differs")
	}
	for _, changed := range []AuthenticationPreflight{
		{Provider: adapterAntigravity, Check: base.Check, AuthenticationMethod: base.AuthenticationMethod, VerifiedAt: base.VerifiedAt},
		{Provider: base.Provider, Check: authCheckEnvironmentPresence, AuthenticationMethod: base.AuthenticationMethod, VerifiedAt: base.VerifiedAt},
		{Provider: base.Provider, Check: base.Check, AuthenticationMethod: authMethodGoogleOAuth, VerifiedAt: base.VerifiedAt},
		{Provider: base.Provider, Check: base.Check, AuthenticationMethod: base.AuthenticationMethod, VerifiedAt: base.VerifiedAt.Add(time.Second)},
	} {
		if sameAuthenticationPreflight(base, changed) {
			t.Fatalf("different authentication provenance compared equal: %#v", changed)
		}
	}
}

func TestProviderAuthenticationPreflightUsesMinimalProviderBoundariesOnce(t *testing.T) {
	repository := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repository, ".grok-config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".grok-config", "auth.json"), []byte(`{"token":"test-only"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	originalReader := readAntigravitySystemCredential
	reads := 0
	readAntigravitySystemCredential = func(context.Context) ([]byte, error) {
		reads++
		return []byte(`{"auth_method":"oauth","token":{"access_token":"test-access","refresh_token":"test-refresh"}}`), nil
	}
	t.Cleanup(func() { readAntigravitySystemCredential = originalReader })
	evidence, err := validateAuthentication(t.Context(), repository, []parsedSelection{
		{model: Model{Adapter: adapterAntigravity}},
		{model: Model{Adapter: adapterGrok}},
		{model: Model{Adapter: adapterGrok}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 2 || reads != 1 || evidence[adapterAntigravity].Check != authCheckOAuthProfilePresence || evidence[adapterGrok].Check != authCheckJSONFilePresence {
		t.Fatalf("provider authentication evidence = %#v", evidence)
	}
	if _, err := validateProviderAuthentication(t.Context(), repository, "unsupported"); err == nil || !strings.Contains(err.Error(), "unsupported benchmark provider") {
		t.Fatalf("unsupported provider authentication error = %v", err)
	}
}

func TestAntigravityOAuthCredentialUsesRepoFileThenSystemKeyring(t *testing.T) {
	repository := t.TempDir()
	credentialPath := filepath.Join(repository, antigravityOAuthRelativePath)
	if err := os.MkdirAll(filepath.Dir(credentialPath), 0o700); err != nil {
		t.Fatal(err)
	}
	fileCredential := []byte(`{"auth_method":"oauth","token":{"access_token":"file-access","refresh_token":"file-refresh"}}`)
	if err := os.WriteFile(credentialPath, fileCredential, 0o600); err != nil {
		t.Fatal(err)
	}
	originalReader := readAntigravitySystemCredential
	readAntigravitySystemCredential = func(context.Context) ([]byte, error) {
		t.Fatal("system keyring read despite repo-local OAuth profile")
		return nil, nil
	}
	t.Cleanup(func() { readAntigravitySystemCredential = originalReader })
	credential, err := benchmarkAntigravityOAuthCredential(t.Context(), repository)
	if err != nil || !strings.Contains(string(credential), "file-refresh") {
		t.Fatalf("repo-local Antigravity OAuth profile = %q, %v", credential, err)
	}
	if err := os.Remove(credentialPath); err != nil {
		t.Fatal(err)
	}
	wrapped := "go-keyring-base64:" + base64.StdEncoding.EncodeToString([]byte(`{"auth_method":"oauth","token":{"access_token":"keyring-access","refresh_token":"keyring-refresh"}}`))
	readAntigravitySystemCredential = func(context.Context) ([]byte, error) { return []byte(wrapped), nil }
	credential, err = benchmarkAntigravityOAuthCredential(t.Context(), repository)
	if err != nil || !strings.Contains(string(credential), "keyring-refresh") {
		t.Fatalf("system Antigravity OAuth profile = %q, %v", credential, err)
	}
	readAntigravitySystemCredential = func(context.Context) ([]byte, error) { return []byte("invalid"), nil }
	if _, err := benchmarkAntigravityOAuthCredential(t.Context(), repository); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("malformed Antigravity OAuth profile error = %v", err)
	}
	if err := os.Mkdir(credentialPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := benchmarkAntigravityOAuthCredential(t.Context(), repository); err == nil || !strings.Contains(err.Error(), "read Antigravity OAuth profile") {
		t.Fatalf("unreadable repo-local Antigravity OAuth profile error = %v", err)
	}
	if _, err := normalizeAntigravityOAuthCredential([]byte("go-keyring-base64:not-base64")); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("malformed Antigravity keyring encoding error = %v", err)
	}
}

func TestSystemAntigravityOAuthCredentialUsesNativeKeyringWithoutLeakingOutput(t *testing.T) {
	binary := ""
	switch runtime.GOOS {
	case platformDarwin:
		binary = "security"
	case platformLinux:
		binary = "secret-tool"
	default:
		t.Skip("native Antigravity keyring command is not supported on this platform")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, binary)
	credential := `{"auth_method":"oauth","token":{"access_token":"native-access","refresh_token":"native-refresh"}}` // #nosec G101 -- test-only OAuth fixture.
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '"+credential+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil { // #nosec G302 -- executable test stub in a private temporary directory.
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	got, err := readSystemAntigravityOAuthCredential(t.Context())
	if err != nil || string(got) != credential {
		t.Fatalf("native Antigravity OAuth credential = %q, %v", got, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'must-not-leak' >&2\nexit 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil { // #nosec G302 -- executable test stub in a private temporary directory.
		t.Fatal(err)
	}
	if _, err := readSystemAntigravityOAuthCredential(t.Context()); err == nil || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("native keyring failure leaked output or succeeded: %v", err)
	}
}

func TestAntigravityKeyringCommandCoversSupportedPlatforms(t *testing.T) {
	for _, test := range []struct {
		goos, binary string
	}{
		{goos: platformDarwin, binary: "security"},
		{goos: platformLinux, binary: "secret-tool"},
	} {
		command, err := antigravityKeyringCommand(t.Context(), test.goos)
		if err != nil || filepath.Base(command.Path) != test.binary {
			t.Fatalf("antigravityKeyringCommand(%q) = %#v, %v", test.goos, command, err)
		}
	}
	if _, err := antigravityKeyringCommand(t.Context(), "unsupported"); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("unsupported Antigravity keyring platform error = %v", err)
	}
}

func TestAntigravityPierArgumentsUseOAuthProfilePath(t *testing.T) {
	repository := t.TempDir()
	model, effort, err := ParseModelSelection("gemini-3.5-flash-low:low")
	if err != nil {
		t.Fatal(err)
	}
	request := ExecutionRequest{
		RepoRoot: repository, Model: model, Effort: effort, Arm: ArmBaseline,
	}
	if _, err := treatmentPierArguments(request); err == nil || !strings.Contains(err.Error(), "benchmark execution requires a positive agent timeout multiplier") {
		t.Fatalf("bare Antigravity timeout error = %v", err)
	}
	request.AgentTimeoutMultiplier = 1
	if _, err := treatmentPierArguments(request); err == nil || !strings.Contains(err.Error(), "OAuth authentication is required") {
		t.Fatalf("missing Antigravity OAuth argument error = %v", err)
	}
	credentialPath := filepath.Join(repository, antigravityOAuthRelativePath)
	if err := os.MkdirAll(filepath.Dir(credentialPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialPath, []byte(`{"auth_method":"oauth","token":{"access_token":"argument-access","refresh_token":"argument-refresh"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	arguments, err := treatmentPierArguments(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(arguments, " "), "antigravity_credentials_path="+credentialPath) {
		t.Fatalf("Antigravity Pier arguments omit OAuth profile: %v", arguments)
	}
}

func TestBareCustomProviderStagesEmbeddedPierAdapter(t *testing.T) {
	stage := t.TempDir()
	for _, adapter := range []string{adapterGrok, adapterAntigravity} {
		path, err := stagePierAgentLayerAdapter(ExecutionRequest{
			Arm: ArmBaseline, Model: Model{Adapter: adapter},
		}, stage)
		if err != nil {
			t.Fatalf("stage bare %s adapter: %v", adapter, err)
		}
		if path != stage {
			t.Fatalf("bare %s adapter path = %q, want %q", adapter, path, stage)
		}
		actual, err := os.ReadFile(filepath.Join(path, "pier_agent_layer.py")) // #nosec G304 -- test-owned staging path.
		if err != nil {
			t.Fatal(err)
		}
		expected, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, expected) {
			t.Fatalf("bare %s adapter differs from embedded release asset", adapter)
		}
	}
}

func TestBareCustomProviderLoadsThroughPinnedPierFactory(t *testing.T) {
	stage := t.TempDir()
	path, err := stagePierAgentLayerAdapter(ExecutionRequest{
		Arm: ArmBaseline, Model: Model{Adapter: adapterGrok},
	}, stage)
	if err != nil {
		t.Fatal(err)
	}
	script := `
from pathlib import Path
from pier.agents.factory import AgentFactory

agent = AgentFactory.create_agent_from_import_path(
    "pier_agent_layer:AgentLayerGrok",
    logs_dir=Path("/tmp/agent-layer-pier-factory-test"),
    model_name="grok-4.5",
    version="1.0.5",
    treatment_agent="grok",
    treatment_model="grok-4.5",
    treatment_reasoning_effort="low",
    treatment_mode="bare",
)
assert agent.name() == "grok"
`
	command := exec.CommandContext(t.Context(), "uvx", "--from", "datacurve-pier=="+PierVersion, "python", "-c", script) // #nosec G204 -- pinned Pier constructs the embedded benchmark adapter.
	command.Env = replaceEnvValue(os.Environ(), "PYTHONPATH", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pinned Pier failed to construct staged bare adapter: %v\n%s", err, output)
	}
}

func TestPierAdapterStagingPreservesNativeAndTreatmentBoundaries(t *testing.T) {
	stage := t.TempDir()
	path, err := stagePierAgentLayerAdapter(ExecutionRequest{
		Arm: ArmBaseline, Model: Model{Adapter: adapterCodex},
	}, stage)
	if err != nil || path != "" {
		t.Fatalf("native bare adapter path = %q, %v", path, err)
	}
	if _, err := os.Stat(filepath.Join(stage, "pier_agent_layer.py")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("native bare execution staged a custom adapter: %v", err)
	}

	bundleRoot := t.TempDir()
	bundlePath := filepath.Join(bundleRoot, "adapter", "pier_agent_layer.py")
	path, err = stagePierAgentLayerAdapter(ExecutionRequest{
		Arm: ArmTreatment, Model: Model{Adapter: adapterCodex},
		Bundle: &TreatmentBundle{AdapterPath: bundlePath},
	}, stage)
	if err != nil || path != filepath.Dir(bundlePath) {
		t.Fatalf("treatment adapter path = %q, %v", path, err)
	}
}

func TestCredentialSecretValuesIncludesGrokJWTKey(t *testing.T) {
	jwt := "test-only-grok-jwt"
	email := "test-only@example.invalid"
	userID := "test-only-user-id"
	values := credentialSecretValues([]byte(`{"key":"` + jwt + `","email":"` + email + `","user_id":"` + userID + `","max_user_inference":"fixture"}`))
	found := map[string]bool{}
	for _, value := range values {
		found[string(value)] = true
	}
	if !found[jwt] || !found[email] || !found[userID] {
		t.Fatalf("Grok credential secrets were not collected for artifact redaction: %q", values)
	}
}

func TestProviderPreflightMapComparisonRejectsMissingOrChangedCapacity(t *testing.T) {
	if !sameIntMap(map[string]int{"grok": 1}, map[string]int{"grok": 1}) {
		t.Fatal("identical provider capacity maps differ")
	}
	if sameIntMap(map[string]int{"grok": 1}, map[string]int{"grok": 1, "antigravity": 1}) {
		t.Fatal("provider capacity maps with different members compared equal")
	}
	if sameIntMap(map[string]int{"grok": 1}, map[string]int{"grok": 2}) {
		t.Fatal("provider capacity maps with different capacity compared equal")
	}
}

func TestBenchmarkProviderRegistryCoversEverySelectableModel(t *testing.T) {
	for _, model := range supportedModels {
		provider, err := benchmarkProvider(model.Adapter)
		if err != nil || provider.Binary == "" || provider.PierAgent == "" || provider.ClientVersion != model.ProviderClientVersion {
			t.Fatalf("model %#v provider = %#v, %v", model, provider, err)
		}
	}
}

func TestStreamCostUsesCachedInputAndDoesNotDoubleCountReasoning(t *testing.T) {
	pricing, err := loadBenchmarkPricing()
	if err != nil {
		t.Fatal(err)
	}
	input, output, cached, creation := 100, 10, 20, 0
	cost, err := priceStreamRequest("fixture", adapterGrok, "grok-4.5", streamTokenUsage{
		InputTokens: &input, OutputTokens: &output, CacheReadInputTokens: &cached, CacheCreationTokens: &creation,
	}, pricing)
	if err != nil {
		t.Fatal(err)
	}
	// (100*$2 + 20*$0.30 + 10*$6) / 1M. Grok reports ordinary input and cache
	// reads separately. Reasoning is explicitly included in
	// output by the pricing asset and therefore never appears in this sum.
	want := 266.0 / 1_000_000
	if cost.minimum != want || cost.maximum != want {
		t.Fatalf("cost=%#v, want %g", cost, want)
	}
}

func TestStreamCostChargesCacheCreationOnceAndBoundsUnknownContextTier(t *testing.T) {
	pricing, err := loadBenchmarkPricing()
	if err != nil {
		t.Fatal(err)
	}
	input, output, cached, creation, reasoning := 10, 5, 20, 30, 3
	cost, err := priceStreamRequest("fixture", adapterGrok, modelGrok45, streamTokenUsage{
		InputTokens: &input, OutputTokens: &output, CacheReadInputTokens: &cached,
		CacheCreationTokens: &creation, ReasoningTokens: &reasoning,
	}, pricing)
	if err != nil {
		t.Fatal(err)
	}
	// Cache creation joins ordinary input once: (10+30)*$2 + 20*$0.30 + 5*$6.
	want := 116.0 / 1_000_000
	if cost.minimum != want || cost.maximum != want {
		t.Fatalf("exact cache-creation cost=%#v, want %g", cost, want)
	}
	// Without a cache field, total context is unknowable. The normalizer must
	// report short/long bounds instead of assuming the short tier.
	cost, err = priceStreamRequest("fixture", adapterGrok, modelGrok45, streamTokenUsage{InputTokens: &input, OutputTokens: &output}, pricing)
	if err != nil {
		t.Fatal(err)
	}
	if cost.minimum >= cost.maximum {
		t.Fatalf("missing context evidence did not retain a real price range: %#v", cost)
	}
	reasoning = output + 1
	if _, err := priceStreamRequest("fixture", adapterGrok, modelGrok45, streamTokenUsage{
		InputTokens: &input, OutputTokens: &output, CacheReadInputTokens: &cached,
		CacheCreationTokens: &creation, ReasoningTokens: &reasoning,
	}, pricing); err == nil || !strings.Contains(err.Error(), "reasoning") {
		t.Fatalf("invalid reasoning usage accepted: %v", err)
	}
}

func TestStreamCostReconcilesOneCoordinatorAndEachCompletedChildExactlyOnce(t *testing.T) {
	stage := t.TempDir()
	coordinator := filepath.Join(stage, "jobs", "task", "agent", "grok.jsonl")
	child := filepath.Join(stage, "jobs", "task", "agent-layer-dispatch", "child.stdout")
	for path, session := range map[string]string{coordinator: "coordinator", child: "child"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		data := "{\"type\":\"usage\",\"usage\":{\"input_tokens\":10,\"output_tokens\":5,\"cache_read_input_tokens\":0,\"cache_creation_input_tokens\":0,\"reasoning_tokens\":0}}\n" +
			"{\"type\":\"end\",\"sessionId\":\"" + session + "\",\"stopReason\":\"end_turn\",\"total_cost_usd\":0.001}\n"
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	record := filepath.Join(stage, "jobs", "task", "agent-layer-dispatch", "child.json")
	if err := os.WriteFile(record, []byte(`{"provider_session_id":"child","state":"completed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		antigravityMCPPreflightEvidence: `{"mcpServers":{"agent-layer":{}}}`,
		grokMCPPreflightEvidence:        `[{"name":"agent-layer"}]`,
		dispatchOptionsPreflightFile:    `{"agents":[]}`,
	} {
		if err := os.WriteFile(filepath.Join(filepath.Dir(record), name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cost, err := streamProviderAttemptCost(stage, adapterGrok, modelGrok45)
	if err != nil {
		t.Fatal(err)
	}
	if cost.invocations != 2 || !cost.providerReported || cost.total.minimum != 0.002 || cost.total.minimum != cost.coordinator.minimum+cost.child.minimum || cost.child.minimum == 0 || cost.coordinator.minimum == 0 {
		t.Fatalf("stream cost did not reconcile coordinator and child exactly once: %#v", cost)
	}
	if err := os.WriteFile(filepath.Join(stage, "jobs", "task", "agent-layer-dispatch", "duplicate.json"), []byte(`{"provider_session_id":"child","state":"completed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := streamProviderAttemptCost(stage, adapterGrok, modelGrok45); err == nil || !strings.Contains(err.Error(), "expected exactly one") {
		t.Fatalf("duplicated child dispatch record was accepted: %v", err)
	}
}

func TestGrokStreamCostRejectsInvalidReportedTotalAndSupportsLegacyFallback(t *testing.T) {
	stage := t.TempDir()
	path := filepath.Join(stage, "jobs", "task", "agent", "grok.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	usage := `{"type":"usage","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"reasoning_tokens":0}}` + "\n"
	if err := os.WriteFile(path, []byte(usage+`{"type":"end","sessionId":"coordinator","stopReason":"end_turn","total_cost_usd":-1}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := streamProviderAttemptCost(stage, adapterGrok, modelGrok45); err == nil || !strings.Contains(err.Error(), "invalid reported cost") {
		t.Fatalf("invalid Grok reported total accepted: %v", err)
	}
	if err := os.WriteFile(path, []byte(usage+`{"type":"end","sessionId":"coordinator","stopReason":"end_turn"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cost, err := streamProviderAttemptCost(stage, adapterGrok, modelGrok45)
	if err != nil {
		t.Fatal(err)
	}
	if cost.providerReported || cost.invocations != 1 || cost.total.minimum <= 0 {
		t.Fatalf("legacy Grok token-pricing fallback = %#v", cost)
	}
}

func TestStreamUsageParsingRejectsMalformedProviderEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider.jsonl")
	for _, test := range []struct {
		name     string
		provider string
		stream   string
		want     string
	}{
		{"malformed JSON", adapterGrok, "{not-json}\n", "decode grok usage"},
		{"abnormal Grok end", adapterGrok, `{"type":"usage","usage":{"input_tokens":1,"output_tokens":1}}` + "\n" + `{"type":"end","sessionId":"id","stopReason":"error"}` + "\n", "ended with"},
		{"usage after Grok end", adapterGrok, `{"type":"usage","usage":{"input_tokens":1,"output_tokens":1}}` + "\n" + `{"type":"end","sessionId":"id","stopReason":"end_turn"}` + "\n" + `{"type":"usage","usage":{"input_tokens":1,"output_tokens":1}}` + "\n", "records after the terminal end event"},
		{"failed Antigravity result", adapterAntigravity, `{"event":"result","result":{"conversation_id":"id","status":"ERROR","usage":{"input_tokens":1,"output_tokens":1,"cache_read_tokens":0}}}` + "\n", "ended with"},
		{"unsupported provider", "unsupported", "{}\n", "unsupported stream cost provider"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.stream), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := parseStreamProviderUsage(path, test.provider); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("provider stream error = %v, want %q", err, test.want)
			}
		})
	}
	if _, _, _, err := parseStreamProviderUsage(filepath.Join(t.TempDir(), "missing"), adapterGrok); err == nil {
		t.Fatal("missing provider stream was accepted")
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 16*1024*1024+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := parseStreamProviderUsage(path, adapterGrok); err == nil {
		t.Fatal("oversized provider stream line was accepted")
	}
}

func TestAntigravityPricingRejectsCacheBeyondTotalInput(t *testing.T) {
	pricing, err := loadBenchmarkPricing()
	if err != nil {
		t.Fatal(err)
	}
	input, output, cached, creation := 1, 1, 2, 0
	if _, err := priceStreamRequest("fixture", adapterAntigravity, modelGeminiFlashLow, streamTokenUsage{
		InputTokens: &input, OutputTokens: &output, CacheReadInputTokens: &cached, CacheCreationTokens: &creation,
	}, pricing); err == nil || !strings.Contains(err.Error(), "more cached than total") {
		t.Fatalf("impossible Antigravity cache usage accepted: %v", err)
	}
	negative := -1
	if _, err := priceStreamRequest("fixture", adapterAntigravity, modelGeminiFlashLow, streamTokenUsage{
		InputTokens: &input, OutputTokens: &output, CacheReadInputTokens: &negative, CacheCreationTokens: &creation,
	}, pricing); err == nil || !strings.Contains(err.Error(), "invalid cache") {
		t.Fatalf("negative Antigravity cache usage accepted: %v", err)
	}
	if _, err := priceStreamRequest("fixture", adapterAntigravity, modelGeminiFlashLow, streamTokenUsage{OutputTokens: &output}, pricing); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete Antigravity usage accepted: %v", err)
	}
}

func TestAntigravityStreamCostRequiresOneSuccessfulCoordinatorTerminal(t *testing.T) {
	stage := t.TempDir()
	path := filepath.Join(stage, "jobs", "task", "agent", "antigravity.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	stream := `{"event":"result","result":{"status":"SUCCESS","conversation_id":"coordinator","response":"answer","usage":{"input_tokens":10,"output_tokens":5,"thinking_tokens":0,"cache_read_tokens":2,"total_tokens":15}}}` + "\n"
	if err := os.WriteFile(path, []byte(stream), 0o600); err != nil {
		t.Fatal(err)
	}
	cost, err := streamProviderAttemptCost(stage, adapterAntigravity, modelGeminiFlashLow)
	if err != nil {
		t.Fatal(err)
	}
	want := 57.3 / 1_000_000 // (10 total - 2 cached)*$1.50 + 2*$0.15 + 5*$9.
	if cost.invocations != 1 || cost.providerReported || cost.coordinator.minimum != want || cost.child.minimum != 0 || cost.total != cost.coordinator {
		t.Fatalf("Antigravity coordinator cost = %#v", cost)
	}
	if err := os.WriteFile(path, []byte(stream+stream), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := streamProviderAttemptCost(stage, adapterAntigravity, modelGeminiFlashLow); err == nil || !strings.Contains(err.Error(), "no single successful terminal") {
		t.Fatalf("duplicated Antigravity terminal accepted: %v", err)
	}
}
