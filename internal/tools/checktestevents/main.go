//go:build tools
// +build tools

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// testEvent holds the go test -json fields needed to track package completion.
type testEvent struct {
	Action  string
	Package string
	Test    string
}

func main() {
	os.Exit(run(os.Args, os.Stderr))
}

// run checks the go test -json event file named by args[1].
// It returns 0 when every started package reported a result, 1 otherwise, and 2 on usage errors.
func run(args []string, errOut io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintf(errOut, messages.CheckTestEventsUsageFmt, args[0])
		return 2
	}

	file, err := os.Open(args[1])
	if err != nil {
		fmt.Fprintf(errOut, messages.CheckTestEventsReadFailedFmt, args[1], err)
		return 1
	}
	defer file.Close()

	unfinished, err := unfinishedPackages(file)
	if err != nil {
		fmt.Fprintf(errOut, messages.CheckTestEventsReadFailedFmt, args[1], err)
		return 1
	}
	if len(unfinished) > 0 {
		fmt.Fprintln(errOut, messages.CheckTestEventsUnfinishedHeader)
		for _, pkg := range unfinished {
			fmt.Fprintf(errOut, messages.CheckTestEventsUnfinishedPackageFmt, pkg)
		}
		return 1
	}
	return 0
}

// unfinishedPackages returns, sorted, the packages with a start event but no
// package-level pass, fail, or skip event. A test binary that is replaced or
// exits through a raw syscall leaves its package in this state while go test
// still exits 0.
func unfinishedPackages(r io.Reader) ([]string, error) {
	started := map[string]bool{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	line := 1
	for scanner.Scan() {
		var event testEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		line++
		if event.Test != "" {
			continue
		}
		switch event.Action {
		case "start":
			started[event.Package] = true
		case "pass", "fail", "skip":
			delete(started, event.Package)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("line %d: %w", line, err)
	}

	unfinished := make([]string, 0, len(started))
	for pkg := range started {
		unfinished = append(unfinished, pkg)
	}
	sort.Strings(unfinished)
	return unfinished, nil
}
