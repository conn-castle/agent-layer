//go:build unix

package herdr

import (
	"errors"
	"os"
	"syscall"
)

func isProcessNotFoundError(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}
