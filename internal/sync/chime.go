package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

const (
	agentLayerChimeMarker             = "agent-layer-chime"
	agentLayerClaudeChimeCommand      = "/usr/bin/afplay /System/Library/Sounds/Blow.aiff >/dev/null 2>&1 & # agent-layer-chime"
	agentLayerCodexChimeCommand       = `/usr/bin/afplay /System/Library/Sounds/Blow.aiff >/dev/null 2>&1 & printf '{"continue":true}\n' # agent-layer-chime`
	agentLayerAntigravityChimeCommand = `/usr/bin/afplay /System/Library/Sounds/Blow.aiff >/dev/null 2>&1 & printf '{"decision":"allow"}\n' # agent-layer-chime`
	agentLayerChimeTimeout            = 5
	codexChimeBeginMarker             = "# BEGIN Agent Layer-managed chime hook. Source: .agent-layer/config.toml [notifications].chime."
	codexChimeEndMarker               = "# END Agent Layer-managed chime hook."
	chimeHandlerTypeKey               = "type"
	chimeHandlerCommandKey            = "command"
	chimeHandlerCommandType           = "command" //nolint:goconst // The type value is independent from the same-named field key.
	chimeHandlerTimeoutKey            = "timeout"
	hooksKey                          = "hooks"
	stopHookKey                       = "Stop"
)

func legacyChimeCommandVariants(command string) map[string]struct{} {
	variants := map[string]struct{}{command: {}}
	if unmarked, ok := strings.CutSuffix(command, " # "+agentLayerChimeMarker); ok {
		variants[unmarked] = struct{}{}
	}
	return variants
}

func ensureNoLegacyAgentSpecificChime(path string, hooks any, command string) error {
	if hooks == nil {
		return nil
	}
	if containsExactChimeCommand(hooks, legacyChimeCommandVariants(command)) {
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

func containsChimeCommandText(content string, command string) bool {
	for variant := range legacyChimeCommandVariants(command) {
		if strings.Contains(content, variant) {
			return true
		}
	}
	return false
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

// existingChimeCleanupTarget returns an existing provider config path only when
// both its parent directory and the file itself are real in-repo filesystem
// entries. Missing paths are reported as not existing so cleanup remains
// idempotent for disabled providers.
func existingChimeCleanupTarget(sys System, root string, dirName string, fileName string) (string, bool, error) {
	dir := filepath.Join(root, dirName)
	path := filepath.Join(dir, fileName)
	dirInfo, err := sys.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return path, false, nil
		}
		return path, false, fmt.Errorf(messages.InstallFailedStatFmt, dir, err)
	}
	if dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return path, false, fmt.Errorf(messages.SyncChimePathConflictFmt, dir)
	}
	fileInfo, err := sys.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, false, nil
		}
		return path, false, fmt.Errorf(messages.InstallFailedStatFmt, path, err)
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 || !fileInfo.Mode().IsRegular() {
		return path, false, fmt.Errorf(messages.SyncChimePathConflictFmt, path)
	}
	return path, true, nil
}
