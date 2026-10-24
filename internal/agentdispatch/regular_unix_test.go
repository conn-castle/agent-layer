//go:build unix

package agentdispatch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestDispatchMetadataRefusesFIFO(t *testing.T) {
	for _, kind := range []string{"invocation", "handle"} {
		for _, linked := range []bool{false, true} {
			name := kind + "/direct"
			if linked {
				name = kind + "/linked"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				run, err := newReservedDispatchRun(root, time.Now().Add(time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				session, err := reserveSession(root, run, testDispatchSessionRetention)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(run.Dir, dispatchRunFile)
				request := InspectRequest{Root: root, InvocationID: run.Record.ID}
				if kind == "handle" {
					path, err = sessionPath(root, session.Name)
					if err != nil {
						t.Fatal(err)
					}
					request = InspectRequest{Root: root, Handle: session.Name}
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				fifo := testutil.MakeFIFO(t, path, linked)
				for _, read := range []func() error{
					func() error { var target any; return readJSON(path, &target) },
					func() error { return Inspect(request) },
				} {
					err := testutil.BoundedFIFO(t, fifo, read)
					if !errors.Is(err, fsutil.ErrNotRegularFile) || !strings.Contains(err.Error(), path) {
						t.Fatalf("error = %v, want non-regular path %s", err, path)
					}
				}
				err = testutil.BoundedFIFO(t, fifo, func() error { return pruneDispatchEvidence(root, time.Now(), testDispatchSessionRetention) })
				if kind == "invocation" {
					if err != nil {
						t.Fatalf("historical invocation should be skipped: %v", err)
					}
				} else if !errors.Is(err, fsutil.ErrNotRegularFile) {
					t.Fatalf("corrupt mapping should fail retention: %v", err)
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
					t.Fatalf("metadata FIFO changed: %v, %v", info, err)
				}
			})
		}
	}
}

func TestDispatchMetadataFollowsRegularSymlinks(t *testing.T) {
	root := t.TempDir()
	run, err := newReservedDispatchRun(root, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run, testDispatchSessionRetention)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := sessionPath(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(run.Dir, dispatchRunFile), mapping} {
		if err := os.Rename(path, path+".target"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path+".target", path); err != nil {
			t.Fatal(err)
		}
	}
	var record RunRecord
	if err := readJSON(filepath.Join(run.Dir, dispatchRunFile), &record); err != nil {
		t.Fatal(err)
	}
	if record.ID != run.Record.ID {
		t.Fatalf("read invocation %s, want %s", record.ID, run.Record.ID)
	}
	for _, request := range []InspectRequest{{Root: root, InvocationID: record.ID}, {Root: root, Handle: session.Name}} {
		if err := Inspect(request); err != nil {
			t.Fatalf("inspect regular link: %v", err)
		}
	}
}
