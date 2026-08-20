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
