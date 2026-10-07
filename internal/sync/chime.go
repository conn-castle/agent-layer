package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

const (
	agentLayerChimeMarker                   = "agent-layer-chime"
	agentLayerClaudeChimeCommand            = `al hook chime claude || { echo 'agent-layer chime handler unavailable' >&2; true; } # agent-layer-chime`
	agentLayerCodexChimeCommand             = `al hook chime codex || { printf 'agent-layer chime handler unavailable\n' >&2; printf '{}\n'; } # agent-layer-chime`
	agentLayerAntigravityChimeCommand       = `al hook chime antigravity || { printf 'agent-layer chime handler unavailable\n' >&2; printf '{"decision":"allow"}\n'; } # agent-layer-chime`
	agentLayerGrokChimeCommand              = `al hook chime grok || { echo 'agent-layer chime handler unavailable' >&2; true; } # agent-layer-chime`
	legacyAgentLayerClaudeChimeCommand      = "/usr/bin/afplay /System/Library/Sounds/Blow.aiff >/dev/null 2>&1 & # agent-layer-chime"
	legacyAgentLayerCodexChimeCommand       = `/usr/bin/afplay /System/Library/Sounds/Blow.aiff >/dev/null 2>&1 & printf '{"continue":true}\n' # agent-layer-chime`
	legacyAgentLayerAntigravityChimeCommand = `/usr/bin/afplay /System/Library/Sounds/Blow.aiff >/dev/null 2>&1 & printf '{"decision":"allow"}\n' # agent-layer-chime`
	agentLayerChimeTimeout                  = 5
	codexChimeBeginMarker                   = "# BEGIN Agent Layer-managed chime hook. Source: .agent-layer/config.toml [notifications].chime."
	codexChimeEndMarker                     = "# END Agent Layer-managed chime hook."
	chimeHandlerTypeKey                     = "type"
	chimeHandlerCommandKey                  = "command"
	chimeHandlerCommandType                 = "command" //nolint:goconst // The type value is independent from the same-named field key.
	chimeHandlerTimeoutKey                  = "timeout"
	hooksKey                                = "hooks"
	stopHookKey                             = "Stop"
)

func legacyChimeCommandVariants(command string) map[string]struct{} {
	variants := map[string]struct{}{command: {}}
	if unmarked, ok := strings.CutSuffix(command, " # "+agentLayerChimeMarker); ok {
		variants[unmarked] = struct{}{}
	}
	return variants
}

func managedChimeCommandVariants(command string) map[string]struct{} {
	commands := []string{command}
	switch command {
	case agentLayerClaudeChimeCommand:
		commands = append(commands, legacyAgentLayerClaudeChimeCommand)
	case agentLayerCodexChimeCommand:
		commands = append(commands, legacyAgentLayerCodexChimeCommand)
	case agentLayerAntigravityChimeCommand:
		commands = append(commands, legacyAgentLayerAntigravityChimeCommand)
	}
	variants := make(map[string]struct{}, len(commands)*2)
	for _, candidate := range commands {
		for variant := range legacyChimeCommandVariants(candidate) {
			variants[variant] = struct{}{}
		}
	}
	return variants
}

func ensureNoLegacyAgentSpecificChime(path string, hooks any, command string) error {
	if hooks == nil {
		return nil
	}
	if containsExactChimeCommand(hooks, managedChimeCommandVariants(command)) {
		return fmt.Errorf(messages.SyncLegacyAgentSpecificChimeFmt, path)
	}
	return nil
}

func containsExactChimeCommand(value any, commands map[string]struct{}) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if key == chimeHandlerCommandKey {
				if command, ok := nested.(string); ok {
					if _, match := commands[command]; match {
						return true
					}
				}
			}
			if containsExactChimeCommand(nested, commands) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if containsExactChimeCommand(nested, commands) {
				return true
			}
		}
	}
	return false
}

// containsChimeCommandText reports whether JSON content mentions a managed
// chime command, either raw or with any valid JSON string escaping.
// It remains a pre-check; structural matching decides which hooks to remove.
func containsChimeCommandText(content string, command string) bool {
	variants := managedChimeCommandVariants(command)
	for variant := range variants {
		if strings.Contains(content, variant) {
			return true
		}
		escaped, err := json.Marshal(variant)
		if err == nil && strings.Contains(content, strings.Trim(string(escaped), `"`)) {
			return true
		}
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		if value, ok := token.(string); ok {
			for variant := range variants {
				if strings.Contains(value, variant) {
					return true
				}
			}
		}
	}
}

func chimeHandler(command string) map[string]any {
	return map[string]any{
		chimeHandlerTypeKey:    chimeHandlerCommandType,
		chimeHandlerCommandKey: command,
		chimeHandlerTimeoutKey: agentLayerChimeTimeout,
	}
}

func chimeHandlerMatchesAny(value any, commands map[string]struct{}) bool {
	handler, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if len(handler) != 3 {
		return false
	}
	command, ok := handler[chimeHandlerCommandKey].(string)
	if !ok {
		return false
	}
	if handler[chimeHandlerTypeKey] != chimeHandlerCommandType {
		return false
	}
	if _, ok := commands[command]; !ok {
		return false
	}
	return numericEquals(handler[chimeHandlerTimeoutKey], agentLayerChimeTimeout)
}

// filterHookGroupHandlers removes handlers matching drop from every hook
// group whose hooks value is a list, updating kept groups in place. Groups
// left with no handlers and no other keys are dropped; other entries are kept
// unchanged. When nonListHooksErr is non-nil, a group whose present hooks
// value is not a list stops filtering with that error, leaving groups already
// filtered as updated. It reports whether any handler or group was removed.
func filterHookGroupHandlers(entries []any, drop func(any) bool, nonListHooksErr error) ([]any, bool, error) {
	changed := false
	result := make([]any, 0, len(entries))
	for _, entry := range entries {
		group, ok := entry.(map[string]any)
		if !ok {
			result = append(result, entry)
			continue
		}
		handlersValue, present := group[hooksKey]
		handlers, ok := handlersValue.([]any)
		if !ok {
			if present && nonListHooksErr != nil {
				return nil, false, nonListHooksErr
			}
			result = append(result, entry)
			continue
		}
		kept := make([]any, 0, len(handlers))
		for _, handler := range handlers {
			if drop(handler) {
				changed = true
				continue
			}
			kept = append(kept, handler)
		}
		if len(kept) == 0 && len(group) == 1 {
			changed = true
			continue
		}
		group[hooksKey] = kept
		result = append(result, group)
	}
	return result, changed, nil
}

// ensureChimePathContained rejects a chime target outside root/providerDir/ownedDir
// and refuses symlinks at the provider directory, owned directory, or target.
// outsideFmt receives the uncleaned target; conflictFmt receives the symlinked path.
func ensureChimePathContained(sys System, root string, target string, providerDir string, ownedDir string, outsideFmt string, conflictFmt string) error {
	ownedRoot := filepath.Clean(filepath.Join(root, providerDir, ownedDir))
	cleanTarget := filepath.Clean(target)
	if cleanTarget != ownedRoot && !strings.HasPrefix(cleanTarget, ownedRoot+string(os.PathSeparator)) {
		return fmt.Errorf(outsideFmt, target)
	}
	for _, path := range []string{
		filepath.Join(root, providerDir),
		ownedRoot,
		cleanTarget,
	} {
		info, err := sys.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf(messages.InstallFailedStatFmt, path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf(conflictFmt, path)
		}
	}
	return nil
}

func numericEquals(value any, want int) bool {
	switch typed := value.(type) {
	case int:
		return typed == want
	case int64:
		return typed == int64(want)
	case float64:
		return typed == float64(want)
	default:
		return false
	}
}

// chimeCleanupTarget is an existing provider config file that may hold an
// Agent Layer chime hook.
type chimeCleanupTarget struct {
	path string
	mode os.FileMode
	// linked is the symlinked directory or file through which path was
	// reached, or empty when both are real in-repo entries.
	linked string
}

// checkWritable refuses to rewrite a config reached through a symlink, which
// may live outside the repository.
func (t chimeCleanupTarget) checkWritable() error {
	if t.linked != "" {
		return fmt.Errorf(messages.SyncChimePathConflictFmt, t.linked)
	}
	return nil
}

// existingChimeCleanupTarget returns an existing provider config file whose
// parent is a directory and which is itself a regular file, following
// user-managed symlinks so cleanup can inspect content it must not rewrite.
// Missing paths and dangling symlinks are reported as not existing so cleanup
// remains idempotent for disabled providers.
func existingChimeCleanupTarget(sys System, root string, dirName string, fileName string) (chimeCleanupTarget, bool, error) {
	dir := filepath.Join(root, dirName)
	target := chimeCleanupTarget{path: filepath.Join(dir, fileName)}
	dirInfo, dirLinked, err := statChimeCleanupEntry(sys, dir)
	if err != nil || dirInfo == nil {
		return target, false, err
	}
	if !dirInfo.IsDir() {
		return target, false, fmt.Errorf(messages.SyncChimePathConflictFmt, dir)
	}
	fileInfo, fileLinked, err := statChimeCleanupEntry(sys, target.path)
	if err != nil || fileInfo == nil {
		return target, false, err
	}
	if !fileInfo.Mode().IsRegular() {
		return target, false, fmt.Errorf(messages.SyncChimePathConflictFmt, target.path)
	}
	target.mode = fileInfo.Mode().Perm()
	switch {
	case dirLinked:
		target.linked = dir
	case fileLinked:
		target.linked = target.path
	}
	return target, true, nil
}

// statChimeCleanupEntry describes path, following a symlink and reporting
// that it did. A missing path or dangling symlink returns nil info.
func statChimeCleanupEntry(sys System, path string) (os.FileInfo, bool, error) {
	info, err := sys.Lstat(path)
	linked := err == nil && info.Mode()&os.ModeSymlink != 0
	if linked {
		info, err = sys.Stat(path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return nil, linked, nil
		}
		return nil, linked, fmt.Errorf(messages.InstallFailedStatFmt, path, err)
	}
	return info, linked, nil
}
