package agentdispatch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
)

func TestOptionsExposeOnlyStartSelectionFacts(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	options, err := BuildOptions(OptionsRequest{
		Root: root,
		Env:  []string{},
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
	raw, err := json.Marshal(options)
	if err != nil {
		t.Fatalf("marshal options: %v", err)
	}
	for _, legacy := range []string{"caller", "random", "capabilities", "fresh", "resume", "inspect", "dispatch_capable", "streaming"} {
		if bytes.Contains(raw, []byte(legacy)) {
			t.Fatalf("v1 field %q leaked into options: %s", legacy, raw)
		}
	}
	if !bytes.Contains(raw, []byte(`"available"`)) || !bytes.Contains(raw, []byte(`"reasoning_effort"`)) {
		t.Fatalf("selection facts absent: %s", raw)
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
	for _, target := range options.Agents {
		if target.Agent != AgentClaude {
			continue
		}
		want := "unsupported provider version; install " + supportedProviderVersions[AgentClaude]
		if target.Available || target.UnavailableReason != want {
			t.Fatalf("availability = %#v", target)
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
	for _, target := range options.Agents {
		if target.Agent != AgentCodex {
			continue
		}
		found = true
		if !target.Available || target.UnavailableReason != "" {
			t.Fatalf("newer-than-tested provider version must stay available: %#v", target)
		}
	}
	if !found {
		t.Fatal("codex target missing from options")
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
	err := executeFreshDispatch(dispatchExecRequest{
		Root:          root,
		Agent:         AgentCodex,
		Prompt:        "Review",
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

func TestStartRejectsUnknownTarget(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	err := Start(StartOptions{Root: root, Agent: "unknown", Prompt: "Review", Env: []string{}, LookPath: alwaysFound})
	requireDispatchExitCode(t, err, ExitUsage)
}

func TestStartPreflightStopsBeforeWorkerLaunch(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	launcher := func(string, string, string) (launchedWorker, error) {
		t.Fatal("preflight failure launched a worker")
		return launchedWorker{}, nil
	}
	err := Start(StartOptions{
		Root: root, Agent: AgentCodex, Prompt: "Review",
		Env: []string{"PATH=/missing"}, LookPath: func(string) (string, error) { return "", exec.ErrNotFound },
		launchWorker: launcher,
	})
	requireDispatchExitCode(t, err, ExitUnavailable)

	err = Start(StartOptions{
		Root: root, Agent: AgentAntigravity, ReasoningEffort: "high", Prompt: "Review",
		Env: []string{"PATH=/missing"}, LookPath: alwaysFound, launchWorker: launcher,
	})
	requireDispatchExitCode(t, err, ExitUsage)

	disableAgentInDispatchConfig(t, root, AgentCodex)
	err = Start(StartOptions{Root: root, Agent: AgentCodex, Prompt: "Review", Env: []string{}, LookPath: alwaysFound, launchWorker: launcher})
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
