package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho 9.9.9\n"), 0o700); err != nil { // #nosec G306 -- test provider must be executable.
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
		if !target.Installed || target.InstalledVersion != "9.9.9" {
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

func TestTargetResolutionEnforcesEligibility(t *testing.T) {
	cfg := dispatchTestConfig(AgentCodex, AgentClaude)
	resolved, err := resolveTarget(cfg, RunOptions{LookPath: alwaysFound, ChooseRandom: chooseOnly(AgentClaude)}, AgentCodex, true)
	if err != nil {
		t.Fatalf("resolve random target: %v", err)
	}
	if resolved.Target.Name != AgentClaude {
		t.Fatalf("target = %q", resolved.Target.Name)
	}
	_, err = resolveTarget(cfg, RunOptions{Agent: "unknown"}, "", false)
	requireDispatchExitCode(t, err, ExitUsage)
	_, err = chooseRandomTarget(cfg, AgentCodex, true, alwaysFound, chooseOnly(AgentCodex))
	requireDispatchExitCode(t, err, ExitTargetFailure)
	_, err = chooseRandomTarget(cfg, "", false, func(string) (string, error) {
		return "", exec.ErrNotFound
	}, nil)
	requireDispatchExitCode(t, err, ExitUnavailable)
	chooserErr := errors.New("random source failed")
	_, err = chooseRandomTarget(cfg, "", false, alwaysFound, func([]string) (string, error) {
		return "", chooserErr
	})
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if !errors.Is(err, chooserErr) {
		t.Fatalf("chooser error was not preserved: %v", err)
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
