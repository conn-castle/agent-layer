//go:build linux

package ownedprocess

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

// TestParseProcStat handles parenthesized command names and rejects partial or
// malformed records without confusing fields inside comm for process metadata.
func TestParseProcStat(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		state         byte
		ok            bool
	}{
		{"spaces and parentheses", "123 (a b) c) S 1 456 0 0", 'S', true},
		{"zombie", "123 (git) Z 1 456", 'Z', true},
		{"dead", "123 (git) X 1 456", 'X', true},
		{"lowercase dead", "123 (git) x 1 456", 'x', true},
		{"empty", "", 0, false},
		{"missing open", "123 git) S 1 456", 0, false},
		{"missing close", "123 (git S 1 456", 0, false},
		{"missing pid", "(git) S 1 456", 0, false},
		{"bad pid", "abc (git) S 1 456", 0, false},
		{"signed pid", "+123 (git) S 1 456", 0, false},
		{"short", "123 (git) S 1", 0, false},
		{"bad state", "123 (git) SS 1 456", 0, false},
		{"bad parent", "123 (git) S nope 456", 0, false},
		{"bad group", "123 (git) S 1 nope", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pid, state, group, ok := parseProcStat(tc.content)
			if ok != tc.ok || (ok && (pid != 123 || state != tc.state || group != 456)) {
				t.Fatalf("parseProcStat(%q) = %d, %c, %d, %v", tc.content, pid, state, group, ok)
			}
		})
	}
}

// TestProcProcessLive covers a zombie main thread with a live thread, stopped
// threads, vanished tasks, and unobservable relevant tasks using a fake /proc.
func TestProcProcessLive(t *testing.T) {
	for _, tc := range []struct {
		name, threadState string
		live, wantErr     bool
	}{
		{"live thread", "S", true, false},
		{"zombie thread", "Z", false, false},
		{"dead thread", "X", false, false},
		{"lowercase dead thread", "x", false, false},
		{"malformed thread", "bad", false, true},
		{"vanished task", "", false, false},
		{"unreadable task directory", "unreadable", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.threadState == "unreadable" {
				writeProcFixture(t, root, "123/task", "not a directory")
			} else if tc.threadState != "" {
				writeProcFixture(t, root, "123/task/124/stat", "124 (worker) "+tc.threadState+" 1 456")
				if err := os.MkdirAll(filepath.Join(root, "123/task/125"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			live, err := procProcessLive(root, 123, 'Z')
			if live != tc.live || (err != nil) != tc.wantErr {
				t.Fatalf("zombie leader liveness=%v err=%v", live, err)
			}
		})
	}
	for _, state := range []byte{'S', 'Z', 'X', 'x'} {
		live, err := procProcessLive(t.TempDir(), 123, state)
		if err != nil || live != (state == 'S') {
			t.Fatalf("state %c: live=%v err=%v", state, live, err)
		}
	}
}

// TestScanProcConservativeErrors uses real Getpgid results to distinguish an
// unreadable owned process from unrelated or already vanished processes.
func TestScanProcConservativeErrors(t *testing.T) {
	pid := os.Getpid()
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatal(err)
	}
	const absentPID = 2147483647
	if _, err := syscall.Getpgid(absentPID); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("fixture PID must be absent: %v", err)
	}
	for _, tc := range []struct {
		name, content string
		pid, group    int
		wantErr       bool
	}{
		{"vanished stat", "", absentPID, pgid, false},
		{"vanished relevant stat", "", pid, pgid, false},
		{"malformed vanished", "bad", absentPID, pgid, false},
		{"malformed unrelated", "bad", pid, absentPID, false},
		{"malformed group member", "bad", pid, pgid, true},
		{"malformed moved leader", "bad", pid, pid, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := strconv.Itoa(tc.pid)
			if tc.content == "" {
				if err := os.Mkdir(filepath.Join(root, path), 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				writeProcFixture(t, root, path+"/stat", tc.content)
			}
			leader, group, err := scanProc(root, tc.group)
			if leader || group || (err != nil) != tc.wantErr {
				t.Fatalf("scanProc: leader=%v group=%v err=%v", leader, group, err)
			}
		})
	}
	_, _, err = scanProc(filepath.Join(t.TempDir(), "missing"), pgid)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing /proc error lost: %v", err)
	}
}

// TestScanProcLiveness checks group membership independently of leader identity,
// including a zombie leader whose remaining thread still holds resources.
func TestScanProcLiveness(t *testing.T) {
	pid := os.Getpid()
	for _, state := range []byte{'S', 'Z', 'X', 'x'} {
		t.Run(string(state), func(t *testing.T) {
			root := t.TempDir()
			writeProcFixture(t, root, strconv.Itoa(pid)+"/stat", fmt.Sprintf("%d (leader) %c 1 %d", pid, state, pid))
			writeProcFixture(t, root, "self/stat", "unrelated")
			leader, group, err := scanProc(root, pid)
			if err != nil || leader != (state == 'S') || group != leader {
				t.Fatalf("scanProc: leader=%v group=%v err=%v", leader, group, err)
			}
		})
	}
	root := t.TempDir()
	writeProcFixture(t, root, strconv.Itoa(pid)+"/stat", fmt.Sprintf("%d (leader) Z 1 %d", pid, pid))
	writeProcFixture(t, root, strconv.Itoa(pid)+"/task/124/stat", fmt.Sprintf("124 (thread) S 1 %d", pid))
	leader, group, err := scanProc(root, pid)
	if err != nil || !leader || !group {
		t.Fatalf("live zombie leader thread lost: leader=%v group=%v err=%v", leader, group, err)
	}
	writeProcFixture(t, root, strconv.Itoa(pid)+"/stat", fmt.Sprintf("%d (moved leader) S 1 0", pid))
	leader, group, err = scanProc(root, pid)
	if err != nil || !leader || group {
		t.Fatalf("moved leader: leader=%v group=%v err=%v", leader, group, err)
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatal(err)
	}
	writeProcFixture(t, root, strconv.Itoa(pid)+"/stat", fmt.Sprintf("%d (member) S 1 %d", pid, pgid))
	leader, group, err = scanProc(root, pgid)
	if err != nil || leader != (pid == pgid) || !group {
		t.Fatalf("group member: leader=%v group=%v err=%v", leader, group, err)
	}
}

// writeProcFixture builds only the stat files needed for an observation case.
func writeProcFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
