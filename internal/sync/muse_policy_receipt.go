package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/musepolicy"
)

// This local receipt authorizes bounded cleanup without probing native user
// state in projects that never enabled Muse. It contains no credentials.
type musePolicyReceipt = musepolicy.Receipt

func readMusePolicyReceipt(sys System, path string) (*musePolicyReceipt, error) {
	document, err := readMuseSettings(sys, path)
	if err != nil {
		// OS path errors already identify the file; avoid repeating the path
		// from readMuseSettings while keeping the underlying error available.
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			err = pathErr
		}
		return nil, fmt.Errorf("invalid Agent Layer Muse policy ownership receipt: %w", err)
	}
	if len(document) == 0 {
		if _, err := sys.Lstat(path); os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("invalid Agent Layer Muse policy ownership receipt %s: empty document", path)
	}
	data, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("invalid Agent Layer Muse policy ownership receipt %s: %w", path, err)
	}
	receipt, err := musepolicy.ParseReceipt(data)
	if err != nil {
		return nil, fmt.Errorf("invalid Agent Layer Muse policy ownership receipt %s: %w", path, err)
	}
	return receipt, nil
}

// musePolicyRetirementDirectory returns the native policy directory recorded
// for this workspace. Directory and Root are forgeable local JSON; they are
// never used as SyncCommands arguments unless both paths are clean and Root
// exactly matches the trusted current workspace. A previous native directory
// for this same workspace may still be retired after HOME/XDG changes.
func musePolicyRetirementDirectory(receipt *musePolicyReceipt, root string) (string, error) {
	if receipt == nil {
		return "", nil
	}
	if receipt.Root != filepath.Clean(receipt.Root) || receipt.Directory != filepath.Clean(receipt.Directory) {
		return "", fmt.Errorf("workspace_root and directory must be clean paths")
	}
	if receipt.Root != root {
		return "", fmt.Errorf("workspace_root does not match the current workspace")
	}
	return receipt.Directory, nil
}

func writeMusePolicyReceipt(sys System, path, directory, root string) error {
	data, err := sys.MarshalIndent(musePolicyReceipt{GeneratedBy: mcpGeneratedBy, Directory: directory, Root: root}, "", "  ")
	if err != nil {
		return err
	}
	if err := sys.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return sys.WriteFileAtomic(path, append(data, '\n'), os.FileMode(0o600))
}
