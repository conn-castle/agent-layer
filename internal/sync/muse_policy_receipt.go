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
