package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/tomlpatch"
)

func TestWriteCodexConfig_MergesSharedStateAndIsIdempotent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	writeExistingCodexConfig(t, root, "# user note\n"+codexHeader+`
model = "old-model"
features = { apps = false, plugins = false, browser_use = false, custom = true }
tui.status_line = ["old-status"]

[projects.`+tomlpatch.FormatKey(absRoot)+`]
trust_level = "untrusted"

[hooks.state]
last_seen = "keep"

[notices]
read = true

[mcp_servers."old"]
command = "old-tool"
`)

	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					Enabled:         &enabled,
					Model:           "gpt-5.3-codex",
					ReasoningEffort: "high",
				},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{{
					ID:        "new",
					Enabled:   &enabled,
					Clients:   []string{"codex"},
					Transport: config.TransportStdio,
					Command:   "new-tool",
				}},
			},
		},
		Env: map[string]string{},
	}

	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	first := readCodexConfig(t, root)
	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("second WriteCodexConfig: %v", err)
	}
	second := readCodexConfig(t, root)
	if first != second {
		t.Fatalf("expected idempotent second sync\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	if !strings.Contains(first, "# user note\n"+codexPartialHeader) {
		t.Fatalf("expected user comment and partial header, got:\n%s", first)
	}
	for _, unexpected := range []string{"old-model", "old-status", `[mcp_servers."old"]`, "apps = false", "plugins = false", "browser_use = false"} {
		if strings.Contains(first, unexpected) {
			t.Fatalf("expected %q to be removed/updated, got:\n%s", unexpected, first)
		}
	}
	for _, expected := range []string{
		`model = "gpt-5.3-codex"`,
		`model_reasoning_effort = "high"`,
		`approval_policy = "never"`,
		`sandbox_mode = "danger-full-access"`,
		`web_search = "live"`,
		`custom = true`,
		`trust_level = "untrusted"`,
		`[hooks.state]`,
		`last_seen = "keep"`,
		`[notices]`,
		`[mcp_servers."new"]`,
		`command = "new-tool"`,
	} {
		if !strings.Contains(first, expected) {
			t.Fatalf("expected %q in merged config:\n%s", expected, first)
		}
	}
	assertValidTOML(t, first)
}

func TestWriteCodexConfig_IdempotentWhenSeedingTrustFromPreAgentLayerConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Existing config predates Agent Layer: it has NO [projects."<root>"] entry, so
	// the first sync seeds trust. The seeded projects block must land before the
	// re-appended mcp_servers block so a second sync produces byte-identical output.
	writeExistingCodexConfig(t, root, `# hand-written codex config
model = "old-model"

[mcp_servers."old"]
command = "old-tool"
`)
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{Enabled: &enabled, Model: "gpt-5.3-codex"},
			},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{{
					ID:        "new",
					Enabled:   &enabled,
					Clients:   []string{"codex"},
					Transport: config.TransportStdio,
					Command:   "new-tool",
				}},
			},
		},
		Env: map[string]string{},
	}

	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("first WriteCodexConfig: %v", err)
	}
	first := readCodexConfig(t, root)
	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("second WriteCodexConfig: %v", err)
	}
	second := readCodexConfig(t, root)
	if first != second {
		t.Fatalf("expected idempotent second sync when seeding trust\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	projectsIdx := strings.Index(first, "[projects.")
	mcpIdx := strings.Index(first, "[mcp_servers.")
	if projectsIdx < 0 || mcpIdx < 0 {
		t.Fatalf("expected both projects and mcp_servers blocks, got:\n%s", first)
	}
	if projectsIdx > mcpIdx {
		t.Fatalf("expected seeded projects block before mcp_servers block, got:\n%s", first)
	}
	assertValidTOML(t, first)
}

func TestWriteCodexConfig_InlineTableProjectsSeedConflictFailsWithActionableError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// projects defined as a root inline table lacking the trusted root: seeding a
	// [projects."<root>"] header would break, so fail with the actionable message
	// naming the offending path rather than the opaque go-toml render error.
	existing := codexPartialHeader + "projects = { \"/other/repo\" = { trust_level = \"on-request\" } }\n"
	writeExistingCodexConfig(t, root, existing)

	err := WriteCodexConfig(RealSystem{}, root, &config.ProjectConfig{Env: map[string]string{}})
	if err == nil || !strings.Contains(err.Error(), "incompatible shape at projects") {
		t.Fatalf("expected projects shape conflict, got %v", err)
	}
	if got := readCodexConfig(t, root); got != existing {
		t.Fatalf("expected conflicting file left untouched, got:\n%s", got)
	}
}

func TestWriteCodexConfig_InlineTableProjectsWithTrustedRootSucceeds(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	// The trusted root is already present in the inline table, so no header is
	// appended and the config is left valid (no false shape conflict).
	existing := codexPartialHeader + "projects = { " + tomlpatch.FormatKey(absRoot) + " = { trust_level = \"on-request\" } }\n"
	writeExistingCodexConfig(t, root, existing)

	if err := WriteCodexConfig(RealSystem{}, root, &config.ProjectConfig{Env: map[string]string{}}); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	merged := readCodexConfig(t, root)
	parsed := parseCodexConfig(t, merged)
	projects := parsed["projects"].(map[string]any)
	entry := projects[absRoot].(map[string]any)
	if got := entry["trust_level"]; got != "on-request" {
		t.Fatalf("expected existing inline trust preserved, got %#v", got)
	}
	assertValidTOML(t, merged)
}

func TestWriteCodexConfig_ManagedScalarAsTableHeaderFailsWithActionableError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	existing := codexPartialHeader + "[model]\nname = \"custom\"\n"
	writeExistingCodexConfig(t, root, existing)
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{Enabled: &enabled, Model: "gpt-5.3-codex"},
			},
		},
		Env: map[string]string{},
	}
	// Managed sync writes a root scalar `model = ...`, which collides with the
	// existing [model] table header; fail loud naming `model`.
	err := WriteCodexConfig(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "incompatible shape at model") {
		t.Fatalf("expected model shape conflict, got %v", err)
	}
	if got := readCodexConfig(t, root); got != existing {
		t.Fatalf("expected conflicting file left untouched, got:\n%s", got)
	}
}

func TestWriteCodexConfig_ManagedScalarTableHeaderPreservedWhenNotManaged(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	existing := codexPartialHeader + "[model]\nname = \"custom\"\n"
	writeExistingCodexConfig(t, root, existing)
	// No model set and approvals None: no managed root scalar is written, so the
	// existing [model] table is not a conflict and is preserved untouched.
	project := &config.ProjectConfig{
		Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone}},
		Env:    map[string]string{},
	}
	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	merged := readCodexConfig(t, root)
	parsed := parseCodexConfig(t, merged)
	modelTable, ok := parsed["model"].(map[string]any)
	if !ok || modelTable["name"] != "custom" {
		t.Fatalf("expected [model] table preserved, got %#v", parsed["model"])
	}
	assertValidTOML(t, merged)
}

func TestCodexTomlEditor_SetPathPreservesInlineComment(t *testing.T) {
	t.Parallel()
	editor := newCodexTomlEditor(`model = "old" # keep me
[tui]
status_line = ["old"] # and me
`)

	editor.setPath([]string{config.CodexModelKey}, `"gpt-5"`)
	editor.setPath([]string{"tui", "status_line"}, `["new"]`)

	out := editor.render()
	if !strings.Contains(out, `model = "gpt-5" # keep me`) {
		t.Fatalf("expected root scalar inline comment preserved, got:\n%s", out)
	}
	if !strings.Contains(out, `status_line = ["new"] # and me`) {
		t.Fatalf("expected nested inline comment preserved, got:\n%s", out)
	}
	assertValidTOML(t, out)
}

func TestCodexTomlEditor_SetPathReplacesInPlaceDroppingDuplicates(t *testing.T) {
	t.Parallel()
	// Duplicate root keys are not valid TOML input to a real sync, but the editor
	// primitive must still collapse them to a single updated line in place.
	editor := newCodexTomlEditor("model = \"a\"\nmodel = \"b\"\n[hooks]\nx = 1\n")

	editor.setPath([]string{config.CodexModelKey}, `"gpt-5"`)

	out := editor.render()
	if strings.Count(out, "model = ") != 1 {
		t.Fatalf("expected a single model line after in-place replace, got:\n%s", out)
	}
	if !strings.Contains(out, `model = "gpt-5"`) {
		t.Fatalf("expected model updated in place, got:\n%s", out)
	}
	if !strings.Contains(out, "[hooks]") {
		t.Fatalf("expected unrelated table preserved, got:\n%s", out)
	}
	assertValidTOML(t, out)
}

// A multiline string may contain a line that looks like a table header. The
// namespace scan (run on every sync to refresh mcp_servers) must treat that as
// string content, not a real header, so unrelated user config is preserved.
func TestCodexTomlEditor_RemoveNamespaceIgnoresMultilineStringHeaders(t *testing.T) {
	t.Parallel()
	editor := newCodexTomlEditor(`notes = """
Example server config you can copy:
[mcp_servers.example]
command = "demo"
"""

[mcp_servers.real]
command = "real-tool"

[hooks]
x = 1
`)

	editor.removeNamespace([]string{config.CodexMCPServersKey})
	out := editor.render()

	if strings.Contains(out, "real-tool") {
		t.Fatalf("expected real mcp_servers table removed, got:\n%s", out)
	}
	parsed := parseCodexConfig(t, out)
	notes, _ := parsed["notes"].(string)
	if !strings.Contains(notes, "[mcp_servers.example]") || !strings.Contains(notes, `command = "demo"`) {
		t.Fatalf("expected multiline string body preserved, got notes=%q\n%s", notes, out)
	}
	if _, ok := parsed["hooks"].(map[string]any); !ok {
		t.Fatalf("expected unrelated [hooks] table preserved, got %#v\n%s", parsed["hooks"], out)
	}
}

// firstTableIndex picks where root scalars are inserted; it must skip header-
// looking lines inside a leading multiline string so the value lands at root.
func TestCodexTomlEditor_RootInsertSkipsMultilineStringHeaders(t *testing.T) {
	t.Parallel()
	editor := newCodexTomlEditor(`notes = """
[mcp_servers.example]
command = "demo"
"""

[hooks]
x = 1
`)

	editor.setPath([]string{config.CodexModelKey}, `"gpt-5"`)
	out := editor.render()

	parsed := parseCodexConfig(t, out)
	if parsed["model"] != "gpt-5" {
		t.Fatalf("expected model set as a real root key, got %#v\n%s", parsed["model"], out)
	}
	notes, _ := parsed["notes"].(string)
	if !strings.Contains(notes, "[mcp_servers.example]") || !strings.Contains(notes, `command = "demo"`) {
		t.Fatalf("expected multiline string body preserved intact, got notes=%q\n%s", notes, out)
	}
}

// ensureTable locates or creates a managed table; it must not match a header-
// looking line inside a multiline string when adding a nested key.
func TestCodexTomlEditor_EnsureTableSkipsMultilineStringHeaders(t *testing.T) {
	t.Parallel()
	editor := newCodexTomlEditor(`notes = """
[tui]
status_line = ["from-string"]
"""
`)

	editor.setPath([]string{"tui", "status_line"}, `["real"]`)
	out := editor.render()

	parsed := parseCodexConfig(t, out)
	tui, ok := parsed["tui"].(map[string]any)
	if !ok {
		t.Fatalf("expected a real [tui] table, got %#v\n%s", parsed["tui"], out)
	}
	assertStringList(t, tui["status_line"], []string{"real"})
	notes, _ := parsed["notes"].(string)
	if !strings.Contains(notes, "[tui]") || !strings.Contains(notes, "from-string") {
		t.Fatalf("expected multiline string body preserved, got notes=%q\n%s", notes, out)
	}
}

// Rewriting a root inline table must keep its leading indentation and any
// single-line trailing comment, matching the PR's preserve-user-formatting goal.
func TestCodexTomlEditor_MutateRootInlineTablePreservesIndentAndComment(t *testing.T) {
	t.Parallel()
	editor := newCodexTomlEditor("  features = { apps = true, custom = true } # keep me\n")

	editor.setPath([]string{"features", "apps"}, "false")
	out := editor.render()

	firstLine := strings.SplitN(out, "\n", 2)[0]
	if !strings.HasPrefix(firstLine, "  features = {") {
		t.Fatalf("expected leading indentation preserved, got:\n%s", out)
	}
	if !strings.HasSuffix(strings.TrimRight(firstLine, " "), "# keep me") {
		t.Fatalf("expected inline comment preserved, got:\n%s", out)
	}
	parsed := parseCodexConfig(t, out)
	features, ok := parsed["features"].(map[string]any)
	if !ok {
		t.Fatalf("expected features table, got %#v\n%s", parsed["features"], out)
	}
	if features["apps"] != false || features["custom"] != true {
		t.Fatalf("expected apps=false and custom=true, got %#v\n%s", features, out)
	}
}

func TestWriteCodexConfig_StatuslineUsesInjectedFragmentAndPreservesTUISiblings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExistingCodexConfig(t, root, codexPartialHeader+`
[tui]
status_line = ["old"]
notifications = true
`)
	writeCodexStatuslineSource(t, root, "[tui]\nstatus_line = [\"new\"]\n")
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
			Agents:    config.AgentsConfig{Codex: config.CodexConfig{Statusline: &enabled}},
		},
		Env: map[string]string{},
	}

	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}

	parsed := parseCodexConfig(t, readCodexConfig(t, root))
	tui, ok := parsed["tui"].(map[string]any)
	if !ok {
		t.Fatalf("expected tui table, got %#v", parsed["tui"])
	}
	assertStringList(t, tui["status_line"], []string{"new"})
	if got := tui["notifications"]; got != true {
		t.Fatalf("expected tui.notifications preserved, got %#v", got)
	}
}

func TestWriteCodexConfig_ChimeAppendsManagedBlockAndPreservesUserStopHooks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExistingCodexConfig(t, root, codexPartialHeader+`
[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = "echo user"
timeout = 2
`)
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
		Env: map[string]string{},
	}

	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	first := readCodexConfig(t, root)
	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("second WriteCodexConfig: %v", err)
	}
	second := readCodexConfig(t, root)
	if first != second {
		t.Fatalf("expected idempotent chime merge\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	for _, want := range []string{codexChimeBeginMarker, agentLayerChimeMarker, `command = "echo user"`} {
		if !strings.Contains(first, want) {
			t.Fatalf("expected %q in Codex config:\n%s", want, first)
		}
	}
	if got := strings.Count(first, codexChimeBeginMarker); got != 1 {
		t.Fatalf("expected one managed chime block, got %d:\n%s", got, first)
	}
	assertValidTOML(t, first)
}

func TestWriteCodexConfig_ChimeConvertsInlineStopAssignmentAndPreservesUserHooks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExistingCodexConfig(t, root, codexPartialHeader+`
[hooks]
state = { last_seen = "keep" }
Stop = [
  { hooks = [{ type = "command", command = "echo user", timeout = 2 }] },
  { name = "no hooks yet", hooks = [] },
]

[notices]
read = true
`)
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
		Env: map[string]string{},
	}

	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	first := readCodexConfig(t, root)
	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("second WriteCodexConfig: %v", err)
	}
	second := readCodexConfig(t, root)
	if first != second {
		t.Fatalf("expected idempotent chime merge\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if strings.Contains(first, "Stop = [") {
		t.Fatalf("expected inline Stop assignment converted before appending chime, got:\n%s", first)
	}
	for _, want := range []string{
		`[hooks]`,
		`state = { last_seen = "keep" }`,
		`[notices]`,
		`command = "echo user"`,
		codexChimeBeginMarker,
		agentLayerChimeMarker,
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("expected %q in Codex config:\n%s", want, first)
		}
	}
	parsed := parseCodexConfig(t, first)
	hooks, ok := parsed["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("expected hooks table, got %#v\n%s", parsed["hooks"], first)
	}
	stop, ok := hooks["Stop"].([]any)
	if !ok || len(stop) != 3 {
		t.Fatalf("expected user Stop hook and managed chime hook, got %#v\n%s", hooks["Stop"], first)
	}
	userStop, ok := stop[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first Stop entry to be a table, got %#v\n%s", stop[0], first)
	}
	userStopHooks, ok := userStop["hooks"].([]any)
	if !ok || len(userStopHooks) != 1 {
		t.Fatalf("expected first user Stop hooks preserved, got %#v\n%s", userStop["hooks"], first)
	}
	userHook, ok := userStopHooks[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first user hook to be a table, got %#v\n%s", userStopHooks[0], first)
	}
	if userHook["type"] != "command" || userHook["command"] != "echo user" || userHook["timeout"] != int64(2) {
		t.Fatalf("expected user hook fields preserved, got %#v\n%s", userHook, first)
	}
	emptyStop, ok := stop[1].(map[string]any)
	if !ok {
		t.Fatalf("expected second Stop entry to be a table, got %#v\n%s", stop[1], first)
	}
	emptyHooks, ok := emptyStop["hooks"].([]any)
	if !ok || len(emptyHooks) != 0 || emptyStop["name"] != "no hooks yet" {
		t.Fatalf("expected empty user hook list preserved, got %#v\n%s", emptyStop, first)
	}
}

func TestWriteCodexConfig_ChimePreservesAgentSpecificStopHooks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{AgentSpecific: map[string]any{
					"hooks": map[string]any{
						"Stop": []any{
							map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo user", "timeout": int64(2)}}},
						},
					},
				}},
			},
		},
		Env: map[string]string{},
	}

	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	merged := readCodexConfig(t, root)
	for _, want := range []string{codexChimeBeginMarker, agentLayerChimeMarker, `command = 'echo user'`} {
		if !strings.Contains(merged, want) {
			t.Fatalf("expected %q in Codex config:\n%s", want, merged)
		}
	}
	assertValidTOML(t, merged)
}

func TestWriteCodexConfig_ChimeDisabledRemovesOnlyManagedBlock(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExistingCodexConfig(t, root, codexPartialHeader+`
[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = "echo user"
timeout = 2

`+codexChimeBlockForTest())
	project := &config.ProjectConfig{
		Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone}},
		Env:    map[string]string{},
	}

	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	merged := readCodexConfig(t, root)
	if strings.Contains(merged, codexChimeBeginMarker) || strings.Contains(merged, agentLayerChimeMarker) {
		t.Fatalf("expected managed chime block removed, got:\n%s", merged)
	}
	if !strings.Contains(merged, `command = "echo user"`) {
		t.Fatalf("expected user Stop hook preserved, got:\n%s", merged)
	}
	assertValidTOML(t, merged)
}

func TestWriteCodexConfig_ChimeRejectsManagedBlockWithTrailingAssignment(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	existing := codexPartialHeader + codexChimeBlockForTest() + `user_key = "would-move"

[notices]
read = true
`
	writeExistingCodexConfig(t, root, existing)
	project := &config.ProjectConfig{
		Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone}},
		Env:    map[string]string{},
	}

	err := WriteCodexConfig(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "markers are incomplete or ambiguous") {
		t.Fatalf("expected ambiguous chime marker error, got %v", err)
	}
	if got := readCodexConfig(t, root); got != existing {
		t.Fatalf("expected ambiguous file left untouched, got:\n%s", got)
	}
}

func TestCleanCodexChimeHookRemovesManagedBlockWhenCodexDisabled(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExistingCodexConfig(t, root, codexPartialHeader+`
[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = "echo user"
timeout = 2

`+codexChimeBlockForTest())

	if err := CleanCodexChimeHook(RealSystem{}, root); err != nil {
		t.Fatalf("CleanCodexChimeHook: %v", err)
	}
	merged := readCodexConfig(t, root)
	if strings.Contains(merged, codexChimeBeginMarker) || strings.Contains(merged, agentLayerChimeMarker) {
		t.Fatalf("expected managed chime block removed, got:\n%s", merged)
	}
	if !strings.Contains(merged, `command = "echo user"`) {
		t.Fatalf("expected user Stop hook preserved, got:\n%s", merged)
	}
	assertValidTOML(t, merged)
}

func TestCleanCodexChimeHookRejectsSymlinkConfigDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	outsideConfig := filepath.Join(outside, "config.toml")
	if err := os.WriteFile(outsideConfig, []byte(codexPartialHeader+codexChimeBlockForTest()), 0o600); err != nil {
		t.Fatalf("write outside config: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".codex")); err != nil {
		t.Fatalf("seed .codex symlink: %v", err)
	}

	err := CleanCodexChimeHook(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "must be a real file") {
		t.Fatalf("expected symlink cleanup error, got %v", err)
	}
	if got := readFileForTest(t, outsideConfig); !strings.Contains(got, agentLayerChimeMarker) {
		t.Fatalf("outside config must not be rewritten, got:\n%s", got)
	}
}

func TestCleanCodexChimeHookRejectsSymlinkConfigFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	codexDir := filepath.Join(root, ".codex")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	outsideConfig := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(outsideConfig, []byte(codexPartialHeader+codexChimeBlockForTest()), 0o600); err != nil {
		t.Fatalf("write outside config: %v", err)
	}
	if err := os.Symlink(outsideConfig, filepath.Join(codexDir, "config.toml")); err != nil {
		t.Fatalf("seed config symlink: %v", err)
	}

	err := CleanCodexChimeHook(RealSystem{}, root)
	if err == nil || !strings.Contains(err.Error(), "must be a real file") {
		t.Fatalf("expected symlink cleanup error, got %v", err)
	}
	if got := readFileForTest(t, outsideConfig); !strings.Contains(got, agentLayerChimeMarker) {
		t.Fatalf("outside config must not be rewritten, got:\n%s", got)
	}
}

func TestCleanCodexChimeHookIgnoresMalformedConfigWithoutChime(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	configPath := filepath.Join(root, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	content := `model = [`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := CleanCodexChimeHook(RealSystem{}, root); err != nil {
		t.Fatalf("CleanCodexChimeHook should ignore malformed no-chime config: %v", err)
	}
	if got := readFileForTest(t, configPath); got != content {
		t.Fatalf("expected malformed no-chime config untouched, got:\n%s", got)
	}
}

func TestWriteCodexConfig_ChimeMigratesExactUnmarkedStopGroup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExistingCodexConfig(t, root, codexPartialHeader+`
[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = "/usr/bin/afplay /System/Library/Sounds/Blow.aiff >/dev/null 2>&1 & printf '{\"continue\":true}\\n'"
timeout = 5
`)
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
		Env: map[string]string{},
	}

	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	merged := readCodexConfig(t, root)
	if got := strings.Count(merged, "[[hooks.Stop]]"); got != 1 {
		t.Fatalf("expected exact unmarked chime group replaced by one managed Stop group, got %d:\n%s", got, merged)
	}
	if !strings.Contains(merged, codexChimeBeginMarker) || !strings.Contains(merged, agentLayerChimeMarker) {
		t.Fatalf("expected managed marker after migration, got:\n%s", merged)
	}
	assertValidTOML(t, merged)
}

func TestWriteCodexConfig_ChimeIgnoresMarkersInsideMultilineStrings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExistingCodexConfig(t, root, codexPartialHeader+`notes = """
`+codexChimeBeginMarker+`
[[hooks.Stop]]
`+codexChimeEndMarker+`
"""
`)
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
		Env: map[string]string{},
	}

	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	merged := readCodexConfig(t, root)
	parsed := parseCodexConfig(t, merged)
	notes, _ := parsed["notes"].(string)
	if !strings.Contains(notes, codexChimeBeginMarker) || !strings.Contains(notes, codexChimeEndMarker) {
		t.Fatalf("expected multiline marker text preserved in notes, got %q\n%s", notes, merged)
	}
	if got := strings.Count(merged, codexChimeBeginMarker); got != 2 {
		t.Fatalf("expected one marker in notes and one managed marker, got %d:\n%s", got, merged)
	}
	assertValidTOML(t, merged)
}

func TestWriteCodexConfig_ChimeRootInlineHooksConflictFails(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	existing := codexPartialHeader + `hooks = { state = { last_seen = "keep" } }
`
	writeExistingCodexConfig(t, root, existing)
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
		Env: map[string]string{},
	}

	err := WriteCodexConfig(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "incompatible shape at hooks") {
		t.Fatalf("expected root inline hooks conflict, got %v", err)
	}
	if got := readCodexConfig(t, root); got != existing {
		t.Fatalf("expected conflicting file left untouched, got:\n%s", got)
	}
}

func TestWriteCodexConfig_ChimeRejectsLegacyAgentSpecificHook(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{AgentSpecific: map[string]any{
					"hooks": map[string]any{
						"Stop": []any{map[string]any{"hooks": []any{chimeHandler(agentLayerCodexChimeCommand)}}},
					},
				}},
			},
		},
		Env: map[string]string{},
	}
	err := WriteCodexConfig(RealSystem{}, t.TempDir(), project)
	if err == nil || !strings.Contains(err.Error(), "agents.codex.agent_specific.hooks") {
		t.Fatalf("expected legacy agent_specific chime error, got %v", err)
	}
}

func TestWriteCodexConfig_CleansKnownFeatureAndStatuslineForms(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExistingCodexConfig(t, root, codexPartialHeader+`
features.apps = false
features.plugins = false
features.browser_use = false
features.in_app_browser = false
features.computer_use = false
features.custom = true
tui = { status_line = ["old"], notifications = true }
`)

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
		},
		Env: map[string]string{},
	}
	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}
	merged := readCodexConfig(t, root)
	for _, unexpected := range []string{"apps = false", "plugins = false", "browser_use = false", "in_app_browser = false", "computer_use = false", "status_line"} {
		if strings.Contains(merged, unexpected) {
			t.Fatalf("expected stale managed path %q removed, got:\n%s", unexpected, merged)
		}
	}
	for _, expected := range []string{"custom = true", "notifications = true"} {
		if !strings.Contains(merged, expected) {
			t.Fatalf("expected %q preserved, got:\n%s", expected, merged)
		}
	}
	assertValidTOML(t, merged)
}

func TestWriteCodexConfig_FailsOnInvalidExistingTOMLWithoutOverwrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const invalid = "[tui\nstatus_line = []\n"
	writeExistingCodexConfig(t, root, invalid)

	err := WriteCodexConfig(RealSystem{}, root, &config.ProjectConfig{Env: map[string]string{}})
	if err == nil || !strings.Contains(err.Error(), "invalid existing Codex config TOML") {
		t.Fatalf("expected invalid existing TOML error, got %v", err)
	}
	if got := readCodexConfig(t, root); got != invalid {
		t.Fatalf("expected invalid file left untouched, got:\n%s", got)
	}
}

func TestWriteCodexConfig_ReadErrorDoesNotWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	readErr := errors.New("read denied")
	wrote := false
	sys := &MockSystem{
		Fallback: RealSystem{},
		ReadFileFunc: func(name string) ([]byte, error) {
			if name == filepath.Join(root, ".codex", "config.toml") {
				return nil, readErr
			}
			return RealSystem{}.ReadFile(name)
		},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			wrote = true
			return RealSystem{}.WriteFileAtomic(filename, data, perm)
		},
	}

	err := WriteCodexConfig(sys, root, &config.ProjectConfig{Env: map[string]string{}})
	if err == nil || !strings.Contains(err.Error(), "read denied") {
		t.Fatalf("expected read error, got %v", err)
	}
	if wrote {
		t.Fatal("expected read error to abort before write")
	}
}

func TestWriteCodexConfig_MalformedAgentSpecificProjectsFails(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		agentSpecific map[string]any
	}{
		{name: "projects scalar", agentSpecific: map[string]any{"projects": "bad"}},
		{name: "project entry scalar", agentSpecific: map[string]any{"projects": map[string]any{"/tmp/repo": "bad"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			project := &config.ProjectConfig{
				Config: config.Config{
					Agents: config.AgentsConfig{
						Codex: config.CodexConfig{AgentSpecific: tt.agentSpecific},
					},
				},
				Env: map[string]string{},
			}
			err := WriteCodexConfig(RealSystem{}, t.TempDir(), project)
			if err == nil || !strings.Contains(err.Error(), "agents.codex.agent_specific.projects") {
				t.Fatalf("expected malformed projects error, got %v", err)
			}
		})
	}
}

func TestWriteCodexConfig_PreservesExistingQuotedProjectTrust(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), `repo"quote]#\slash`)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	writeExistingCodexConfig(t, root, codexPartialHeader+`
[projects.`+tomlpatch.FormatKey(absRoot)+`]
trust_level = "on-request"
`)

	if err := WriteCodexConfig(RealSystem{}, root, &config.ProjectConfig{Env: map[string]string{}}); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}

	merged := readCodexConfig(t, root)
	if strings.Count(merged, "[projects.") != 1 {
		t.Fatalf("expected one project table, got:\n%s", merged)
	}
	parsed := parseCodexConfig(t, merged)
	projects := parsed["projects"].(map[string]any)
	projectEntry := projects[absRoot].(map[string]any)
	if got := projectEntry["trust_level"]; got != "on-request" {
		t.Fatalf("expected existing trust preserved, got %#v", got)
	}
}

func TestWriteCodexConfig_ExistingShapeConflictFailsWithoutOverwrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const conflict = "tui = \"not-a-table\"\n"
	writeExistingCodexConfig(t, root, conflict)

	err := WriteCodexConfig(RealSystem{}, root, &config.ProjectConfig{Env: map[string]string{}})
	if err == nil || !strings.Contains(err.Error(), "incompatible shape at tui") {
		t.Fatalf("expected shape conflict, got %v", err)
	}
	if got := readCodexConfig(t, root); got != conflict {
		t.Fatalf("expected conflicting file left untouched, got:\n%s", got)
	}
}

func TestWriteCodexConfig_UpdatesInlineTablesAndUnknownPassthroughLeaves(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExistingCodexConfig(t, root, `# user-owned comment
features = { custom = false }
tui = { notifications = false }

[unrelated]
keep = true
`)
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					AgentSpecific: map[string]any{
						"features": map[string]any{
							"apps":    true,
							"plugins": true,
							"custom":  true,
						},
						"tui": map[string]any{
							"notifications": true,
						},
						"experimental": map[string]any{
							"nested": int64(7),
						},
					},
				},
			},
		},
		Env: map[string]string{},
	}

	if err := WriteCodexConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteCodexConfig: %v", err)
	}

	merged := readCodexConfig(t, root)
	if !strings.HasPrefix(merged, codexPartialHeader+"# user-owned comment") {
		t.Fatalf("expected inserted header with user comment preserved after it, got:\n%s", merged)
	}
	parsed := parseCodexConfig(t, merged)
	features := parsed["features"].(map[string]any)
	if got := features["apps"]; got != true {
		t.Fatalf("expected managed feature apps updated in inline table, got %#v", got)
	}
	if got := features["plugins"]; got != true {
		t.Fatalf("expected managed feature plugins updated in inline table, got %#v", got)
	}
	if got := features["custom"]; got != true {
		t.Fatalf("expected unknown feature passthrough updated in inline table, got %#v", got)
	}
	tui := parsed["tui"].(map[string]any)
	if got := tui["notifications"]; got != true {
		t.Fatalf("expected unknown tui passthrough updated in inline table, got %#v", got)
	}
	experimental := parsed["experimental"].(map[string]any)
	if got := experimental["nested"]; got != int64(7) {
		t.Fatalf("expected experimental passthrough table, got %#v", got)
	}
	unrelated := parsed["unrelated"].(map[string]any)
	if got := unrelated["keep"]; got != true {
		t.Fatalf("expected unrelated table preserved, got %#v", got)
	}
}

func TestWriteCodexConfig_AgentSpecificManagedScalarTableFails(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const existing = codexPartialHeader + "\n[unrelated]\nkeep = true\n"
	writeExistingCodexConfig(t, root, existing)
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					AgentSpecific: map[string]any{
						config.CodexModelKey: map[string]any{"name": "bad"},
					},
				},
			},
		},
		Env: map[string]string{},
	}

	err := WriteCodexConfig(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "cannot render TOML table as scalar literal") {
		t.Fatalf("expected scalar table conflict, got %v", err)
	}
	if got := readCodexConfig(t, root); got != existing {
		t.Fatalf("expected existing config left untouched, got:\n%s", got)
	}
}

func TestCodexTomlEditor_RemovesManagedNamespaceAcrossEquivalentForms(t *testing.T) {
	t.Parallel()
	editor := newCodexTomlEditor(`
mcp_servers = { inline = { command = "old" } }
mcp_servers.dotted.command = "old"

[mcp_servers.table]
command = "old"

[unrelated]
mcp_servers = "not-root"
`)

	editor.removeNamespace([]string{config.CodexMCPServersKey})
	out := editor.render()
	if strings.Contains(out, "inline") || strings.Contains(out, "dotted") || strings.Contains(out, "[mcp_servers.table]") {
		t.Fatalf("expected root mcp_servers namespace removed, got:\n%s", out)
	}
	if !strings.Contains(out, `[unrelated]`) || !strings.Contains(out, `mcp_servers = "not-root"`) {
		t.Fatalf("expected unrelated table entry preserved, got:\n%s", out)
	}
	assertValidTOML(t, out)
}

func TestCodexTomlEditor_SetPathPlacesRootBeforeTablesAndCreatesTables(t *testing.T) {
	t.Parallel()
	editor := newCodexTomlEditor(`[hooks.state]
last_seen = "keep"
`)

	editor.setPath([]string{config.CodexModelKey}, `"gpt-5"`)
	editor.setPath([]string{"tui", "status_line"}, `["weekly-limit"]`)

	out := editor.render()
	if !strings.HasPrefix(out, `model = "gpt-5"`+"\n[hooks.state]") {
		t.Fatalf("expected root scalar before first table, got:\n%s", out)
	}
	if !strings.Contains(out, "\n[tui]\nstatus_line = [\"weekly-limit\"]") {
		t.Fatalf("expected tui table created, got:\n%s", out)
	}
	assertValidTOML(t, out)
}

func TestCodexTomlEditor_RemovesEmptyInlineTableAfterDeletingNestedValue(t *testing.T) {
	t.Parallel()
	editor := newCodexTomlEditor(`features = { apps = false }
[hooks.state]
keep = true
`)

	editor.removePath([]string{"features", "apps"})
	out := editor.render()
	if strings.Contains(out, "features") {
		t.Fatalf("expected empty inline features table removed, got:\n%s", out)
	}
	if !strings.Contains(out, "[hooks.state]") {
		t.Fatalf("expected unrelated table preserved, got:\n%s", out)
	}
}

func TestFormatInlineValue_RendersCompositeLiteralsForCodexPassthrough(t *testing.T) {
	t.Parallel()
	got := formatInlineValue(map[string]any{
		"enabled": true,
		"limit":   int64(3),
		"names":   []any{"one", "two"},
		"ratio":   1.5,
	})
	for _, expected := range []string{
		`enabled = true`,
		`limit = 3`,
		`names = ["one", "two"]`,
		`ratio = 1.5`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("expected %q in inline literal %q", expected, got)
		}
	}
	if literal, err := tomlLiteral("value"); err != nil || literal != `"value"` {
		t.Fatalf("unexpected scalar literal %q err=%v", literal, err)
	}
}

func TestMergeCodexConfig_FailsOnInvalidManagedFragment(t *testing.T) {
	t.Parallel()
	_, err := mergeCodexConfig("config.toml", "", codexManagedConfig{
		Content:     "[broken\n",
		TrustedRoot: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "generated Codex config is invalid TOML") {
		t.Fatalf("expected invalid managed fragment error, got %v", err)
	}
}

func TestCodexPathHandledElsewhere_ClassifiesManagedAndPassthroughPaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path []string
		want bool
	}{
		{path: []string{config.CodexModelKey}, want: true},
		{path: []string{config.CodexProjectsKey, "/repo"}, want: true},
		{path: []string{"features", config.CodexFeatureAppsKey}, want: true},
		{path: []string{"features", "custom"}, want: false},
		{path: []string{"tui", "status_line"}, want: true},
		{path: []string{"tui", "notifications"}, want: false},
		{path: []string{"custom", "leaf"}, want: false},
	}
	for _, tt := range tests {
		if got := codexPathHandledElsewhere(tt.path); got != tt.want {
			t.Fatalf("codexPathHandledElsewhere(%#v) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func writeExistingCodexConfig(t *testing.T, root string, content string) {
	t.Helper()
	path := filepath.Join(root, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write existing codex config: %v", err)
	}
}

func readCodexConfig(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".codex", "config.toml")) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read codex config: %v", err)
	}
	return string(data)
}

func parseCodexConfig(t *testing.T, content string) map[string]any {
	t.Helper()
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("parse codex config: %v\n%s", err, content)
	}
	return parsed
}

func assertValidTOML(t *testing.T, content string) {
	t.Helper()
	_ = parseCodexConfig(t, content)
}

func codexChimeBlockForTest() string {
	var builder strings.Builder
	appendCodexChimeBlock(&builder)
	return builder.String()
}
