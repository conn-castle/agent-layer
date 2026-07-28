package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// canonicalJSON uses encoding/json's deterministic map-key ordering and
// compact encoding. Benchmark identity structs intentionally avoid interfaces
// and maps with non-string keys, so this is stable across supported Go builds.
func canonicalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("canonical JSON: %w", err)
	}
	return data, nil
}

func hashCanonical(value any) (string, error) {
	data, err := canonicalJSON(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
