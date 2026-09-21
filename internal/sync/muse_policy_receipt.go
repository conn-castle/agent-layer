package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// This local receipt authorizes bounded cleanup without probing native user
// state in projects that never enabled Muse. It contains no credentials.
type musePolicyReceipt struct {
	GeneratedBy string `json:"generated_by"`
	Directory   string `json:"directory"`
	Root        string `json:"workspace_root"`
}

func readMusePolicyReceipt(sys System, path string) (*musePolicyReceipt, error) {
	document, err := readMuseSettings(sys, path)
	if err != nil {
		return nil, fmt.Errorf("read Muse policy ownership receipt %s: %w", path, err)
	}
	if len(document) == 0 {
		if _, err := sys.Lstat(path); os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("invalid empty Muse policy ownership receipt: %s", path)
	}
	data, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("read Muse policy ownership receipt %s: %w", path, err)
	}
	var receipt musePolicyReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return nil, fmt.Errorf("read Muse policy ownership receipt %s: %w", path, err)
	}
	if receipt.GeneratedBy != mcpGeneratedBy || !filepath.IsAbs(receipt.Directory) || !filepath.IsAbs(receipt.Root) || filepath.Base(receipt.Directory) != museDirectoryName {
		return nil, fmt.Errorf("invalid Agent Layer Muse policy ownership receipt: %s", path)
	}
	return &receipt, nil
}

// musePolicyRetirementDirectory returns the native policy directory recorded
// for this workspace. Directory and Root are forgeable local JSON; they are
// never used as SyncCommands arguments unless Root matches the trusted current
// workspace. A previous native directory for this same workspace may still be
// retired after HOME/XDG changes.
func musePolicyRetirementDirectory(receipt *musePolicyReceipt, root string) (string, error) {
	if receipt == nil {
		return "", nil
	}
	if filepath.Clean(receipt.Root) != filepath.Clean(root) {
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
