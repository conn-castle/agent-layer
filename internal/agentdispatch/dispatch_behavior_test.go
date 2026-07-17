package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/clients"
)

func TestOptionsExposeOnlyV013Capabilities(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	options, err := BuildOptions(OptionsRequest{
		Root: root,
		Env:  []string{clients.EnvDispatchCallerAgent + "=" + AgentCodex},
		LookPath: func(name string) (string, error) {
			if name == "claude" {
				return "/mock/claude", nil
			}
			return "", exec.ErrNotFound
		},
		VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil },
	})
	if err != nil {
		t.Fatalf("BuildOptions: %v", err)
	}
	if options.Caller.Agent != AgentCodex || !options.Caller.Known {
		t.Fatalf("caller = %#v", options.Caller)
	}
	if got := strings.Join(options.Random.Pool, ","); got != AgentClaude {
		t.Fatalf("random pool = %q", got)
	}
	raw, err := json.Marshal(options)
	if err != nil {
		t.Fatalf("marshal options: %v", err)
	}
	for _, legacy := range []string{"dispatch_capable", "streaming"} {
		if bytes.Contains(raw, []byte(legacy)) {
			t.Fatalf("v1 field %q leaked into options: %s", legacy, raw)
		}
	}
	if !bytes.Contains(raw, []byte(`"capabilities"`)) {
		t.Fatalf("v0.13 capabilities absent: %s", raw)
	}
}

func TestOptionsReportExactUnsupportedInstalledVersion(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binary := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho 1.0.0\n"), 0o700); err != nil { // #nosec G306 -- test provider must be executable.
		t.Fatal(err)
	}
	options, err := BuildOptions(OptionsRequest{
		Root: root,
		Env:  []string{},
		LookPath: func(name string) (string, error) {
			if name == "claude" {
				return binary, nil
			}
			return "", exec.ErrNotFound
		},
	})
	if err != nil {
		t.Fatalf("BuildOptions: %v", err)
	}
	for _, target := range options.Targets {
		if target.Agent != AgentClaude {
			continue
		}
		if !target.Installed || target.InstalledVersion != "1.0.0" {
			t.Fatalf("options hid the exact installed version: %#v", target)
		}
		want := "unsupported provider version; install " + supportedProviderVersions[AgentClaude]
		if target.Capabilities.Fresh.Supported || target.Capabilities.Fresh.Reason != want {
			t.Fatalf("fresh capability = %#v", target.Capabilities.Fresh)
		}
		return
	}
	t.Fatal("claude target missing from options")
}

func TestNewerProviderVersionsRemainDispatchable(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	parts := strings.Split(supportedProviderVersions[AgentCodex], ".")
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		t.Fatalf("parse supported codex patch version: %v", err)
	}
	newerVersion := fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+2)
	newerLookup := func(_ string, agent string) (string, error) {
		if agent == AgentCodex {
			return newerVersion, nil
		}
		return supportedProviderVersions[agent], nil
	}
	options, err := BuildOptions(OptionsRequest{
		Root:          root,
		Env:           []string{},
		LookPath:      alwaysFound,
		VersionLookup: newerLookup,
	})
	if err != nil {
		t.Fatalf("BuildOptions: %v", err)
	}
	found := false
	for _, target := range options.Targets {
		if target.Agent != AgentCodex {
			continue
		}
		found = true
		if target.InstalledVersion != newerVersion {
			t.Fatalf("installed version = %q, want %q", target.InstalledVersion, newerVersion)
		}
		if !target.Capabilities.Fresh.Supported || !target.Capabilities.Resume.Supported {
			t.Fatalf("newer-than-tested provider version must stay dispatchable, got fresh=%#v resume=%#v", target.Capabilities.Fresh, target.Capabilities.Resume)
		}
		if !target.RandomEligible {
			t.Fatalf("newer-than-tested provider version must stay random-eligible: %#v", target)
		}
		if !strings.Contains(target.CompatibilityWarning, newerVersion) || !strings.Contains(target.CompatibilityWarning, supportedProviderVersions[AgentCodex]) {
			t.Fatalf("compatibility warning must name installed and tested versions, got %q", target.CompatibilityWarning)
		}
		if len(target.UnavailableReasons) != 0 {
			t.Fatalf("newer-than-tested provider version must not be reported unavailable: %#v", target.UnavailableReasons)
		}
	}
	if !found {
		t.Fatal("codex target missing from options")
	}
	for _, target := range options.Targets {
		if target.Agent != AgentCodex && target.CompatibilityWarning != "" {
			t.Fatalf("version equal to the tested pin must not warn: %#v", target)
		}
	}
	version, err := requireSupportedVersion("/mock/codex", AgentCodex, newerLookup)
	if err != nil {
		t.Fatalf("dispatch gate rejected newer-than-tested provider version: %v", err)
	}
	if version != newerVersion {
		t.Fatalf("dispatch gate version = %q, want %q", version, newerVersion)
	}
	if _, err := requireSupportedVersion("/mock/codex", AgentCodex, func(string, string) (string, error) { return "0.0.1", nil }); err == nil {
		t.Fatal("dispatch gate accepted an older-than-tested provider version")
	}
	_, err = requireSupportedVersion("/mock/codex", AgentCodex, func(string, string) (string, error) { return "0.144", nil })
	requireDispatchExitCode(t, err, ExitUnavailable)
}

func TestNewerProviderVersionDispatchWarnsOnStderrOnly(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	writeDispatchStub(t, binDir, "codex", `printf '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}\n'`)
	newerVersion := "999.0.0"
	var stdout, stderr bytes.Buffer
	err := Run(RunOptions{
		Root:          root,
		Agent:         AgentCodex,
		PromptArgs:    []string{"Review"},
		Stdout:        &stdout,
		Stderr:        &stderr,
		Env:           []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + filepath.Join(t.TempDir(), "codex.log")},
		LookPath:      mockLookPath(binDir),
		VersionLookup: func(string, string) (string, error) { return newerVersion, nil },
	})
	if err != nil {
		t.Fatalf("dispatch to a newer-than-tested provider version must be attempted: %v", err)
	}
	if !strings.Contains(stderr.String(), newerVersion) || !strings.Contains(stderr.String(), supportedProviderVersions[AgentCodex]) {
		t.Fatalf("stderr must carry the compatibility warning with both versions, got %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "warning") {
		t.Fatalf("compatibility warning leaked into the final-answer stdout: %q", stdout.String())
	}
}

func TestTargetResolutionEnforcesEligibility(t *testing.T) {
	cfg := dispatchTestConfig(AgentCodex, AgentClaude)
	versionLookup := func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }
	resolved, err := resolveTarget(cfg, RunOptions{LookPath: alwaysFound, VersionLookup: versionLookup, ChooseRandom: chooseOnly(AgentClaude)}, AgentCodex, true)
	if err != nil {
		t.Fatalf("resolve random target: %v", err)
	}
	if resolved.Target.Name != AgentClaude {
		t.Fatalf("target = %q", resolved.Target.Name)
	}
	_, err = resolveTarget(cfg, RunOptions{Agent: "unknown"}, "", false)
	requireDispatchExitCode(t, err, ExitUsage)
	_, err = chooseRandomTarget(cfg, "", AgentCodex, true, "", "", alwaysFound, versionLookup, chooseOnly(AgentCodex))
	requireDispatchExitCode(t, err, ExitTargetFailure)
	_, err = chooseRandomTarget(cfg, "", "", false, "", "", func(string) (string, error) {
		return "", exec.ErrNotFound
	}, versionLookup, nil)
	requireDispatchExitCode(t, err, ExitUnavailable)
	chooserErr := errors.New("random source failed")
	_, err = chooseRandomTarget(cfg, "", "", false, "", "", alwaysFound, versionLookup, func([]string) (string, error) {
		return "", chooserErr
	})
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if !errors.Is(err, chooserErr) {
		t.Fatalf("chooser error was not preserved: %v", err)
	}
}

func TestRandomResolutionFiltersInvocationOverridesBeforeSelection(t *testing.T) {
	cfg := dispatchTestConfig(AgentCodex, AgentClaude, AgentAntigravity)
	versionLookup := func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }
	tests := []struct {
		name            string
		model           string
		reasoningEffort string
		wantPool        string
	}{
		{name: "no overrides preserve the eligible pool", wantPool: "codex,claude,antigravity"},
		{name: "model override keeps model-capable targets", model: "custom-model", wantPool: "codex,claude,antigravity"},
		{name: "reasoning override excludes unsupported targets", reasoningEffort: "high", wantPool: "codex,claude"},
		{name: "model and reasoning overrides require both capabilities", model: "custom-model", reasoningEffort: "high", wantPool: "codex,claude"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := resolveTarget(cfg, RunOptions{
				Agent:           AgentRandom,
				Model:           tt.model,
				ReasoningEffort: tt.reasoningEffort,
				LookPath:        alwaysFound,
				VersionLookup:   versionLookup,
				ChooseRandom: func(pool []string) (string, error) {
					if got := strings.Join(pool, ","); got != tt.wantPool {
						return "", fmt.Errorf("random pool = %q, want %q", got, tt.wantPool)
					}
					return pool[len(pool)-1], nil
				},
			}, "", false)
			if err != nil {
				t.Fatalf("resolveTarget: %v", err)
			}
			if resolved.Target.Name == AgentAntigravity && strings.TrimSpace(tt.reasoningEffort) != "" {
				t.Fatalf("selected reasoning-incompatible target: %s", resolved.Target.Name)
			}
		})
	}
}

func TestRandomResolutionRejectsEmptyOverridePoolBeforeSelection(t *testing.T) {
	cfg := dispatchTestConfig(AgentAntigravity)
	chooserCalled := false
	resolve := func() error {
		_, err := resolveTarget(cfg, RunOptions{
			Agent:           AgentRandom,
			Model:           "custom-model",
			ReasoningEffort: "high",
			LookPath:        alwaysFound,
			VersionLookup:   func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil },
			ChooseRandom: func([]string) (string, error) {
				chooserCalled = true
				return AgentAntigravity, nil
			},
		}, "", false)
		return err
	}

	first := requireDispatchExitError(t, resolve(), ExitUsage)
	second := requireDispatchExitError(t, resolve(), ExitUsage)
	want := "no agents eligible for `al dispatch --agent random` support all requested overrides (--model and --reasoning-effort); remove unsupported overrides or select an explicit compatible agent"
	if first.Message != want || second.Message != want {
		t.Fatalf("empty compatible pool errors = %q and %q, want %q", first.Message, second.Message, want)
	}
	if chooserCalled {
		t.Fatal("random chooser was invoked for an empty override-compatible pool")
	}
}

func TestRandomEligibilitySkipsIncompatibleProvidersBeforeChoice(t *testing.T) {
	cfg := dispatchTestConfig(AgentCodex, AgentClaude, AgentAntigravity)
	lookups := map[string]int{}
	versionLookup := func(_ string, agent string) (string, error) {
		lookups[agent]++
		if agent == AgentCodex {
			return "0.0.1", nil
		}
		if agent == AgentAntigravity {
			return "", errors.New("unreadable version")
		}
		return "999.0.0", nil
	}
	selected, err := chooseRandomTarget(cfg, "", "", false, "", "", alwaysFound, versionLookup, func(pool []string) (string, error) {
		if got := strings.Join(pool, ","); got != AgentClaude {
			return "", fmt.Errorf("pool = %q, want %q", got, AgentClaude)
		}
		return AgentClaude, nil
	})
	if err != nil {
		t.Fatalf("chooseRandomTarget: %v", err)
	}
	if selected.Target.Name != AgentClaude || selected.InstalledVersion != "999.0.0" {
		t.Fatalf("selected target = %#v", selected)
	}
	for _, agent := range []string{AgentCodex, AgentClaude, AgentAntigravity} {
		if lookups[agent] != 1 {
			t.Fatalf("version lookups[%s] = %d, want 1", agent, lookups[agent])
		}
	}
}

func TestRandomDiscoveryPreservesUnsupportedVersionFacts(t *testing.T) {
	cfg := dispatchTestConfig(AgentCodex)
	target, ok := lookupTarget(AgentCodex)
	if !ok {
		t.Fatal("codex target missing")
	}
	wantReason := "unsupported provider version; install " + supportedProviderVersions[AgentCodex]
	for _, installed := range []string{"0.0.1", "not-semver"} {
		facts := discoverTarget(cfg, target, "", false, alwaysFound, rawTargetVersionDiscovery(func(string, string) (string, error) {
			return installed, nil
		}))
		if facts.RandomEligible || facts.InstalledVersion != installed || facts.Fresh.Reason != wantReason || facts.RandomExclusionReason != wantReason {
			t.Fatalf("random discovery for version %q lost compatibility facts: %#v", installed, facts)
		}
	}
}

func TestRandomResolutionCachesCompatibleProviderVersions(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	callPath := filepath.Join(t.TempDir(), "version-calls")
	codexPath := filepath.Join(binDir, "codex")
	script := "#!/bin/sh\nprintf '%s\\n' " + supportedProviderVersions[AgentCodex] + "\nprintf 'called\\n' >> " + callPath + "\n"
	if err := os.WriteFile(codexPath, []byte(script), 0o700); err != nil { // #nosec G306 -- test provider must be executable.
		t.Fatal(err)
	}
	cfg := dispatchTestConfig(AgentCodex)
	lookPath := func(binary string) (string, error) {
		if binary == "codex" {
			return codexPath, nil
		}
		return "", exec.ErrNotFound
	}
	for range 2 {
		selected, err := chooseRandomTarget(cfg, root, "", false, "", "", lookPath, nil, chooseOnly(AgentCodex))
		if err != nil {
			t.Fatalf("choose random target: %v", err)
		}
		if selected.Target.Name != AgentCodex || selected.InstalledVersion != supportedProviderVersions[AgentCodex] {
			t.Fatalf("selected target = %#v", selected)
		}
	}
	data, err := os.ReadFile(callPath) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	if calls := strings.Count(string(data), "called"); calls != 1 {
		t.Fatalf("compatible random resolution ran provider version discovery %d times, want 1 cached lookup", calls)
	}
}

func TestBuildOptionsResolvesEachProviderBinaryOnce(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	lookups := map[string]int{}
	_, err := BuildOptions(OptionsRequest{
		Root: root,
		Env:  []string{},
		LookPath: func(binary string) (string, error) {
			lookups[binary]++
			return "/mock/" + binary, nil
		},
		VersionLookup: func(_ string, agent string) (string, error) {
			return supportedProviderVersions[agent], nil
		},
	})
	if err != nil {
		t.Fatalf("BuildOptions: %v", err)
	}
	for _, target := range targetRegistry() {
		binary := target.Binary
		if lookups[binary] != 1 {
			t.Fatalf("LookPath(%q) calls = %d, want 1", binary, lookups[binary])
		}
	}
}

func TestDefaultRandomChooserRejectsEmptyPool(t *testing.T) {
	if _, err := defaultRandomChooser(nil); err == nil {
		t.Fatal("defaultRandomChooser accepted an empty pool")
	}
}

func TestDispatchPreflightStopsBeforeProviderLaunch(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentCodex,
		PromptArgs: []string{"Review"},
		Env:        []string{"PATH=/missing"},
		LookPath:   func(string) (string, error) { return "", exec.ErrNotFound },
	})
	requireDispatchExitCode(t, err, ExitUnavailable)

	err = Run(RunOptions{
		Root:            root,
		Agent:           AgentAntigravity,
		ReasoningEffort: "high",
		PromptArgs:      []string{"Review"},
		Env:             []string{"PATH=/missing"},
		LookPath:        alwaysFound,
	})
	requireDispatchExitCode(t, err, ExitUsage)

	disableAgentInDispatchConfig(t, root, AgentCodex)
	err = Run(RunOptions{Root: root, Agent: AgentCodex, PromptArgs: []string{"Review"}, Env: []string{}, LookPath: alwaysFound})
	requireDispatchExitCode(t, err, ExitConfig)
}

func TestFieldOptionsRejectUnknownTarget(t *testing.T) {
	option := fieldOptionWithDiscovery(dispatchTestConfig(AgentCodex), targetMeta{Name: "unknown"}, agentoptions.KindModel, agentoptions.DiscoveryRequest{})
	if option.OverrideSupported || option.AllowCustom || len(option.Suggestions) != 0 {
		t.Fatalf("unknown target option = %#v", option)
	}
	_, err := BuildOptions(OptionsRequest{Root: " "})
	requireDispatchExitCode(t, err, ExitConfig)
}
