package benchmark

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// TaskTreeChecksum returns Pier 0.3.1's deterministic task-directory identity.
func TaskTreeChecksum(root string) (string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("inspect task checksum root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("task checksum root must be a directory")
	}
	checksum, err := pierDirectoryChecksum(root)
	if err != nil {
		return "", err
	}
	if checksum == emptyPierDirectoryChecksum {
		return "", fmt.Errorf("task checksum root contains no files")
	}
	return checksum, nil
}

func pierDirectoryChecksum(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	descriptors := make([]string, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("task checksum does not accept symbolic link %s", path)
		}
		var property string
		if entry.IsDir() {
			child, err := pierDirectoryChecksum(path)
			if err != nil {
				return "", err
			}
			if child == emptyPierDirectoryChecksum {
				continue
			}
			property = "dirhash:" + child
		} else {
			data, err := os.ReadFile(path) // #nosec G304 -- path is below the explicit task root.
			if err != nil {
				return "", err
			}
			property = "data:" + digestBytes(data)
		}
		descriptors = append(descriptors, property+"\x00name:"+entry.Name())
	}
	sort.Strings(descriptors)
	return digestBytes([]byte(strings.Join(descriptors, "\x00\x00"))), nil
}

var emptyPierDirectoryChecksum = digestBytes(nil)
