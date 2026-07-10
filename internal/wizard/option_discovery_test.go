package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestMain(m *testing.M) {
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		return agentoptions.DiscoveryRequest{}
	}
	os.Exit(m.Run())
}

func TestPromptModelsAntigravityUsesReadyAsyncPrefetch(t *testing.T) {
	orig := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = orig })

	binDir := t.TempDir()
	agyPath := filepath.Join(binDir, "agy")
	script := "#!/bin/sh\nif [ \"$1\" = \"models\" ]; then\n  printf 'Ready Async Model\\nFallback Wizard Model\\n'\nfi\n"
	if err := os.WriteFile(agyPath, []byte(script), 0o700); err != nil { // #nosec G306 -- test writes an executable mock agy stub; the executable bit is required.
		t.Fatalf("write agy stub: %v", err)
	}
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		return agentoptions.DiscoveryRequest{
			LookPath: func(name string) (string, error) {
				if name != "agy" {
					return "", errors.New("unexpected binary lookup")
				}
				return agyPath, nil
			},
			Live:    true,
			Timeout: 30 * time.Second,
		}
	}

	cache := &wizardOptionDiscoveryCache{}
	cache.prefetchAntigravityModels()
	waitForAntigravityModelDiscoveryReady(t, cache)

	choices := NewChoices()
	choices.EnabledAgents[AgentAntigravity] = true
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			if title != messages.WizardAntigravityModelTitle {
				t.Fatalf("unexpected select title %q", title)
			}
			want := []string{messages.WizardLeaveBlankOption, "Ready Async Model", "Fallback Wizard Model", messages.WizardCustomOption}
			if !slices.Equal(options, want) {
				t.Fatalf("options = %v, want %v", options, want)
			}
			*current = "Ready Async Model"
			return nil
		},
	}

	if err := promptModels(ui, choices, cache); err != nil {
		t.Fatalf("promptModels error: %v", err)
	}
	if choices.AntigravityModel != "Ready Async Model" {
		t.Fatalf("AntigravityModel = %q, want ready async selection", choices.AntigravityModel)
	}
}

func TestPromptModelsAntigravityFallsBackWithoutWaitingForPendingAsyncPrefetch(t *testing.T) {
	orig := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = orig })

	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	releaseDiscovery := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseDiscovery()

	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		return agentoptions.DiscoveryRequest{
			LookPath: func(name string) (string, error) {
				if name != "agy" {
					return "", errors.New("unexpected binary lookup")
				}
				startedOnce.Do(func() { close(started) })
				<-release
				return "", errors.New("released pending discovery")
			},
			Live:    true,
			Timeout: 30 * time.Second,
		}
	}

	cache := &wizardOptionDiscoveryCache{}
	cache.prefetchAntigravityModels()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async Antigravity model prefetch to start")
	}

	catalogOptions := config.FieldOptionValues(config.AntigravityModelFieldKey)
	wantOptions := make([]string, 0, len(catalogOptions)+2)
	wantOptions = append(wantOptions, messages.WizardLeaveBlankOption)
	wantOptions = append(wantOptions, catalogOptions...)
	wantOptions = append(wantOptions, messages.WizardCustomOption)

	choices := NewChoices()
	choices.EnabledAgents[AgentAntigravity] = true
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			if title != messages.WizardAntigravityModelTitle {
				t.Fatalf("unexpected select title %q", title)
			}
			if !slices.Equal(options, wantOptions) {
				t.Fatalf("options = %v, want catalog fallback %v", options, wantOptions)
			}
			*current = messages.WizardLeaveBlankOption
			return nil
		},
	}

	result := make(chan error, 1)
	go func() {
		result <- promptModels(ui, choices, cache)
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("promptModels error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("promptModels blocked waiting for pending Antigravity model prefetch")
	}

	releaseDiscovery()
	waitForAntigravityModelDiscoveryReady(t, cache)
}

func TestPromptModelsScriptedAntigravityWaitsForPendingAsyncPrefetch(t *testing.T) {
	orig := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = orig })

	binDir := t.TempDir()
	agyPath := filepath.Join(binDir, "agy")
	script := "#!/bin/sh\nif [ \"$1\" = \"models\" ]; then\n  printf 'Live Scripted Model\\n'\nfi\n"
	if err := os.WriteFile(agyPath, []byte(script), 0o700); err != nil { // #nosec G306 -- test writes an executable mock agy stub; the executable bit is required.
		t.Fatalf("write agy stub: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	releaseDiscovery := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseDiscovery()

	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		return agentoptions.DiscoveryRequest{
			LookPath: func(name string) (string, error) {
				if name != "agy" {
					return "", errors.New("unexpected binary lookup")
				}
				startedOnce.Do(func() { close(started) })
				<-release
				return agyPath, nil
			},
			Live:    true,
			Timeout: 30 * time.Second,
		}
	}

	cache := &wizardOptionDiscoveryCache{}
	cache.prefetchAntigravityModels()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async Antigravity model prefetch to start")
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		releaseDiscovery()
	}()

	choices := NewChoices()
	choices.EnabledAgents[AgentAntigravity] = true
	ui := &ScriptedUI{
		answers: scriptedAnswers{
			Select: map[string]string{messages.WizardAntigravityModelTitle: "Live Scripted Model"},
		},
		used: make(map[string]struct{}),
	}

	if err := promptModels(ui, choices, cache); err != nil {
		t.Fatalf("promptModels error: %v", err)
	}
	if choices.AntigravityModel != "Live Scripted Model" {
		t.Fatalf("AntigravityModel = %q, want live scripted selection", choices.AntigravityModel)
	}
}

func TestAntigravityModelOptionsReturnsCachedCopy(t *testing.T) {
	cache := &wizardOptionDiscoveryCache{
		antigravityModelValues: []string{"Cached Model"},
		antigravityModelsReady: true,
	}

	values := cache.antigravityModelOptions(false)
	values[0] = "Mutated Model"

	again := cache.antigravityModelOptions(false)
	if !slices.Equal(again, []string{"Cached Model"}) {
		t.Fatalf("cached values = %v, want immutable cached model", again)
	}
}

func TestClaudeModelOptionsUsesSharedFieldCatalog(t *testing.T) {
	orig := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = orig })
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		return agentoptions.DiscoveryRequest{
			Live: true,
			LookPath: func(string) (string, error) {
				t.Fatal("Claude model options must not run live discovery without an authoritative CLI source")
				return "", errors.New("unexpected lookup")
			},
		}
	}

	got := modelOptions(AgentClaude)
	want := config.FieldOptionValues(config.ClaudeModelFieldKey)
	if !slices.Equal(got, want) {
		t.Fatalf("Claude model options = %v, want shared catalog %v", got, want)
	}
}

func TestPromptModelsCodexUsesSharedFieldCatalogs(t *testing.T) {
	choices := NewChoices()
	choices.EnabledAgents[AgentCodex] = true

	prompted := make(map[string]bool)
	ui := &MockUI{
		SelectFunc: func(title string, options []string, _ *string) error {
			var catalog []string
			switch title {
			case messages.WizardCodexModelTitle:
				catalog = config.FieldOptionValues(config.CodexModelFieldKey)
			case messages.WizardCodexReasoningEffortTitle:
				catalog = config.FieldOptionValues(config.CodexReasoningEffortFieldKey)
			default:
				t.Fatalf("unexpected select title %q", title)
			}
			want := make([]string, 0, len(catalog)+2)
			want = append(want, messages.WizardLeaveBlankOption)
			want = append(want, catalog...)
			want = append(want, messages.WizardCustomOption)
			if !slices.Equal(options, want) {
				t.Fatalf("%s options = %v, want shared catalog choices %v", title, options, want)
			}
			prompted[title] = true
			return nil
		},
		ConfirmFunc:     func(string, *bool) error { return nil },
		MultiSelectFunc: func(string, []string, *[]string) error { return nil },
	}

	if err := promptModels(ui, choices, &wizardOptionDiscoveryCache{}); err != nil {
		t.Fatalf("promptModels error: %v", err)
	}
	for _, title := range []string{messages.WizardCodexModelTitle, messages.WizardCodexReasoningEffortTitle} {
		if !prompted[title] {
			t.Fatalf("expected Codex prompt %q", title)
		}
	}
}

func TestPrefetchAntigravityModelsStartsOnlyOnce(t *testing.T) {
	orig := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = orig })

	started := make(chan struct{})
	release := make(chan struct{})
	var requestCalls int
	var startedOnce sync.Once
	var releaseOnce sync.Once
	releaseDiscovery := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseDiscovery()

	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		requestCalls++
		return agentoptions.DiscoveryRequest{
			LookPath: func(name string) (string, error) {
				if name != "agy" {
					return "", errors.New("unexpected binary lookup")
				}
				startedOnce.Do(func() { close(started) })
				<-release
				return "", errors.New("released pending discovery")
			},
			Live:    true,
			Timeout: 30 * time.Second,
		}
	}

	cache := &wizardOptionDiscoveryCache{}
	cache.prefetchAntigravityModels()
	cache.prefetchAntigravityModels()
	if requestCalls != 1 {
		t.Fatalf("discovery requests = %d, want one idempotent prefetch", requestCalls)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async Antigravity model prefetch to start")
	}

	releaseDiscovery()
	waitForAntigravityModelDiscoveryReady(t, cache)
	cache.prefetchAntigravityModels()
	if requestCalls != 1 {
		t.Fatalf("discovery requests after completion = %d, want one idempotent prefetch", requestCalls)
	}
}

func TestPromptWizardFlowPrefetchesAntigravityModelsOnceAcrossModelRevisit(t *testing.T) {
	orig := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = orig })

	var discoveryRequests int
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		discoveryRequests++
		return agentoptions.DiscoveryRequest{}
	}

	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll

	var modelPrompts int
	var enableLayerPrompts int
	ui := &MockUI{
		SelectFunc: func(title string, options []string, _ *string) error {
			if title != messages.WizardAntigravityModelTitle {
				return nil
			}
			modelPrompts++
			wantOptions := append([]string{messages.WizardLeaveBlankOption}, config.FieldOptionValues(config.AntigravityModelFieldKey)...)
			wantOptions = append(wantOptions, messages.WizardCustomOption)
			if !slices.Equal(options, wantOptions) {
				t.Fatalf("options = %v, want catalog fallback %v", options, wantOptions)
			}
			return nil
		},
		MultiSelectFunc: func(title string, _ []string, selected *[]string) error {
			switch title {
			case messages.WizardEnableAgentsTitle:
				*selected = []string{AgentAntigravity}
			case messages.WizardEnableDefaultMCPServersTitle:
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			switch title {
			case messages.WizardEnableAgentLayerPrompt:
				enableLayerPrompts++
				if enableLayerPrompts == 1 {
					return errWizardBack
				}
				*value = false
			case messages.WizardEnableWarningsPrompt:
				*value = false
			}
			return nil
		},
	}

	if err := promptWizardFlow(t.TempDir(), ui, choices); err != nil {
		t.Fatalf("promptWizardFlow error: %v", err)
	}
	if modelPrompts != 2 {
		t.Fatalf("model prompts = %d, want 2 after back-navigation revisit", modelPrompts)
	}
	if enableLayerPrompts != 2 {
		t.Fatalf("enable-layer prompts = %d, want 2", enableLayerPrompts)
	}
	if discoveryRequests != 1 {
		t.Fatalf("discovery requests = %d, want exactly one flow-owned prefetch", discoveryRequests)
	}
}

func TestPromptWizardFlowSkipsAntigravityPrefetchWhenAgentDisabled(t *testing.T) {
	orig := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = orig })

	var discoveryRequests int
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		discoveryRequests++
		return agentoptions.DiscoveryRequest{}
	}

	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll
	ui := &MockUI{
		MultiSelectFunc: func(title string, _ []string, selected *[]string) error {
			switch title {
			case messages.WizardEnableAgentsTitle, messages.WizardEnableDefaultMCPServersTitle:
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			switch title {
			case messages.WizardEnableAgentLayerPrompt, messages.WizardEnableWarningsPrompt:
				*value = false
			}
			return nil
		},
	}

	if err := promptWizardFlow(t.TempDir(), ui, choices); err != nil {
		t.Fatalf("promptWizardFlow error: %v", err)
	}
	if discoveryRequests != 0 {
		t.Fatalf("discovery requests = %d, want none when Antigravity stays disabled", discoveryRequests)
	}
}

func waitForAntigravityModelDiscoveryReady(t *testing.T, cache *wizardOptionDiscoveryCache) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		cache.mu.Lock()
		ready := cache.antigravityModelsReady
		cache.mu.Unlock()
		if ready {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for async Antigravity model discovery")
		case <-tick.C:
		}
	}
}
