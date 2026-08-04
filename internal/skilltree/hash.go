// Package skilltree owns the canonical Agent Skill tree hash encoding.
package skilltree

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
)

// HashPrefix versions the canonical encoding.
const HashPrefix = "sha256-v1:"

// File is one canonical regular file.
type File struct {
	Path       string
	Data       []byte
	Executable bool
}

// Hash returns the canonical digest over sorted paths, exact bytes, and the
// executable bit.
func Hash(files []File) string {
	sorted := append([]File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	digest := sha256.New()
	for _, file := range sorted {
		mode := "100644"
		if file.Executable {
			mode = "100755"
		}
		_, _ = digest.Write([]byte(strconv.Itoa(len(file.Path))))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(file.Path))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(mode))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(strconv.Itoa(len(file.Data))))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(file.Data)
	}
	return HashPrefix + hex.EncodeToString(digest.Sum(nil))
}
