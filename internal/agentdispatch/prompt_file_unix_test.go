//go:build unix

package agentdispatch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestPromptFileRegularLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt")
	want := bytes.Repeat([]byte("p"), MaxStdinPromptBytes)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readPromptFile(context.Background(), path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("exact-limit prompt: bytes=%d, error=%v", len(got), err)
	}
	const oversize = 64 * 1024 * 1024
	if err := os.Truncate(path, oversize); err != nil {
		t.Fatal(err)
	}
	_, err = readPromptFile(context.Background(), path)
	assertPromptFileExit(t, err, ExitUsage, fmt.Sprintf("dispatch prompt is %d bytes; the maximum is %d bytes", oversize, MaxStdinPromptBytes))
}

func TestPromptFileFIFOContents(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int
		linked bool
	}{
		{"delayed-symlink", 192 * 1024, true},
		{"exact-limit", MaxStdinPromptBytes, false},
		{"empty", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "prompt")
			fifo := testutil.MakeFIFO(t, path, tc.linked)
			want := bytes.Repeat([]byte("p"), tc.size)
			writer := promptFileWriter(t, fifo, func(fd int) error {
				if len(want) == 0 {
					return nil
				}
				first := min(4096, len(want))
				if err := writePromptFileBytes(fd, want[:first]); err != nil {
					return err
				}
				time.Sleep(20 * time.Millisecond)
				return writePromptFileBytes(fd, want[first:])
			})
			defer joinPromptFileWriter(t, writer)
			var got string
			err := testutil.BoundedFIFO(t, fifo, func() error {
				var err error
				got, err = resolvePromptSource(context.Background(), "", path)
				return err
			})
			if tc.size == 0 {
				assertPromptFileExit(t, err, ExitUsage, "dispatch prompt file is empty or whitespace")
			} else if err != nil || got != string(want) {
				t.Fatalf("FIFO prompt: bytes=%d, error=%v", len(got), err)
			}
		})
	}
}

func TestPromptFileFIFOWriterAlreadyWaiting(t *testing.T) {
	fifo := testutil.MakeFIFO(t, filepath.Join(t.TempDir(), "prompt"), false)
	done := make(chan error, 1)
	go func() {
		file, err := os.OpenFile(fifo, os.O_WRONLY, 0) // #nosec G304 -- test-controlled FIFO path.
		if err == nil {
			_, err = file.Write([]byte("waiting writer"))
			_ = file.Close()
		}
		done <- err
	}()
	// Release a regressed blocking writer even if the reader fails before opening.
	t.Cleanup(func() {
		fd, err := unix.Open(fifo, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			defer func() { _ = unix.Close(fd) }()
		}
		joinPromptFileWriter(t, done)
	})
	time.Sleep(30 * time.Millisecond)
	select {
	case err := <-done:
		done <- err
		t.Fatalf("writer returned before reader opened: %v", err)
	default:
	}
	var got []byte
	err := testutil.BoundedFIFO(t, fifo, func() error {
		var err error
		got, err = readPromptFile(context.Background(), fifo)
		return err
	})
	if err != nil || string(got) != "waiting writer" {
		t.Fatalf("waiting writer: %q, %v", got, err)
	}
}

func TestPromptFileFIFOOverLimitWithoutEOF(t *testing.T) {
	fifo := testutil.MakeFIFO(t, filepath.Join(t.TempDir(), "prompt"), false)
	release := make(chan struct{})
	writer := promptFileWriter(t, fifo, func(fd int) error {
		err := writePromptFileBytes(fd, bytes.Repeat([]byte("p"), MaxStdinPromptBytes+64*1024))
		select {
		case <-release:
		case <-time.After(4 * time.Second):
			return errors.New("oversize writer was not released")
		}
		return err
	})
	defer func() {
		close(release)
		joinPromptFileWriter(t, writer)
	}()
	err := testutil.BoundedFIFO(t, fifo, func() error {
		_, err := readPromptFile(context.Background(), fifo)
		return err
	})
	assertPromptFileExit(t, err, ExitUsage, fmt.Sprintf("dispatch prompt file exceeds the maximum of %d bytes", MaxStdinPromptBytes))
}

func TestPromptFileFIFOCancellation(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o400} {
		t.Run(fmt.Sprintf("mode-%o", mode), func(t *testing.T) {
			fifo := testutil.MakeFIFO(t, filepath.Join(t.TempDir(), "prompt"), false)
			if err := os.Chmod(fifo, mode); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timer := time.AfterFunc(150*time.Millisecond, cancel)
			defer timer.Stop()
			started := time.Now()
			err := testutil.BoundedFIFO(t, fifo, func() error {
				_, err := readPromptFile(ctx, fifo)
				return err
			})
			assertPromptFileExit(t, err, ExitUnavailable, "dispatch prompt preparation was cancelled")
			if time.Since(started) > time.Second {
				t.Fatal("prompt cancellation took more than 1s")
			}
			// Temporarily permit the probe on the read-only fixture; cancellation
			// itself ran without write permission.
			if err := os.Chmod(fifo, 0o600); err != nil {
				t.Fatal(err)
			}
			fd, err := unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
			if err == nil {
				_ = unix.Close(fd)
			}
			if !errors.Is(err, unix.ENXIO) {
				t.Fatalf("first writer probe = %v, want ENXIO (no reader)", err)
			}
		})
	}
}

func TestPromptFileFIFOCancellationBeforeEOF(t *testing.T) {
	fifo := testutil.MakeFIFO(t, filepath.Join(t.TempDir(), "prompt"), false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connected := make(chan int, 1)
	release := make(chan struct{})
	writer := promptFileWriter(t, fifo, func(fd int) error {
		if err := writePromptFileBytes(fd, []byte("partial prompt")); err != nil {
			return err
		}
		connected <- fd
		select {
		case <-release:
			return nil
		case <-time.After(4 * time.Second):
			return errors.New("connected writer was not released")
		}
	})
	defer joinPromptFileWriter(t, writer)
	defer close(release)
	done := make(chan error, 1)
	go func() {
		_, err := readPromptFile(ctx, fifo)
		done <- err
	}()
	var fd int
	select {
	case fd = <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not connect")
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		assertPromptFileExit(t, err, ExitUnavailable, "dispatch prompt preparation was cancelled")
	case <-time.After(time.Second):
		t.Fatal("reader did not cancel before EOF")
	}
	if _, err := unix.Write(fd, []byte("probe")); !errors.Is(err, unix.EPIPE) {
		t.Fatalf("write after cancellation = %v, want EPIPE", err)
	}
}

func TestPromptFilePreservesDescriptorFlags(t *testing.T) {
	for _, nonblock := range []bool{false, true} {
		t.Run(fmt.Sprintf("nonblock-%t", nonblock), func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reader.Close() }()
			fd := int(reader.Fd())
			if err := unix.SetNonblock(fd, nonblock); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, err := writer.Write([]byte("pipe prompt"))
				_ = writer.Close()
				done <- err
			}()
			defer joinPromptFileWriter(t, done)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				got, err := readPromptFile(ctx, fmt.Sprintf("/dev/fd/%d", fd))
				if err == nil && string(got) != "pipe prompt" {
					err = fmt.Errorf("pipe prompt = %q", got)
				}
				result <- err
			}()
			select {
			case err := <-result:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("descriptor read blocked")
			}
			flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
			if err != nil || (flags&unix.O_NONBLOCK != 0) != nonblock {
				t.Fatalf("original descriptor flags=%d, error=%v, want nonblock=%t", flags, err, nonblock)
			}
		})
	}
}

func TestPromptFileCancellationCreatesNoInvocation(t *testing.T) {
	for _, continuation := range []bool{false, true} {
		t.Run(fmt.Sprintf("continue-%t", continuation), func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			var session Session
			if continuation {
				_, session = terminalConversationForAsyncTest(t, root)
			}
			before, err := listSessions(root)
			if err != nil {
				t.Fatal(err)
			}
			runsPath := filepath.Join(root, ".agent-layer", "tmp", "runs")
			runsBefore, err := os.ReadDir(runsPath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			fifo := testutil.MakeFIFO(t, filepath.Join(t.TempDir(), "prompt"), false)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timer := time.AfterFunc(150*time.Millisecond, cancel)
			defer timer.Stop()
			launched := false
			launcher := func(string, string, string) (launchedWorker, error) {
				launched = true
				return launchedWorker{}, errors.New("unexpected worker launch")
			}
			err = testutil.BoundedFIFO(t, fifo, func() error {
				if continuation {
					return Continue(ContinueOptions{Context: ctx, Root: root, WorkDir: root, Handle: session.Name, PromptFile: fifo, launchWorker: launcher})
				}
				return Start(StartOptions{Context: ctx, Root: root, WorkDir: root, Agent: AgentCodex, PromptFile: fifo, launchWorker: launcher})
			})
			assertPromptFileExit(t, err, ExitUnavailable, "dispatch prompt preparation was cancelled")
			after, err := listSessions(root)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("sessions changed: before=%#v after=%#v error=%v", before, after, err)
			}
			runsAfter, err := os.ReadDir(runsPath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if len(runsAfter) != len(runsBefore) || launched {
				t.Fatalf("cancelled preparation created invocation: runs %d -> %d, launched=%t", len(runsBefore), len(runsAfter), launched)
			}
		})
	}
}

func assertPromptFileExit(t *testing.T, err error, code int, message string) {
	t.Helper()
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != code || !strings.Contains(err.Error(), message) {
		t.Fatalf("error = %v, want exit %d containing %q", err, code, message)
	}
}

// promptFileWriter delays connection and bounds it so reader regressions cannot
// leave a writer stuck in open. Writes then block; the reader always closes its
// end on return, which fails a pending write with EPIPE.
func promptFileWriter(t *testing.T, fifo string, write func(int) error) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			fd, err := unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
			if errors.Is(err, unix.ENXIO) || errors.Is(err, unix.EINTR) {
				time.Sleep(time.Millisecond)
				continue
			}
			if err == nil {
				if err = unix.SetNonblock(fd, false); err == nil {
					err = write(fd)
				}
				_ = unix.Close(fd)
			}
			done <- err
			return
		}
		done <- errors.New("FIFO writer could not connect within 3s")
	}()
	return done
}

func writePromptFileBytes(fd int, data []byte) error {
	for len(data) > 0 {
		n, err := unix.Write(fd, data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil && !errors.Is(err, unix.EINTR) {
			return err
		}
	}
	return nil
}

func joinPromptFileWriter(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, unix.EPIPE) {
			t.Errorf("FIFO writer: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Error("FIFO writer did not exit within 4s")
	}
}
