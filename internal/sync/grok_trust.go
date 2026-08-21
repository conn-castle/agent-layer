package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/tomlpatch"
)

const grokTrustedFoldersHeader = `# Seeded by Agent Layer so project hooks, MCP, and LSP load without /hooks-trust.
# Existing folder entries are preserved on later syncs.

`

const grokHomeClaudeCompatHeader = `# Seeded by Agent Layer so Grok loads AGENTS.md and skips .claude/CLAUDE.md.

`

const grokClaudeAgentsCompatSection = `[compat.claude]
agents = false
`

const grokCompatClaudeSection = "compat.claude"

type grokTrustedFoldersFile struct {
	Folders map[string]any `toml:"folders"`
}

func writeGrokTrustedFolders(sys System, root string) error {
	absRoot, err := grokTrustedFolderRoot(root)
	if err != nil {
		return err
	}

	homeDir := filepath.Join(root, ".grok-config")
	if err := fsutil.EnsurePrivateDir(homeDir); err != nil {
		return fmt.Errorf(messages.SyncGrokHomeEnsureFailedFmt, err)
	}

	path := filepath.Join(homeDir, "trusted_folders.toml")
	existing, err := sys.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(messages.SyncGrokTrustReadFailedFmt, path, err)
	}

	if len(existing) > 0 {
		var parsed grokTrustedFoldersFile
		if err := toml.Unmarshal(existing, &parsed); err != nil {
			return fmt.Errorf(messages.SyncGrokTrustParseFailedFmt, path, err)
		}
		if _, exists := parsed.Folders[absRoot]; exists {
			return nil
		}
		content := strings.TrimRight(string(existing), "\n") + "\n\n" + grokTrustedFolderSection(absRoot, sys.Now().Unix())
		if err := sys.WriteFileAtomic(path, []byte(content), 0o600); err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
		}
		return nil
	}

	content := grokTrustedFoldersHeader + grokTrustedFolderSection(absRoot, sys.Now().Unix())
	if err := sys.WriteFileAtomic(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	return nil
}

func grokTrustedFolderRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("%s", messages.SyncGrokTrustRootRequired)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf(messages.SyncGrokTrustRootResolveFailedFmt, root, err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf(messages.SyncGrokTrustRootResolveFailedFmt, absRoot, err)
	}
	return canonicalRoot, nil
}

func grokTrustedFolderSection(absRoot string, decidedAt int64) string {
	return fmt.Sprintf("[folders.%s]\ntrusted = true\ndecided_at = %d\n", tomlpatch.FormatKey(absRoot), decidedAt)
}

type grokHomeCompatFile struct {
	Compat struct {
		Claude struct {
			Agents *bool `toml:"agents"`
		} `toml:"claude"`
	} `toml:"compat"`
}

// writeGrokHomeClaudeCompat sets [compat.claude] agents = false in repo-local
// GROK_HOME config so Grok does not load .claude/CLAUDE.md alongside AGENTS.md.
func writeGrokHomeClaudeCompat(sys System, root string) error {
	homeDir := filepath.Join(root, ".grok-config")
	if err := fsutil.EnsurePrivateDir(homeDir); err != nil {
		return fmt.Errorf(messages.SyncGrokHomeEnsureFailedFmt, err)
	}

	path := filepath.Join(homeDir, "config.toml")
	exists, err := validateGrokConfigFile(sys, path)
	if err != nil {
		return err
	}
	if !exists {
		content := grokHomeClaudeCompatHeader + grokClaudeAgentsCompatSection
		if err := sys.WriteFileAtomic(path, []byte(content), 0o600); err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
		}
		return nil
	}

	existing, err := sys.ReadFile(path)
	if err != nil {
		return fmt.Errorf(messages.SyncGrokHomeConfigReadFailedFmt, path, err)
	}
	disabled, err := grokClaudeAgentsDisabled(existing)
	if err != nil {
		return fmt.Errorf(messages.SyncGrokHomeConfigParseFailedFmt, path, err)
	}
	if disabled {
		return nil
	}

	updated, err := applyGrokClaudeAgentsCompat(string(existing))
	if err != nil {
		return fmt.Errorf(messages.SyncGrokHomeConfigParseFailedFmt, path, err)
	}
	if err := grokClaudeAgentsCompatApplied([]byte(updated)); err != nil {
		return fmt.Errorf(messages.SyncGrokHomeConfigCompatFmt, path, err)
	}
	if err := sys.WriteFileAtomic(path, []byte(updated), 0o600); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	return nil
}

func grokClaudeAgentsDisabled(data []byte) (bool, error) {
	var parsed grokHomeCompatFile
	if err := toml.Unmarshal(data, &parsed); err != nil {
		return false, err
	}
	return parsed.Compat.Claude.Agents != nil && !*parsed.Compat.Claude.Agents, nil
}

func grokClaudeAgentsCompatApplied(data []byte) error {
	disabled, err := grokClaudeAgentsDisabled(data)
	if err != nil {
		return err
	}
	if !disabled {
		return fmt.Errorf("%s", messages.SyncGrokHomeConfigCompatUnapplied)
	}
	return nil
}

func applyGrokClaudeAgentsCompat(content string) (string, error) {
	doc := tomlpatch.ParseDocument(content)
	block, ok := doc.Sections[grokCompatClaudeSection]
	if !ok {
		trimmed := strings.TrimRight(content, "\n")
		if trimmed == "" {
			return grokHomeClaudeCompatHeader + grokClaudeAgentsCompatSection, nil
		}
		return trimmed + "\n\n" + grokClaudeAgentsCompatSection, nil
	}
	oldSection := strings.Join(block.Lines, "\n")
	tomlpatch.SetKeyValue(block, nil, "agents", "false", "")
	newSection := strings.Join(block.Lines, "\n")
	if oldSection == newSection {
		return content, nil
	}
	if !strings.Contains(content, oldSection) {
		return "", fmt.Errorf("%s", messages.SyncGrokHomeConfigCompatUnapplied)
	}
	return strings.Replace(content, oldSection, newSection, 1), nil
}
