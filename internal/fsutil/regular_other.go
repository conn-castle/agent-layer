//go:build !unix

package fsutil

import "os"

func openRegularFile(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := RequireRegularFile(info, path); err != nil {
		return nil, err
	}
	return os.Open(path) // #nosec G304 -- caller supplies the document path; its resolved kind was checked above.
}
