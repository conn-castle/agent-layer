package config

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

var osReadFileFunc = os.ReadFile

// LoadInstructions reads .agent-layer/instructions/*.md in lexicographic order.
func LoadInstructions(dir string) ([]InstructionFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingInstructionsDirFmt, dir, err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".md") {
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return nil, fmt.Errorf(messages.ConfigNoInstructionFilesFmt, dir)
	}

	sort.Strings(names)

	files := make([]InstructionFile, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := osReadFileFunc(path)
		if err != nil {
			return nil, fmt.Errorf(messages.ConfigFailedReadInstructionFmt, path, err)
		}
		data = bytes.TrimPrefix(data, utf8BOM)
		files = append(files, InstructionFile{
			Name:    name,
			Content: string(data),
		})
	}

	return files, nil
}

// walkInstructionFiles is a helper to walk instruction files in a directory.
func walkInstructionFiles(dir string, fn func(path string, entry fs.DirEntry) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if err := fn(path, entry); err != nil {
			return err
		}
	}
	return nil
}
