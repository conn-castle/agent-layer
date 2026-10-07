//go:build unix

package herdr

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestProcessNotFoundErrorMapsENOENTAndESRCH(t *testing.T) {
	if !isProcessNotFoundError(os.ErrNotExist) {
		t.Fatal("missing path was not treated as a gone process")
	}
	if !isProcessNotFoundError(fmt.Errorf("read: %w", syscall.ESRCH)) {
		t.Fatal("ESRCH was not treated as a gone process")
	}
	if isProcessNotFoundError(syscall.EPERM) {
		t.Fatal("unrelated process error was treated as gone")
	}
}
