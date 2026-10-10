package sync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/herdr"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/tomlpatch"
)

const (
	agentLayerHerdRMarker = "agent-layer-herdr"
	herdrCommandHookType  = `type = "command"`
	// HerdR persists session changes with a five-second debounce. Four bounded
	// verification attempts fit below this native-hook timeout without leaving
	// verification detached from the hook that reported the command.
	herdrTimeout            = herdr.HookTimeoutSeconds
	herdrMuseEvent          = "SessionStart"
	herdrMuseFirstTurnEvent = "UserPromptSubmit"
	herdrAgyProvider        = "agy"
	herdrGrokProvider       = "grok"
	herdrEnabledKey         = "enabled"
	codexHerdRBeginMarker   = "# BEGIN Agent Layer-managed HerdR recovery hook."
	codexHerdREndMarker     = "# END Agent Layer-managed HerdR recovery hook."
)

// herdrCommand is materialized during sync, rather than relying on a native
// hook retaining Agent Layer's environment. A development sync records the
// exact absolute source executable and bypass flag; release syncs use the
// normal `al` resolution. This deliberately does not persist any other env.
func herdrCommand(provider string, arguments ...string) string {
	argumentsText := ""
	for index, argument := range arguments {
		if canonical, err := filepath.EvalSymlinks(argument); index == 0 && err == nil {
			argument = canonical
		}
		argumentsText += " " + shellSingleQuote(argument)
	}
	executable := canonicalHerdRDevelopmentExecutable()
	if os.Getenv("AL_DEV_BYPASS_VERSION_DISPATCH") != "" && executable != "" {
		quoted := shellSingleQuote(executable)
		return fmt.Sprintf("AL_DEV_BYPASS_VERSION_DISPATCH=1 AL_DEV_EXECUTABLE=%s exec %s hook herdr %s%s # %s", quoted, quoted, provider, argumentsText, agentLayerHerdRMarker)
	}
	return fmt.Sprintf("exec al hook herdr %s%s # %s", provider, argumentsText, agentLayerHerdRMarker)
}

func canonicalHerdRDevelopmentExecutable() string {
	executable := strings.TrimSpace(os.Getenv("AL_DEV_EXECUTABLE"))
	if !filepath.IsAbs(executable) {
		return ""
	}
	canonical, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return ""
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return ""
	}
	return filepath.Clean(canonical)
}

func herdrHandler(command string) map[string]any {
	return map[string]any{chimeHandlerTypeKey: chimeHandlerCommandType, chimeHandlerCommandKey: command, chimeHandlerTimeoutKey: herdrTimeout}
}

func isHerdRHandler(value any) bool {
	handler, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if handler[chimeHandlerTypeKey] != chimeHandlerCommandType {
		return false
	}
	command, _ := handler[chimeHandlerCommandKey].(string)
	// Release and development hooks have always emitted this terminal marker.
	// A marker in an argument or filename does not establish hook ownership.
	return strings.HasSuffix(command, " # "+agentLayerHerdRMarker)
}

func injectClaudeHerdRHookAtRoot(settings map[string]any, root string) error {
	return injectEventHook(settings, "SessionStart", herdrCommand("claude", root))
}

func injectEventHook(settings map[string]any, event, command string) error {
	raw, exists := settings[hooksKey]
	var hooks map[string]any
	if !exists {
		hooks = map[string]any{}
		settings[hooksKey] = hooks
	} else {
		var ok bool
		hooks, ok = raw.(map[string]any)
		if !ok {
			return fmt.Errorf("hooks must be a table")
		}
	}
	entries, err := removeManagedHandlers(hooks[event])
	if err != nil {
		return fmt.Errorf("hooks.%s: %w", event, err)
	}
	hooks[event] = append(entries, map[string]any{hooksKey: []any{herdrHandler(command)}})
	return nil
}

func removeManagedHandlers(value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}
	entries, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("must be an array")
	}
	result, _, _ := filterHookGroupHandlers(entries, isHerdRHandler, nil)
	return result, nil
}

func cleanClaudeHerdRHook(sys System, root string) error {
	target, exists, err := existingChimeCleanupTarget(sys, root, ".claude", "settings.json")
	if err != nil || !exists {
		return err
	}
	path := target.path
	data, err := sys.ReadFile(path)
	if err != nil || !strings.Contains(string(data), agentLayerHerdRMarker) {
		return err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("invalid Claude settings %s: %w", path, err)
	}
	hooks, ok := settings[hooksKey].(map[string]any)
	if !ok {
		return nil
	}
	entries, err := removeManagedHandlers(hooks["SessionStart"])
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		delete(hooks, "SessionStart")
	} else {
		hooks["SessionStart"] = entries
	}
	if len(hooks) == 0 {
		delete(settings, hooksKey)
	}
	if err := target.checkWritable(); err != nil {
		return err
	}
	output, err := sys.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return sys.WriteFileAtomic(path, append(output, '\n'), target.mode)
}

func writeAgyHerdRHook(sys System, root string) error {
	path := filepath.Join(root, ".agy", "config", "hooks.json")
	if err := ensureHerdRHookPathContained(sys, root, path); err != nil {
		return err
	}
	var document map[string]any
	data, err := sys.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(messages.SyncReadFailedFmt, path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		document = map[string]any{}
	} else if err := json.Unmarshal(data, &document); err != nil || document == nil {
		return fmt.Errorf("invalid Antigravity hooks %s", path)
	}
	document[agentLayerHerdRMarker] = map[string]any{herdrEnabledKey: true, "PreInvocation": []any{herdrHandler(herdrCommand(herdrAgyProvider, root))}}
	encoded, err := sys.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := sys.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return sys.WriteFileAtomic(path, append(encoded, '\n'), 0o600)
}

func cleanAgyHerdRHook(sys System, root string) error {
	path := filepath.Join(root, ".agy", "config", "hooks.json")
	if err := ensureHerdRHookPathContained(sys, root, path); err != nil {
		// Never follow, rewrite, or remove a user-managed symlinked path.
		if errors.Is(err, errHerdRHookPathConflict) {
			return nil
		}
		return err
	}
	data, err := sys.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), agentLayerHerdRMarker) {
		return nil
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("invalid Antigravity hooks %s: %w", path, err)
	}
	if _, ok := document[agentLayerHerdRMarker]; !ok {
		return nil
	}
	delete(document, agentLayerHerdRMarker)
	if len(document) == 0 {
		return sys.Remove(path)
	}
	encoded, err := sys.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return sys.WriteFileAtomic(path, append(encoded, '\n'), 0o600)
}

func providerHerdRHookPath(root, provider string) string {
	if provider == herdrGrokProvider {
		return filepath.Join(root, ".grok", "hooks", "agent-layer-herdr.json")
	}
	return filepath.Join(root, ".github", "hooks", "agent-layer-herdr.json")
}

func writeProviderHerdRHook(sys System, root, provider, event, command string) error {
	path := providerHerdRHookPath(root, provider)
	if err := ensureHerdRHookPathContained(sys, root, path); err != nil {
		return err
	}
	document := map[string]any{}
	data, err := sys.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &document); err != nil || document == nil {
			return fmt.Errorf("invalid %s HerdR hooks %s", provider, path)
		}
	}
	if err := injectEventHook(document, event, command); err != nil {
		return fmt.Errorf("update %s HerdR hooks %s: %w", provider, path, err)
	}
	encoded, err := sys.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := sys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return sys.WriteFileAtomic(path, append(encoded, '\n'), 0o600)
}

func cleanProviderHerdRHook(sys System, root, provider string) error {
	path := providerHerdRHookPath(root, provider)
	if err := ensureHerdRHookPathContained(sys, root, path); err != nil {
		// Never follow, rewrite, or remove a user-managed symlinked path.
		if errors.Is(err, errHerdRHookPathConflict) {
			return nil
		}
		return err
	}
	data, err := sys.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), agentLayerHerdRMarker) {
		return nil
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil || document == nil {
		return fmt.Errorf("invalid %s HerdR hooks %s", provider, path)
	}
	hooks, ok := document[hooksKey].(map[string]any)
	if !ok {
		return fmt.Errorf("%s HerdR hooks must be an object: %s", provider, path)
	}
	for event, entries := range hooks {
		var kept []any
		var err error
		kept, err = removeManagedHandlers(entries)
		if err != nil {
			return fmt.Errorf("clean %s hooks.%s: %w", provider, event, err)
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	if len(hooks) == 0 {
		onlyManaged := true
		for key := range document {
			if key != hooksKey {
				onlyManaged = false
			}
		}
		if onlyManaged {
			return sys.Remove(path)
		}
	}
	encoded, err := sys.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return sys.WriteFileAtomic(path, append(encoded, '\n'), 0o600)
}

var errHerdRHookPathConflict = errors.New("HerdR hook path conflict")

// ensureHerdRHookPathContained rejects a hook path outside root, or one reached
// through a symlinked or non-directory parent, or whose existing final entry is
// a symlink or non-regular file, so writes and cleanup never follow user links.
func ensureHerdRHookPathContained(sys System, root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return fmt.Errorf("HerdR hook path is outside the project: %s", path)
	}
	for directory := filepath.Dir(path); directory != root; directory = filepath.Dir(directory) {
		info, err := sys.Lstat(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf(messages.InstallFailedStatFmt, directory, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: %s", errHerdRHookPathConflict, directory)
		}
	}
	info, err := sys.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf(messages.InstallFailedStatFmt, path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", errHerdRHookPathConflict, path)
	}
	return nil
}

func injectMuseHerdRHook(document map[string]any, enabled bool, root string) error {
	raw, exists := document[hooksKey]
	hooks := map[string]any{}
	if exists {
		var ok bool
		hooks, ok = raw.(map[string]any)
		if !ok {
			return errors.New("muse hooks must be an object")
		}
	}
	for _, event := range []string{herdrMuseEvent, herdrMuseFirstTurnEvent} {
		entries, err := removeManagedHandlers(hooks[event])
		if err != nil {
			return fmt.Errorf("muse hooks.%s: %w", event, err)
		}
		if enabled {
			entries = append(entries, map[string]any{hooksKey: []any{herdrHandler(herdrCommand("muse", root))}})
		}
		if len(entries) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = entries
		}
	}
	if len(hooks) == 0 {
		delete(document, hooksKey)
	} else {
		document[hooksKey] = hooks
	}
	return nil
}

func (e *codexTomlEditor) applyCodexHerdRHook(path string, enabled bool, root ...string) (bool, error) {
	// Markers inside multiline strings are user content, not an owned block.
	start, end := -1, -1
	var markerErr error
	tomlpatch.WalkLinesOutsideMultiline(e.lines, func(index int, line string, _ tomlpatch.StringState) tomlpatch.LineWalkResult {
		switch strings.TrimSpace(line) {
		case codexHerdRBeginMarker:
			if start >= 0 {
				markerErr = fmt.Errorf("duplicate HerdR hook markers in %s", path)
				return tomlpatch.LineWalkResult{Stop: true}
			}
			start = index
		case codexHerdREndMarker:
			if start < 0 || end >= 0 {
				markerErr = fmt.Errorf("invalid HerdR hook markers in %s", path)
				return tomlpatch.LineWalkResult{Stop: true}
			}
			end = index
		}
		return tomlpatch.LineWalkResult{}
	})
	if markerErr != nil {
		return false, markerErr
	}
	if start >= 0 && end < 0 {
		return false, fmt.Errorf("unterminated HerdR hook marker in %s", path)
	}
	changed := false
	var preserved []string
	if start >= 0 {
		preserved = preserveCodexNativeHookState(e.lines[start+1 : end])
		e.removeRanges([]lineRange{{start: start, end: end}})
		changed = true
	}
	if !enabled {
		if len(preserved) > 0 {
			e.appendBlock(preserved)
		}
		return changed, nil
	}
	if e.rootInlineTableExists(hooksKey) {
		return false, fmt.Errorf(messages.SyncCodexExistingConfigShapeConflictFmt, path, hooksKey)
	}
	command := herdrCommand("codex", root...)
	e.appendBlock([]string{codexHerdRBeginMarker,
		"[[hooks.SessionStart]]", "[[hooks.SessionStart.hooks]]", herdrCommandHookType, fmt.Sprintf("command = %q", command), fmt.Sprintf("timeout = %d", herdrTimeout),
		"[[hooks.UserPromptSubmit]]", "[[hooks.UserPromptSubmit.hooks]]", herdrCommandHookType, fmt.Sprintf("command = %q", command), fmt.Sprintf("timeout = %d", herdrTimeout),
		codexHerdREndMarker})
	if len(preserved) > 0 {
		e.appendBlock(preserved)
	}
	return true, nil
}

func preserveCodexNativeHookState(lines []string) []string {
	for index, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[hooks.state") {
			return append([]string(nil), lines[index:]...)
		}
	}
	return nil
}
