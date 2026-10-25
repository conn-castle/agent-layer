package skilljournal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// CheckAdoptionStaging recognizes retired recovery evidence without changing it.
// Check each ancestor before reading so linked tiers cannot escape the project.
func CheckAdoptionStaging(stagingRoot string) error {
	for _, dir := range []string{filepath.Dir(filepath.Dir(stagingRoot)), filepath.Dir(stagingRoot), stagingRoot} {
		exists, err := recoveryDirectoryExists(dir)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
	}
	entries, err := os.ReadDir(stagingRoot)
	if err != nil {
		return err
	}
	var findings []error
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), LocalBackupPrefix) {
			findings = append(findings, fmt.Errorf("%w: "+messages.AdoptionJournalFmt, ErrAdoptionJournal, filepath.Join(stagingRoot, entry.Name())))
		}
	}
	path := filepath.Join(stagingRoot, FileName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return errors.Join(findings...)
	}
	if err != nil {
		return errors.Join(append(findings, err)...)
	}
	if !info.Mode().IsRegular() {
		return errors.Join(append(findings, fmt.Errorf("%w: "+messages.JournalRegularFileFmt, ErrMalformed, path))...)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- validated regular journal under real recovery directories.
	if err != nil {
		return errors.Join(append(findings, err)...)
	}
	var doc Document
	if json.Unmarshal(data, &doc) == nil && (doc.Version == AdoptionVersion || len(doc.LocalRetirements) > 0) {
		findings = append(findings, fmt.Errorf("%w: "+messages.AdoptionJournalFmt, ErrAdoptionJournal, path))
	}
	return errors.Join(findings...)
}
