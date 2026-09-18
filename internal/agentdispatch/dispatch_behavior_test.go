package agentdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
)

func TestOptionsCancellationStopsVersionProbes(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binary := filepath.Join(t.TempDir(), "slow-provider")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil { // #nosec G306 -- executable provider fixture.
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	options, err := BuildOptions(OptionsRequest{
		Root: root, Context: ctx, Env: []string{},
		LookPath: func(string) (string, error) { return binary, nil },
	})
	if !errors.Is(err, context.DeadlineExceeded) || options != nil {
		t.Fatalf("cancelled options request returned options=%+v err=%v", options, err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("version probes outlived request cancellation: %v", elapsed)
	}
}

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
	replaceDispatchConfigText(t, root, "[agents.grok]\nenabled = false", "[agents.grok]\nenabled = true")
	lookups := map[string]int{}
	var mu sync.Mutex
	_, err := BuildOptions(OptionsRequest{
		Root: root,
		Env:  []string{},
		LookPath: func(binary string) (string, error) {
			mu.Lock()
			defer mu.Unlock()
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

func TestOptionsVersionQueriesRunConcurrently(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	replaceDispatchConfigText(t, root, "[agents.grok]\nenabled = false", "[agents.grok]\nenabled = true")
	started := make(chan string, 4)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := BuildOptions(OptionsRequest{Root: root, Env: []string{"AL_NO_NETWORK=1"}, LookPath: alwaysFound,
			VersionLookup: func(_ string, agent string) (string, error) {
				started <- agent
				<-release
				return supportedProviderVersions[agent], nil
			}})
		done <- err
	}()
	for range 4 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			<-done
			t.Fatal("provider version queries were serialized")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestOptionsSkipsDisabledProviderQueries(t *testing.T) {
	options := buildTargetOptions(dispatchTestConfig(AgentCodex), agentoptions.DiscoveryRequest{Live: true,
		LookPath: func(binary string) (string, error) {
			if binary != AgentCodex {
				t.Errorf("queried disabled provider %s", binary)
			}
			return "", os.ErrNotExist
		}})
	for _, option := range options {
		if option.Agent != AgentCodex && (option.Available || option.Model.Source != "not_requested" || len(option.Model.Suggestions) != 0) {
			t.Errorf("unexpected disabled provider metadata: %+v", option)
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

	err = Start(StartOptions{
		Root: root, Agent: AgentAntigravity, Model: "gemini-3.5-flash-low", ReasoningEffort: "high", Prompt: "Review",
		Env: []string{"PATH=/missing"}, LookPath: alwaysFound, launchWorker: launcher,
	})
	requireDispatchExitCode(t, err, ExitUsage)

	err = Start(StartOptions{
		Root: root, Agent: AgentAntigravity, Model: "gemini-3.5-flash-low", ReasoningEffort: "low", Prompt: "Review",
		Env: []string{"PATH=/missing"}, LookPath: alwaysFound, launchWorker: launcher,
	})
	requireDispatchExitCode(t, err, ExitUnavailable)

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
