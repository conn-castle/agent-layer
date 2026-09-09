package wizard

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestMain(m *testing.M) {
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest { return agentoptions.DiscoveryRequest{} }
	os.Exit(m.Run())
}

func TestModelDiscoveryPrefetchWaitsAndReusesResult(t *testing.T) {
	original := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = original })
	path := filepath.Join(t.TempDir(), "agy")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'new-model\\tNew Model\\n'\n"), 0o700); err != nil { // #nosec G306 -- executable harness fixture.
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var lookups atomic.Int32
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		return agentoptions.DiscoveryRequest{Live: true, LookPath: func(string) (string, error) {
			if lookups.Add(1) == 1 {
				close(started)
			}
			<-release
			return path, nil
		}}
	}
	var out bytes.Buffer
	cache := &wizardOptionDiscoveryCache{out: &out}
	cache.prefetch(AgentAntigravity)
	<-started
	// A pending discovery must be awaited rather than replaced by stale options.
	timer := time.AfterFunc(30*time.Millisecond, func() { close(release) })
	defer timer.Stop()
	ui := &MockUI{SelectFunc: func(_ string, options []string, current *string) error {
		if !slices.Contains(options, "new-model") {
			t.Errorf("missing discovered model: %v", options)
		}
		*current = "new-model"
		return nil
	}}
	var selected string
	for range 2 {
		if err := cache.selectModel(ui, AgentAntigravity, messages.WizardAntigravityModelTitle, &selected); err != nil {
			t.Fatal(err)
		}
	}
	if lookups.Load() != 1 {
		t.Fatalf("discovery ran %d times", lookups.Load())
	}
	if !bytes.Contains(out.Bytes(), []byte("Discovering antigravity models")) {
		t.Fatal("missing progress message")
	}
}

func TestModelDiscoveryFailureAllowsExplicitSelection(t *testing.T) {
	original := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = original })
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		return agentoptions.DiscoveryRequest{Live: true, LookPath: func(string) (string, error) { return "", errors.New("harness missing") }}
	}
	for _, agent := range []string{AgentCopilotCLI, AgentClaude, AgentCodex, AgentGrok, AgentAntigravity} {
		for _, selection := range []string{messages.WizardLeaveBlankOption, messages.WizardCustomOption} {
			t.Run(agent+"/"+selection, func(t *testing.T) {
				warned := false
				ui := &MockUI{
					NoteFunc: func(title, body string) error {
						warned = true
						if !strings.Contains(title, agent) || !strings.Contains(body, "harness missing") {
							t.Fatalf("missing discovery failure context: %s: %s", title, body)
						}
						return nil
					},
					SelectFunc: func(_ string, options []string, current *string) error {
						if !warned {
							t.Fatal("picker opened without surfacing discovery failure")
						}
						if !slices.Equal(options, []string{messages.WizardLeaveBlankOption, messages.WizardCustomOption}) {
							t.Fatalf("unexpected suggestions after failed discovery: %v", options)
						}
						*current = selection
						return nil
					},
					InputFunc: func(_ string, current *string) error {
						if *current != "my-custom-model" {
							t.Fatalf("lost configured model: %q", *current)
						}
						return nil
					},
				}
				cache := &wizardOptionDiscoveryCache{}
				selected := "my-custom-model"
				if err := cache.selectModel(ui, agent, "Model", &selected); err != nil {
					t.Fatal(err)
				}
				want := "my-custom-model"
				if selection == messages.WizardLeaveBlankOption {
					want = ""
				}
				if selected != want {
					t.Fatalf("selected=%q, want %q", selected, want)
				}
			})
		}
	}
}

func TestModelDiscoveryFailureNotePreservesNavigation(t *testing.T) {
	original := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = original })
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		return agentoptions.DiscoveryRequest{Live: true, LookPath: func(string) (string, error) { return "", errors.New("harness missing") }}
	}
	for _, navigation := range []error{errWizardBack, errWizardCancelled} {
		ui := &MockUI{
			NoteFunc:   func(string, string) error { return navigation },
			SelectFunc: func(string, []string, *string) error { t.Fatal("picker opened after navigation"); return nil },
		}
		selected := "my-custom-model"
		cache := &wizardOptionDiscoveryCache{}
		if err := cache.selectModel(ui, AgentCopilotCLI, "Model", &selected); !errors.Is(err, navigation) {
			t.Fatalf("error=%v", err)
		}
		if selected != "my-custom-model" {
			t.Fatalf("selected=%q", selected)
		}
	}
}

func TestPrefetchAllModelsRunsConcurrently(t *testing.T) {
	original := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = original })
	started := make(chan string, 5)
	release := make(chan struct{})
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		return agentoptions.DiscoveryRequest{Live: true, LookPath: func(name string) (string, error) { started <- name; <-release; return "", os.ErrNotExist }}
	}
	cache := &wizardOptionDiscoveryCache{}
	cache.prefetchAll()
	for range 5 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("discovery did not start concurrently")
		}
	}
	close(release)
	for _, entry := range cache.entries {
		<-entry.done
	}
}

func TestScriptedModelSelectionDoesNotDiscover(t *testing.T) {
	original := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = original })
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		t.Fatal("explicit scripted model caused discovery")
		return agentoptions.DiscoveryRequest{}
	}
	ui := &ScriptedUI{answers: scriptedAnswers{Select: map[string]string{messages.WizardAntigravityModelTitle: "future-native-model"}}, used: map[string]struct{}{}}
	var value string
	cache := &wizardOptionDiscoveryCache{}
	if err := cache.selectModel(ui, AgentAntigravity, messages.WizardAntigravityModelTitle, &value); err != nil {
		t.Fatal(err)
	}
	if value != "future-native-model" {
		t.Fatalf("configured value = %q", value)
	}
	if err := ui.AssertComplete(); err != nil {
		t.Fatal(err)
	}
}

func TestWizardStartsDiscoveryBeforeFirstPromptAndCancelsOnExit(t *testing.T) {
	original := wizardOptionDiscoveryRequestFunc
	t.Cleanup(func() { wizardOptionDiscoveryRequestFunc = original })
	executable := filepath.Join(t.TempDir(), "harness")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec sleep 60\n"), 0o700); err != nil { // #nosec G306 -- executable harness fixture.
		t.Fatal(err)
	}
	started := make(chan string, 5)
	wizardOptionDiscoveryRequestFunc = func() agentoptions.DiscoveryRequest {
		return agentoptions.DiscoveryRequest{Live: true, LookPath: func(name string) (string, error) {
			started <- name
			return executable, nil
		}}
	}
	cache := &wizardOptionDiscoveryCache{}
	ui := &MockUI{SelectFunc: func(_ string, _ []string, _ *string) error {
		for range 5 {
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("discovery was not started before the first prompt")
			}
		}
		return errWizardCancelled
	}}
	// Even initially disabled agents must be ready if enabled on the next page.
	choices := NewChoices()
	choices.ApprovalMode = "all"
	if err := promptWizardFlow(t.TempDir(), ui, choices, cache); !errors.Is(err, errWizardCancelled) {
		t.Fatalf("flow error=%v", err)
	}
	if cache.ctx.Err() == nil {
		t.Fatal("wizard exit did not cancel discovery")
	}
	for _, entry := range cache.entries {
		select {
		case <-entry.done:
		case <-time.After(3 * time.Second):
			t.Fatal("discovery worker outlived wizard cancellation")
		}
	}
}
