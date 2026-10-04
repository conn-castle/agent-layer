package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHerdRCommandPinsDevelopmentExecutableAtSync(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate", "al")
	if err := os.MkdirAll(filepath.Dir(candidate), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(root, "live", "bin", "al")
	if err := os.MkdirAll(filepath.Dir(live), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(candidate, live); err != nil {
		t.Fatal(err)
	}
	canonicalCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "1")
	t.Setenv("AL_DEV_EXECUTABLE", live)
	command := herdrCommand("muse", "/scratch/project")
	if !strings.Contains(command, "AL_DEV_BYPASS_VERSION_DISPATCH=1") || !strings.Contains(command, "AL_DEV_EXECUTABLE='"+canonicalCandidate+"'") || !strings.Contains(command, "exec '"+canonicalCandidate+"' hook herdr muse '/scratch/project'") {
		t.Fatalf("development command did not pin its source executable: %q", command)
	}
	if strings.Contains(command, "$AL_DEV_EXECUTABLE") {
		t.Fatalf("command relies on hook environment: %q", command)
	}
}

func TestHerdRCommandKeepsCanonicalCandidateWhenLiveSymlinkChanges(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"candidate/al-review", "candidate/al-corrected", "live/bin/al"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	first := filepath.Join(root, "candidate", "al-review")
	second := filepath.Join(root, "candidate", "al-corrected")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	live := filepath.Join(root, "live", "bin", "al")
	if err := os.Symlink(first, live); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "1")
	t.Setenv("AL_DEV_EXECUTABLE", live)
	firstCommand := herdrCommand("codex", root)
	canonicalFirst, err := filepath.EvalSymlinks(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, live); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(firstCommand, "'"+canonicalFirst+"'") || strings.Contains(firstCommand, live) {
		t.Fatalf("generated hook did not retain canonical executable: %q", firstCommand)
	}
}

func TestHerdRCommandUsesReleaseResolutionWithoutDevelopmentContext(t *testing.T) {
	t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "")
	t.Setenv("AL_DEV_EXECUTABLE", "/scratch/candidate/al")
	if got, want := herdrCommand("claude"), "exec al hook herdr claude # agent-layer-herdr"; got != want {
		t.Fatalf("release command = %q, want %q", got, want)
	}
}

func TestCodexHerdRReplacementPreservesNativeTrustStateInsideOldMarker(t *testing.T) {
	editor := newCodexTomlEditor(strings.Join([]string{
		codexHerdRBeginMarker,
		"[[hooks.SessionStart]]",
		"[[hooks.SessionStart.hooks]]",
		`type = "command"`,
		`command = "exec al hook herdr codex # agent-layer-herdr"`,
		"timeout = 5",
		"[hooks.state]",
		`trusted_hash = "sha256:keep"`,
		codexHerdREndMarker,
	}, "\n"))
	if _, err := editor.applyCodexHerdRHook("config.toml", true); err != nil {
		t.Fatal(err)
	}
	output := editor.render()
	if !strings.Contains(output, "[hooks.state]\ntrusted_hash = \"sha256:keep\"") || strings.Count(output, codexHerdRBeginMarker) != 1 {
		t.Fatalf("native trust state was not preserved during replacement:\n%s", output)
	}
	if !strings.Contains(output, "[[hooks.SessionStart]]") || strings.Contains(output, "[[hooks.UserPromptSubmit]]") || strings.Contains(output, "[[hooks.Stop]]") {
		t.Fatalf("Codex HerdR must remain session-only:\n%s", output)
	}
	if strings.Contains(output, "PermissionRequest") {
		t.Fatalf("Codex HerdR must not infer blocked from permission requests:\n%s", output)
	}
	if _, err := editor.applyCodexHerdRHook("config.toml", false); err != nil {
		t.Fatal(err)
	}
	output = editor.render()
	if strings.Contains(output, codexHerdRBeginMarker) || !strings.Contains(output, "trusted_hash = \"sha256:keep\"") {
		t.Fatalf("native trust state was not preserved during cleanup:\n%s", output)
	}
}

func TestMuseHerdRHooksRegisterFirstPromptAndPreserveBothEventGroups(t *testing.T) {
	root := t.TempDir()
	document := map[string]any{hooksKey: map[string]any{
		herdrMuseEvent:          []any{map[string]any{hooksKey: []any{map[string]any{"type": "command", "command": "echo start-user"}}}},
		herdrMuseFirstTurnEvent: []any{map[string]any{hooksKey: []any{map[string]any{"type": "command", "command": "echo prompt-user"}}}},
	}}
	if err := injectMuseHerdRHook(document, true, root); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{herdrMuseEvent, herdrMuseFirstTurnEvent} {
		entries := document[hooksKey].(map[string]any)[event].([]any)
		if len(entries) != 2 || !isHerdRHandler(entries[1].(map[string]any)[hooksKey].([]any)[0]) {
			t.Fatalf("Muse %s hooks = %#v", event, entries)
		}
	}
	if err := injectMuseHerdRHook(document, true, root); err != nil {
		t.Fatal(err)
	}
	if err := injectMuseHerdRHook(document, false, root); err != nil {
		t.Fatal(err)
	}
	hooks := document[hooksKey].(map[string]any)
	for _, event := range []string{herdrMuseEvent, herdrMuseFirstTurnEvent} {
		entries := hooks[event].([]any)
		if len(entries) != 1 || isHerdRHandler(entries[0].(map[string]any)[hooksKey].([]any)[0]) {
			t.Fatalf("Muse %s cleanup = %#v", event, entries)
		}
	}
}

func TestHerdRHookWritersRejectSymlinkedProviderDirectories(t *testing.T) {
	for _, directory := range []string{
		filepath.Join(".github", "hooks"),
		filepath.Join(".grok", "hooks"),
		filepath.Join(".agy", "config"),
	} {
		t.Run(directory, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			path := filepath.Join(root, directory)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
			if err := ensureHerdRHookPathContained(RealSystem{}, root, filepath.Join(path, "agent-layer-herdr.json")); err == nil {
				t.Fatal("symlinked HerdR hook directory was accepted")
			}
		})
	}
}

func TestProviderRecoveryProjectionPreservesUserHooksOnResyncAndDisable(t *testing.T) {
	for _, provider := range []string{"grok"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			path := providerHerdRHookPath(root, provider)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			event := "SessionStart"
			user := map[string]any{hooksKey: []any{map[string]any{"type": "command", "command": "echo keep-user"}}}
			document := map[string]any{"hooks": map[string]any{event: []any{user}}, "user_metadata": "keep-metadata"}
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := writeProviderHerdRHook(RealSystem{}, root, provider, event, herdrCommand(provider)); err != nil {
					t.Fatal(err)
				}
			}
			data, err = os.ReadFile(path) // #nosec G304 -- test-owned hook path.
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(data), "keep-user") != 1 || strings.Count(string(data), agentLayerHerdRMarker) != 1 {
				t.Fatalf("resync lost or duplicated hooks: %s", data)
			}
			for range 2 {
				if err := cleanProviderHerdRHook(RealSystem{}, root, provider); err != nil {
					t.Fatal(err)
				}
			}
			data, err = os.ReadFile(path) // #nosec G304 -- test-owned hook path.
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "keep-user") || !strings.Contains(string(data), "keep-metadata") || strings.Contains(string(data), agentLayerHerdRMarker) {
				t.Fatalf("disable changed user hooks: %s", data)
			}
		})
	}
}

func TestRunHerdRCleanupPreservesExistingProviderSymlinkPolicy(t *testing.T) {
	for _, directory := range []string{filepath.Join(".github", "hooks"), filepath.Join(".grok", "hooks"), filepath.Join(".agy", "config")} {
		t.Run(directory, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			if err := copyFixtureRepo(filepath.Join("testdata", "fixture-repo"), root); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(root, ".agent-layer", "config.toml")
			data, err := os.ReadFile(configPath) // #nosec G304 -- path is within the test-owned fixture.
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, []byte(strings.ReplaceAll(string(data), "enabled = true", "enabled = false")), 0o600); err != nil { // #nosec G703 -- fixed config path inside the test-owned fixture.
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, directory)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, directory)); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(outside, "user-hook.json")
			if err := os.WriteFile(sentinel, []byte("preserve"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Run(root); err != nil {
				// Grok's pre-existing chime cleanup rejects this shared directory.
				// Recovery cleanup must not introduce an earlier/different failure.
				if directory != filepath.Join(".grok", "hooks") || !strings.Contains(err.Error(), "cleaning Agent Layer chime hooks") {
					t.Fatalf("recovery cleanup changed the existing symlink policy: %v", err)
				}
			} else if directory == filepath.Join(".grok", "hooks") {
				t.Fatal("expected existing Grok chime symlink policy to remain enforced")
			}
			got, err := os.ReadFile(sentinel) // #nosec G304 -- fixed sentinel inside the test-owned fixture.
			if err != nil || string(got) != "preserve" {
				t.Fatalf("unrelated provider state changed: %q %v", got, err)
			}
		})
	}
}

func TestHerdRHookCanonicalRootMatchesLogicalAndPhysicalLaunch(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if got, want := herdrCommand("codex", link), herdrCommand("codex", root); got != want {
		t.Fatalf("logical root changes trusted hook: got %q want %q", got, want)
	}
}
