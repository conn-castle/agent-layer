package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
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
	if !strings.Contains(output, "[[hooks.SessionStart]]") || !strings.Contains(output, "[[hooks.UserPromptSubmit]]") || strings.Contains(output, "[[hooks.Stop]]") {
		t.Fatalf("Codex HerdR must install only session-start and first-prompt hooks:\n%s", output)
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
			// Chime cleanup skips a linked provider directory that holds no
			// managed hook; recovery cleanup must not introduce a failure.
			if _, err := Run(root); err != nil {
				t.Fatalf("recovery cleanup changed the existing symlink policy: %v", err)
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

func TestHerdRHooksLeaveSymlinkedHookFilesUntouched(t *testing.T) {
	for _, tc := range []struct {
		name  string
		path  string
		write func(root string) error
		clean func(root string) error
	}{
		{
			name:  "antigravity",
			path:  filepath.Join(".agy", "config", "hooks.json"),
			write: func(root string) error { return writeAgyHerdRHook(RealSystem{}, root) },
			clean: func(root string) error { return cleanAgyHerdRHook(RealSystem{}, root) },
		},
		{
			name: "grok",
			path: filepath.Join(".grok", "hooks", "agent-layer-herdr.json"),
			write: func(root string) error {
				return writeProviderHerdRHook(RealSystem{}, root, herdrGrokProvider, "SessionStart", "exec al hook herdr grok")
			},
			clean: func(root string) error { return cleanProviderHerdRHook(RealSystem{}, root, herdrGrokProvider) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(t.TempDir(), "user-hooks.json")
			content := `{"` + agentLayerHerdRMarker + `":{"enabled":true},"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"exec al hook herdr # ` + agentLayerHerdRMarker + `"}]}]}}`
			if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, tc.path)
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if err := tc.write(root); err == nil {
				t.Fatal("HerdR hook writer accepted a symlinked hook file")
			}
			if err := tc.clean(root); err != nil {
				t.Fatalf("HerdR cleanup failed on a symlinked hook file: %v", err)
			}
			if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("symlinked hook file was replaced or removed: info=%v err=%v", info, err)
			}
			if got, err := os.ReadFile(target); err != nil || string(got) != content { // #nosec G304 -- test-owned path.
				t.Fatalf("linked hook file changed: %q %v", got, err)
			}
		})
	}
}

func TestCleanAgyHerdRHookIgnoresUnownedUnparseableFile(t *testing.T) {
	for _, content := range []string{"", "{not json"} {
		root := t.TempDir()
		path := filepath.Join(root, ".agy", "config", "hooks.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := cleanAgyHerdRHook(RealSystem{}, root); err != nil {
			t.Fatalf("cleanup of unowned hooks.json %q failed: %v", content, err)
		}
		if got, err := os.ReadFile(path); err != nil || string(got) != content { // #nosec G304 -- test-owned path.
			t.Fatalf("unowned hooks.json changed: %q %v", got, err)
		}
	}
}

func TestCodexHerdRExpandsExistingSessionStartAssignment(t *testing.T) {
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents:        config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}},
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
		Env: map[string]string{},
	}
	writeExistingCodexConfig(t, root, codexPartialHeader+`
[hooks]
SessionStart = [{ matcher = "startup", hooks = [{ type = "command", command = "echo user-start", timeout = 3 }] }]
`)
	if err := writeCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeCodexConfig: %v", err)
	}
	first := readCodexConfig(t, root)
	if err := writeCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("second writeCodexConfig: %v", err)
	}
	if second := readCodexConfig(t, root); second != first {
		t.Fatalf("expected idempotent merge\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	sessionStart, ok := parseCodexConfig(t, first)[hooksKey].(map[string]any)["SessionStart"].([]any)
	if !ok || len(sessionStart) != 2 {
		t.Fatalf("expected user and HerdR SessionStart groups:\n%s", first)
	}
	user := sessionStart[0].(map[string]any)
	userHook := user[hooksKey].([]any)[0].(map[string]any)
	if user["matcher"] != "startup" || userHook["command"] != "echo user-start" || userHook["timeout"] != int64(3) {
		t.Fatalf("user SessionStart group changed: %#v", user)
	}
	if !isHerdRHandler(sessionStart[1].(map[string]any)[hooksKey].([]any)[0]) {
		t.Fatalf("managed HerdR SessionStart group missing: %#v", sessionStart[1])
	}
}

// TestWriteCodexConfigHerdRTitleBoundary exercises the public config writer,
// rather than the title helper, for the ownership-bearing native settings.
func TestWriteCodexConfigHerdRTitleBoundary(t *testing.T) {
	enabled := true
	cases := []struct {
		name, existing string
		agentSpecific  map[string]any
		wantTitle      []any
		wantError      string
	}{
		{
			name:      "existing title retains user order after activity",
			existing:  codexPartialHeader + "\n[tui]\nterminal_title = [\"activity\", \"project-name\", \"thread-name\"]\n",
			wantTitle: []any{"activity", "thread-id", "project-name", "thread-name"},
		},
		{
			name:     "explicit agent title wins and shares tui status line",
			existing: codexPartialHeader + "\n[tui]\nterminal_title = [\"project-name\"]\n",
			agentSpecific: map[string]any{codexTUIKey: map[string]any{
				codexTerminalTitleKey: []any{"spinner", "project-name"},
				codexStatusLineKey:    []any{"model"},
			}},
			wantTitle: []any{"spinner", "thread-id", "project-name"},
		},
		{
			name:      "malformed existing title fails without overwrite",
			existing:  codexPartialHeader + "\n[tui]\nterminal_title = \"not-a-list\"\n",
			wantError: "invalid Codex tui.terminal_title",
		},
		{
			name:      "non-string existing title item fails without overwrite",
			existing:  codexPartialHeader + "\n[tui]\nterminal_title = [\"project-name\", 3]\n",
			wantError: "invalid Codex tui.terminal_title",
		},
		{
			name:      "native empty title is preserved and idempotent",
			existing:  codexPartialHeader + "\n[tui]\nterminal_title = []\n",
			wantTitle: []any{},
		},
		{
			name:     "explicit empty title wins against native title",
			existing: codexPartialHeader + "\n[tui]\nterminal_title = [\"activity\", \"project-name\"]\n",
			agentSpecific: map[string]any{codexTUIKey: map[string]any{
				codexTerminalTitleKey: []any{},
			}},
			wantTitle: []any{},
		},
		{
			name: "empty title item remains malformed",
			agentSpecific: map[string]any{codexTUIKey: map[string]any{
				codexTerminalTitleKey: []any{""},
			}},
			wantError: "must contain only non-empty title items",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeExistingCodexConfig(t, root, tc.existing)
			project := &config.ProjectConfig{Config: config.Config{Agents: config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled, AgentSpecific: tc.agentSpecific}}}, Env: map[string]string{}}
			err := writeCodexConfig(RealSystem{}, root, project)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("writeCodexConfig error = %v, want %q", err, tc.wantError)
				}
				if got := readCodexConfig(t, root); got != tc.existing {
					t.Fatalf("failed merge overwrote user config:\n%s", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			first := readCodexConfig(t, root)
			if err := writeCodexConfig(RealSystem{}, root, project); err != nil {
				t.Fatal(err)
			}
			if second := readCodexConfig(t, root); second != first {
				t.Fatalf("second sync was not byte-idempotent\nfirst:\n%s\nsecond:\n%s", first, second)
			}
			parsed := parseCodexConfig(t, first)
			tui := parsed[codexTUIKey].(map[string]any)
			if got := tui[codexTerminalTitleKey]; !reflect.DeepEqual(got, tc.wantTitle) {
				t.Fatalf("terminal_title = %#v, want %#v", got, tc.wantTitle)
			}
			if tc.agentSpecific != nil && strings.Count(first, "[tui]") != 1 {
				t.Fatalf("expected one tui table:\n%s", first)
			}
		})
	}

	root := t.TempDir()
	user := codexPartialHeader + `
[hooks]
UserPromptSubmit = [{ matcher = "prompt", hooks = [{ type = "command", command = "echo user-prompt" }] }]
`
	writeExistingCodexConfig(t, root, user)
	project := &config.ProjectConfig{Config: config.Config{Agents: config.AgentsConfig{Codex: config.CodexConfig{Enabled: &enabled}}}, Env: map[string]string{}}
	for range 2 {
		if err := writeCodexConfig(RealSystem{}, root, project); err != nil {
			t.Fatal(err)
		}
	}
	content := readCodexConfig(t, root)
	hooks := parseCodexConfig(t, content)[hooksKey].(map[string]any)[codexUserPromptKey].([]any)
	if len(hooks) != 2 || hooks[0].(map[string]any)["matcher"] != "prompt" || !isHerdRHandler(hooks[1].(map[string]any)[hooksKey].([]any)[0]) {
		t.Fatalf("UserPromptSubmit order/dedupe = %#v\n%s", hooks, content)
	}
}

func TestCodexHerdRIgnoresMarkersInsideMultilineStrings(t *testing.T) {
	notes := strings.Join([]string{
		`notes = """`,
		codexHerdRBeginMarker,
		"user text",
		codexHerdREndMarker,
		`"""`,
	}, "\n")
	managedEditor := newCodexTomlEditor("")
	if _, err := managedEditor.applyCodexHerdRHook("config.toml", true); err != nil {
		t.Fatal(err)
	}
	managed := managedEditor.render()
	for name, content := range map[string]string{
		"string only":        notes,
		"string and managed": notes + "\n\n" + managed,
	} {
		t.Run(name, func(t *testing.T) {
			for _, enabled := range []bool{true, false} {
				editor := newCodexTomlEditor(content)
				if _, err := editor.applyCodexHerdRHook("config.toml", enabled); err != nil {
					t.Fatalf("enabled=%t: %v", enabled, err)
				}
				output := editor.render()
				if !strings.HasPrefix(output, notes+"\n") {
					t.Fatalf("enabled=%t: multiline string content changed:\n%s", enabled, output)
				}
				var parsed map[string]any
				if err := toml.Unmarshal([]byte(output), &parsed); err != nil {
					t.Fatalf("enabled=%t: invalid TOML: %v\n%s", enabled, err, output)
				}
				_, hasHooks := parsed[hooksKey]
				if hasHooks != enabled {
					t.Fatalf("enabled=%t: managed hook presence = %t:\n%s", enabled, hasHooks, output)
				}
			}
		})
	}
}

func TestCodexHerdRPreservesAgentSpecificSessionStartAcrossResyncs(t *testing.T) {
	for _, chime := range []bool{false, true} {
		t.Run(fmt.Sprintf("chime %t", chime), func(t *testing.T) {
			root := t.TempDir()
			enabled := true
			userSessionStart := func(command string) map[string]any {
				return map[string]any{hooksKey: map[string]any{"SessionStart": []any{map[string]any{
					"matcher": "startup",
					hooksKey:  []any{map[string]any{"type": "command", "command": command, "timeout": int64(3)}},
				}}}}
			}
			project := &config.ProjectConfig{
				Config: config.Config{
					Agents: config.AgentsConfig{Codex: config.CodexConfig{
						Enabled:       &enabled,
						AgentSpecific: userSessionStart("echo user-start"),
					}},
					Notifications: config.NotificationsConfig{Chime: &chime},
				},
				Env: map[string]string{},
			}
			assertSessionStart := func(t *testing.T, content, userCommand string) {
				t.Helper()
				sessionStart, ok := parseCodexConfig(t, content)[hooksKey].(map[string]any)["SessionStart"].([]any)
				if !ok {
					t.Fatalf("expected SessionStart groups:\n%s", content)
				}
				herdRHandlers := 0
				var userGroups []map[string]any
				for _, raw := range sessionStart {
					group := raw.(map[string]any)
					managed := false
					for _, handler := range group[hooksKey].([]any) {
						if isHerdRHandler(handler) {
							herdRHandlers++
							managed = true
						}
					}
					if !managed {
						userGroups = append(userGroups, group)
					}
				}
				if herdRHandlers != 1 {
					t.Fatalf("expected exactly one HerdR handler, got %d:\n%s", herdRHandlers, content)
				}
				if len(userGroups) != 1 {
					t.Fatalf("expected exactly one user SessionStart group, got %d:\n%s", len(userGroups), content)
				}
				userHook := userGroups[0][hooksKey].([]any)[0].(map[string]any)
				if userGroups[0]["matcher"] != "startup" || userHook["command"] != userCommand || userHook["timeout"] != int64(3) {
					t.Fatalf("user SessionStart group changed: %#v\n%s", userGroups[0], content)
				}
				if got := strings.Count(content, codexHerdRBeginMarker); got != 1 {
					t.Fatalf("expected one HerdR begin marker, got %d:\n%s", got, content)
				}
				if got := strings.Count(content, codexHerdREndMarker); got != 1 {
					t.Fatalf("expected one HerdR end marker, got %d:\n%s", got, content)
				}
			}

			var previous string
			for i := range 3 {
				if err := writeCodexConfig(RealSystem{}, root, project); err != nil {
					t.Fatalf("sync %d: %v", i, err)
				}
				content := readCodexConfig(t, root)
				assertValidTOML(t, content)
				assertSessionStart(t, content, "echo user-start")
				if i > 0 && content != previous {
					t.Fatalf("sync %d changed converged content\nprevious:\n%s\ncurrent:\n%s", i, previous, content)
				}
				previous = content
			}

			project.Config.Agents.Codex.AgentSpecific = userSessionStart("echo user-edited")
			for i := range 2 {
				if err := writeCodexConfig(RealSystem{}, root, project); err != nil {
					t.Fatalf("edited sync %d: %v", i, err)
				}
				content := readCodexConfig(t, root)
				assertValidTOML(t, content)
				assertSessionStart(t, content, "echo user-edited")
				if strings.Contains(content, "echo user-start") {
					t.Fatalf("stale user command remained after edit:\n%s", content)
				}
				if i > 0 && content != previous {
					t.Fatalf("edited sync %d changed converged content\nprevious:\n%s\ncurrent:\n%s", i, previous, content)
				}
				previous = content
			}
		})
	}
}

func TestCodexEmptySessionStartReleaseRegression(t *testing.T) {
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Root: root,
		Env:  map[string]string{},
		Config: config.Config{
			Agents: config.AgentsConfig{Codex: config.CodexConfig{
				Enabled: &enabled,
				AgentSpecific: map[string]any{
					hooksKey: map[string]any{codexSessionStartKey: []any{}},
				},
			}},
		},
	}
	if err := writeCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("valid empty SessionStart prevents sync: %v", err)
	}
	assertCodexManagedHookEvents(t, readCodexConfig(t, root), true, false, nil)
}

// TestCodexEmptyManagedHookEventsAcrossSyncsAndTransitions covers explicitly
// empty agent_specific lists for every hook event Agent Layer appends as
// [[hooks.<event>]] array tables: fresh generation, repeated sync, and chime and
// Codex enabled/disabled transitions must stay valid and keep user entries.
func TestCodexEmptyManagedHookEventsAcrossSyncsAndTransitions(t *testing.T) {
	userPreToolUse := []any{map[string]any{
		"matcher": "Bash",
		hooksKey:  []any{map[string]any{"type": "command", "command": "echo user-pre", "timeout": int64(4)}},
	}}
	userStop := []any{map[string]any{
		"matcher": "done",
		hooksKey:  []any{map[string]any{"type": "command", "command": "echo user-stop", "timeout": int64(2)}},
	}}
	cases := map[string]struct {
		hooks map[string]any
		user  map[string][]any
	}{
		"empty SessionStart": {
			hooks: map[string]any{codexSessionStartKey: []any{}},
		},
		"empty UserPromptSubmit": {
			hooks: map[string]any{codexUserPromptKey: []any{}},
		},
		"empty Stop": {
			hooks: map[string]any{codexStopKey: []any{}},
		},
		"empty SessionStart and Stop with nonempty other event": {
			hooks: map[string]any{codexSessionStartKey: []any{}, codexUserPromptKey: []any{}, codexStopKey: []any{}, "PreToolUse": userPreToolUse},
			user:  map[string][]any{"PreToolUse": userPreToolUse},
		},
		"empty SessionStart with nonempty Stop": {
			hooks: map[string]any{codexSessionStartKey: []any{}, codexUserPromptKey: []any{}, codexStopKey: userStop},
			user:  map[string][]any{codexStopKey: userStop},
		},
	}
	for name, tc := range cases {
		for _, chime := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s chime %t", name, chime), func(t *testing.T) {
				root := t.TempDir()
				codexEnabled := true
				chimeEnabled := chime
				project := &config.ProjectConfig{
					Root: root,
					Env:  map[string]string{},
					Config: config.Config{
						Agents: config.AgentsConfig{Codex: config.CodexConfig{
							Enabled:       &codexEnabled,
							AgentSpecific: map[string]any{hooksKey: tc.hooks},
						}},
						Notifications: config.NotificationsConfig{Chime: &chimeEnabled},
					},
				}
				syncUntilStable := func(t *testing.T, step string, herdR bool) {
					t.Helper()
					var previous string
					for i := range 3 {
						var err error
						if herdR {
							err = writeCodexConfig(RealSystem{}, root, project)
						} else {
							// Codex disabled with VS Code enabled still projects shared settings.
							err = writeCodexConfigWithCLISettings(RealSystem{}, root, project, false)
						}
						if err != nil {
							t.Fatalf("%s sync %d: %v", step, i, err)
						}
						content := readCodexConfig(t, root)
						assertCodexManagedHookEvents(t, content, herdR, chimeEnabled, tc.user)
						if i > 0 && content != previous {
							t.Fatalf("%s sync %d changed converged content\nprevious:\n%s\ncurrent:\n%s", step, i, previous, content)
						}
						previous = content
					}
				}

				syncUntilStable(t, "fresh", true)

				chimeEnabled = !chime
				syncUntilStable(t, "chime toggled", true)
				chimeEnabled = chime
				syncUntilStable(t, "chime restored", true)

				codexEnabled = false
				syncUntilStable(t, "codex disabled with vscode", false)
				codexEnabled = true
				syncUntilStable(t, "codex re-enabled", true)

				codexEnabled = false
				if err := cleanCodexChimeHook(RealSystem{}, root); err != nil {
					t.Fatalf("clean disabled codex hooks: %v", err)
				}
				content := readCodexConfig(t, root)
				assertValidTOML(t, content)
				if strings.Contains(content, codexHerdRBeginMarker) || strings.Contains(content, codexChimeBeginMarker) {
					t.Fatalf("managed hooks remained after Codex was disabled:\n%s", content)
				}
				codexEnabled = true
				syncUntilStable(t, "codex re-enabled after cleanup", true)
			})
		}
	}
}

func TestCodexEmptyManagedHookEventsReplaceExistingUserEntries(t *testing.T) {
	existing := map[string]string{
		// v0.23.1 rendered explicitly empty lists as assignments with no managed hooks.
		"v0.23.1 empty assignments": codexPartialHeader + "[hooks]\nSessionStart = []\nUserPromptSubmit = []\nStop = []\n",
		"previous user array tables": codexPartialHeader + `[[hooks.SessionStart]]
matcher = "startup"
[[hooks.SessionStart.hooks]]
type = "command"
command = "echo old-start"

[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = "echo old-stop"
`,
		"previous user inline lists": codexPartialHeader + `[hooks]
SessionStart = [{ hooks = [{ type = "command", command = "echo old-start" }] }]
Stop = [{ hooks = [{ type = "command", command = "echo old-stop" }] }]
`,
	}
	for name, content := range existing {
		for _, chime := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s chime %t", name, chime), func(t *testing.T) {
				root := t.TempDir()
				enabled := true
				project := &config.ProjectConfig{
					Root: root,
					Env:  map[string]string{},
					Config: config.Config{
						Agents: config.AgentsConfig{Codex: config.CodexConfig{
							Enabled: &enabled,
							AgentSpecific: map[string]any{hooksKey: map[string]any{
								codexSessionStartKey: []any{},
								codexUserPromptKey:   []any{},
								codexStopKey:         []any{},
							}},
						}},
						Notifications: config.NotificationsConfig{Chime: &chime},
					},
				}
				writeExistingCodexConfig(t, root, content)
				var previous string
				for i := range 2 {
					if err := writeCodexConfig(RealSystem{}, root, project); err != nil {
						t.Fatalf("sync %d: %v", i, err)
					}
					output := readCodexConfig(t, root)
					assertCodexManagedHookEvents(t, output, true, chime, nil)
					if strings.Contains(output, "echo old-") {
						t.Fatalf("stale user hook remained after agent_specific emptied it:\n%s", output)
					}
					if i > 0 && output != previous {
						t.Fatalf("sync %d changed converged content\nprevious:\n%s\ncurrent:\n%s", i, previous, output)
					}
					previous = output
				}
			})
		}
	}
}

// assertCodexManagedHookEvents checks that content is valid TOML with exactly
// the expected managed SessionStart, UserPromptSubmit, and Stop handlers, and
// that every user hook event equals want (events absent from want must hold no
// user entries).
func assertCodexManagedHookEvents(t *testing.T, content string, herdR bool, chime bool, want map[string][]any) {
	t.Helper()
	hooks, _ := parseCodexConfig(t, content)[hooksKey].(map[string]any)
	managedCounts := map[string]int{}
	for event, raw := range hooks {
		if event == "state" {
			continue
		}
		entries, ok := raw.([]any)
		if !ok {
			t.Fatalf("hooks.%s is not an array:\n%s", event, content)
		}
		var user []any
		for _, entry := range entries {
			group, _ := entry.(map[string]any)
			handlers, _ := group[hooksKey].([]any)
			if len(handlers) == 1 && (event == codexSessionStartKey || event == codexUserPromptKey) && isHerdRHandler(handlers[0]) {
				managedCounts[event]++
				continue
			}
			if len(handlers) == 1 && event == codexStopKey && chimeHandlerMatchesAny(handlers[0], managedChimeCommandVariants(agentLayerCodexChimeCommand)) {
				managedCounts[event]++
				continue
			}
			user = append(user, entry)
		}
		if len(user) == 0 && len(want[event]) == 0 {
			continue
		}
		if !reflect.DeepEqual(user, want[event]) {
			t.Fatalf("hooks.%s user entries = %#v, want %#v:\n%s", event, user, want[event], content)
		}
	}
	for event := range want {
		if _, ok := hooks[event]; !ok {
			t.Fatalf("hooks.%s user entries missing:\n%s", event, content)
		}
	}
	wantManaged := map[string]bool{codexSessionStartKey: herdR, codexUserPromptKey: herdR, codexStopKey: chime}
	markers := map[string]string{codexSessionStartKey: codexHerdRBeginMarker, codexUserPromptKey: codexHerdRBeginMarker, codexStopKey: codexChimeBeginMarker}
	for event, enabled := range wantManaged {
		want := 0
		if enabled {
			want = 1
		}
		if managedCounts[event] != want || strings.Count(content, markers[event]) != want {
			t.Fatalf("expected %d managed hooks.%s handlers and markers, got %d handlers and %d markers:\n%s",
				want, event, managedCounts[event], strings.Count(content, markers[event]), content)
		}
	}
}
