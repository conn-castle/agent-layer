package benchmark

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func promoteSanitizedPierArtifacts(request ExecutionRequest, stage string) error {
	var secrets [][]byte
	for _, path := range []string{
		filepath.Join(request.RepoRoot, ".codex", "auth.json"),
		filepath.Join(request.RepoRoot, ".claude-config", ".credentials.json"),
	} {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 { // #nosec G304 -- fixed repo-local credential paths.
			secrets = append(secrets, credentialSecretValues(data)...)
		}
	}
	paths := []string{request.RepoRoot}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, home)
	}
	if err := sanitizeArtifacts(stage, secrets, paths); err != nil {
		return err
	}
	destination, err := artifactDestination(request)
	if err != nil {
		return err
	}
	return copyRequiredTree(stage, destination)
}

func artifactDestination(request ExecutionRequest) (string, error) {
	if request.EventID == "" || request.EventID == "." || request.EventID == ".." ||
		filepath.Base(request.EventID) != request.EventID {
		return "", fmt.Errorf("invalid Pier artifact event ID")
	}
	evidenceRoot, err := filepath.Abs(request.EvidenceDir)
	if err != nil {
		return "", fmt.Errorf("resolve benchmark evidence directory: %w", err)
	}
	return filepath.Join(
		evidenceRoot, "attempts", fmt.Sprintf("%d", request.Attempt),
		"tasks", request.Task, "artifacts", request.EventID,
	), nil
}

func credentialSecretValues(data []byte) [][]byte {
	values := [][]byte{data}
	var decoded any
	if json.Unmarshal(data, &decoded) != nil {
		return values
	}
	var collect func(any, bool)
	collect = func(value any, secret bool) {
		switch typed := value.(type) {
		case string:
			if secret && typed != "" {
				values = append(values, []byte(typed))
			}
		case []any:
			for _, child := range typed {
				collect(child, secret)
			}
		case map[string]any:
			for key, child := range typed {
				collect(child, secretCredentialKey(key))
			}
		}
	}
	collect(decoded, false)
	return uniqueSecretValues(values)
}

func secretCredentialKey(key string) bool {
	var normalized strings.Builder
	for _, character := range strings.ToLower(key) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			normalized.WriteRune(character)
		}
	}
	value := normalized.String()
	return strings.HasSuffix(value, "token") ||
		strings.HasSuffix(value, "secret") ||
		strings.HasSuffix(value, "password") ||
		strings.HasSuffix(value, "apikey") ||
		value == "authorization" ||
		value == "cookie" ||
		value == "credential"
}

func uniqueSecretValues(values [][]byte) [][]byte {
	seen := make(map[string]struct{}, len(values))
	var unique [][]byte
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		key := string(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func sanitizeArtifacts(root string, secrets [][]byte, paths []string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G122,G304 -- regular file discovered below the restricted attempt stage.
		if err != nil {
			return err
		}
		for _, secret := range secrets {
			if len(secret) > 0 {
				data = bytes.ReplaceAll(data, secret, []byte("[REDACTED]"))
			}
		}
		for _, value := range paths {
			if value != "" {
				data = bytes.ReplaceAll(data, []byte(value), []byte("[PATH]"))
			}
		}
		for _, secret := range secrets {
			if len(secret) > 0 && bytes.Contains(data, secret) {
				return fmt.Errorf("credential bytes remain in staged artifact %s", path)
			}
		}
		return os.WriteFile(path, data, 0o600) // #nosec G122,G703 -- same regular file beneath the restricted stage.
	})
}
