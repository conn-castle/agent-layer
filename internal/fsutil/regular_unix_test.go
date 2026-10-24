//go:build unix

package fsutil

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestReadRegularFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "document")
	if err := os.WriteFile(path, []byte("document\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{path, link} {
		data, err := ReadRegularFile(path)
		if err != nil || string(data) != "document\n" {
			t.Fatalf("read %s = %q, %v", path, data, err)
		}
	}
	if _, err := ReadRegularFile(filepath.Join(root, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing error = %v", err)
	}
}

func TestReadRegularFileRefusesSpecialFiles(t *testing.T) {
	for _, kind := range []string{"fifo", "linked-fifo", "directory", "socket"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "document")
			var fifo string
			switch kind {
			case "fifo", "linked-fifo":
				fifo = testutil.MakeFIFO(t, path, kind == "linked-fifo")
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "socket":
				// Keep the socket path within macOS's sockaddr_un limit.
				root, err := os.MkdirTemp("", "al-sock-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(root) })
				path = filepath.Join(root, "socket")
				listener, err := net.Listen("unix", path)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = listener.Close() }()
			}
			read := func() error { _, err := ReadRegularFile(path); return err }
			var err error
			if fifo != "" {
				err = testutil.BoundedFIFO(t, fifo, read)
			} else {
				err = read()
			}
			if !errors.Is(err, ErrNotRegularFile) || !strings.Contains(err.Error(), path) {
				t.Fatalf("error = %v, want non-regular path %s", err, path)
			}
		})
	}
}
