package agentdispatch

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// workerCommand is the hidden `al` subcommand that launchDetachedWorker runs.
const workerCommand = "__dispatch-worker"

// TestMain makes the test binary a faithful stand-in for `al` and keeps the
// package hermetic. Under `go test`, launchDetachedWorker re-executes this
// binary as the detached worker, so the worker command must run the real
// worker instead of the whole suite. Provider CLIs installed on the developer's
// PATH are hidden so no test reaches a real provider, and the suite fails if
// any worker it launched is still running when it ends.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == workerCommand {
		os.Exit(runTestWorker(os.Args[2:]))
	}
	path, shadowRoot, err := hideProviders(os.Getenv("PATH"))
	if err == nil {
		err = os.Setenv("PATH", path)
	}
	code := 1
	if err == nil {
		code = m.Run()
	}
	if err == nil {
		err = reapDetachedWorkers(5 * time.Second)
	}
	if shadowRoot != "" {
		err = errors.Join(err, os.RemoveAll(shadowRoot)) // #nosec G703 -- shadowRoot is the temporary directory this TestMain created.
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

// runTestWorker mirrors cmd/al's hidden worker command.
func runTestWorker(args []string) int {
	flags := flag.NewFlagSet(workerCommand, flag.ContinueOnError)
	root := flags.String("root", "", "")
	runID := flags.String("run", "", "")
	if err := flags.Parse(args); err != nil || *root == "" || *runID == "" {
		fmt.Fprintln(os.Stderr, "dispatch worker requires --root and --run")
		return 2
	}
	gate := os.NewFile(3, "dispatch-worker-gate")
	if gate == nil {
		fmt.Fprintln(os.Stderr, "dispatch worker gate is unavailable")
		return 1
	}
	defer func() { _ = gate.Close() }()
	if err := RunWorker(*root, *runID, gate); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// hideProviders replaces every PATH entry that contains a dispatch provider
// binary with a shadow directory linking everything else in it, so system
// tools that share a directory with a provider stay reachable. Tests install
// stub providers in their own directories instead.
func hideProviders(path string) (string, string, error) {
	providers := map[string]bool{}
	for _, target := range targetRegistry() {
		providers[target.Binary] = true
	}
	shadowRoot := ""
	dirs := filepath.SplitList(path)
	for i, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil || !slices.ContainsFunc(entries, func(entry os.DirEntry) bool { return providers[entry.Name()] }) {
			continue
		}
		if shadowRoot == "" {
			if shadowRoot, err = os.MkdirTemp("", "agentdispatch-path-"); err != nil {
				return "", "", err
			}
		}
		source, err := filepath.Abs(dir)
		if err != nil {
			return "", shadowRoot, err
		}
		shadow := filepath.Join(shadowRoot, strconv.Itoa(i))
		if err := os.Mkdir(shadow, 0o700); err != nil {
			return "", shadowRoot, err
		}
		for _, entry := range entries {
			if !providers[entry.Name()] {
				if err := os.Symlink(filepath.Join(source, entry.Name()), filepath.Join(shadow, entry.Name())); err != nil {
					return "", shadowRoot, err
				}
			}
		}
		dirs[i] = shadow
	}
	return strings.Join(dirs, string(os.PathListSeparator)), shadowRoot, nil
}

// reapDetachedWorkers reaps released children, such as detached workers, that
// already exited and reports any child still running after the grace period.
// A released process stays a child of the test binary until it is reaped.
func reapDetachedWorkers(grace time.Duration) error {
	deadline := time.Now().Add(grace)
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		switch {
		case errors.Is(err, syscall.ECHILD):
			return nil
		case errors.Is(err, syscall.EINTR):
			continue
		case err != nil:
			return fmt.Errorf("reap dispatch workers: %w", err)
		case pid > 0:
			continue
		case time.Now().After(deadline):
			return errors.New("a child process, such as a detached dispatch worker, outlived the test suite; a test must wait for every process it starts")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
