package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

// UserHomeDir is a package-level variable to allow test stubbing across packages.
var UserHomeDir = os.UserHomeDir

const (
	geminiTrustFolder = "TRUST_FOLDER"
	geminiTrustFile   = "trustedFolders.json"
	geminiDir         = ".gemini"
	geminiDirPerm     = 0o755
	geminiTrustPerm   = 0o600
)

// EnsureGeminiTrustedFolder adds root to ~/.gemini/trustedFolders.json so that
// Gemini CLI treats the workspace as trusted and loads project-level settings.
// Returns a warning on failure; never returns an error.
func EnsureGeminiTrustedFolder(sys System, root string) *warnings.Warning {
	home, err := UserHomeDir()
	if err != nil {
		return geminiTrustWarning(fmt.Sprintf(messages.SyncGeminiTrustHomeDirFailedFmt, err))
	}

	dir := filepath.Join(home, geminiDir)
	path := filepath.Join(dir, geminiTrustFile)

	folders, err := readTrustedFolders(sys, path)
	if err != nil {
		var readErr *trustReadError
		if errors.As(err, &readErr) {
			return geminiTrustWarning(readErr.Error())
		}
		return geminiTrustWarning(fmt.Sprintf(messages.SyncGeminiTrustCorruptFmt, path, err))
	}

	// Already trusted — no-op.
	if folders[root] == geminiTrustFolder {
		return nil
	}

	folders[root] = geminiTrustFolder

	data, err := sys.MarshalIndent(folders, "", "  ")
	if err != nil {
		return geminiTrustWarning(fmt.Sprintf(messages.SyncGeminiTrustMarshalFailedFmt, err))
	}
	// Append trailing newline for POSIX compliance.
	data = append(data, '\n')

	if err := sys.MkdirAll(dir, geminiDirPerm); err != nil {
		return geminiTrustWarning(fmt.Sprintf(messages.SyncGeminiTrustCreateDirFailedFmt, dir, err))
	}

	if err := sys.WriteFileAtomic(path, data, geminiTrustPerm); err != nil {
		return geminiTrustWarning(fmt.Sprintf(messages.SyncGeminiTrustWriteFailedFmt, path, err))
	}

	return nil
}

// trustReadError is an I/O read error (distinct from corrupt JSON).
type trustReadError struct{ err error }

func (e *trustReadError) Error() string { return e.err.Error() }
func (e *trustReadError) Unwrap() error { return e.err }

// readTrustedFolders reads and parses the trusted folders JSON file.
// Missing or empty files return an empty map with nil error.
// I/O failures return a *trustReadError; corrupt JSON returns a plain error.
func readTrustedFolders(sys System, path string) (map[string]string, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, &trustReadError{fmt.Errorf(messages.SyncGeminiTrustReadFailedFmt, path, err)}
	}

	if len(data) == 0 {
		return make(map[string]string), nil
	}

	var folders map[string]string
	if err := json.Unmarshal(data, &folders); err != nil {
		return nil, err
	}
	if folders == nil {
		folders = make(map[string]string)
	}
	return folders, nil
}

// geminiTrustWarning builds a non-fatal warning for trust folder operations.
func geminiTrustWarning(msg string) *warnings.Warning {
	return &warnings.Warning{
		Code:              warnings.CodeGeminiTrustFolderFailed,
		Subject:           geminiTrustFile,
		Message:           msg,
		Fix:               messages.SyncGeminiTrustFix,
		Source:            warnings.SourceInternal,
		Severity:          warnings.SeverityWarning,
		NoiseSuppressible: true,
	}
}
