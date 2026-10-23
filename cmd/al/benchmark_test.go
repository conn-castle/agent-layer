package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBenchmarkCompanionHandoff(t *testing.T) {
	originalExec, originalLookPath := benchmarkExec, benchmarkLookPath
	t.Cleanup(func() { benchmarkExec, benchmarkLookPath = originalExec, originalLookPath })
	benchmarkLookPath = func(name string) (string, error) {
		if name != "al-benchmark" {
			t.Fatalf("unexpected executable: %s", name)
		}
		return "/optional/al-benchmark", nil
	}
	want := errors.New("handoff result")
	benchmarkExec = func(path string, args, env []string) error {
		if path != "/optional/al-benchmark" || strings.Join(args, "|") != "al-benchmark|run|study with spaces.toml|--recover-only|--task|task-a" {
			t.Fatalf("handoff: %s %q", path, args)
		}
		if strings.Join(env, "\n") != strings.Join(os.Environ(), "\n") {
			t.Fatal("handoff changed the environment")
		}
		return want
	}
	command := newRootCmd()
	command.SetArgs([]string{"benchmark", "run", "study with spaces.toml", "--recover-only", "--task", "task-a"})
	if err := command.Execute(); !errors.Is(err, want) {
		t.Fatalf("Execute() = %v", err)
	}
}

func TestBenchmarkMissingCompanionIsActionable(t *testing.T) {
	original := benchmarkLookPath
	t.Cleanup(func() { benchmarkLookPath = original })
	benchmarkLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	command := newRootCmd()
	command.SetArgs([]string{"benchmark", "--help"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "go install") || !strings.Contains(err.Error(), "optional al-benchmark") {
		t.Fatalf("Execute() = %v", err)
	}
}

func TestBenchmarkCanceledBeforeHandoff(t *testing.T) {
	original := benchmarkLookPath
	t.Cleanup(func() { benchmarkLookPath = original })
	benchmarkLookPath = func(string) (string, error) { return "/optional/al-benchmark", nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := newRootCmd()
	command.SetArgs([]string{"benchmark", "run", "study.toml"})
	if err := command.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteContext() = %v", err)
	}
}

func TestBenchmarkHandoffPreservesProcessIOAndExit(t *testing.T) {
	if os.Getenv("AL_TEST_BENCHMARK_HANDOFF") == "1" {
		command := newRootCmd()
		command.SetArgs([]string{"benchmark", "run", "study with spaces.toml", "--recover-only"})
		if err := command.Execute(); err != nil {
			panic(err)
		}
		os.Exit(0)
	}
	bin := t.TempDir()
	body := "#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$*\"\ncat\nprintf 'companion diagnostic\\n' >&2\nexit 23\n"
	if err := os.WriteFile(filepath.Join(bin, "al-benchmark"), []byte(body), 0o700); err != nil { // #nosec G306 -- test-owned executable companion stub.
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestBenchmarkHandoffPreservesProcessIOAndExit$")
	command.Dir = bin
	command.Env = append(os.Environ(), "AL_TEST_BENCHMARK_HANDOFF=1", "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	command.Stdin = strings.NewReader("caller input\n")
	output, err := command.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
		t.Fatalf("handoff exit = %v; output = %s", err, output)
	}
	for _, want := range []string{bin, "run study with spaces.toml --recover-only", "caller input", "companion diagnostic"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("handoff output %q lacks %q", output, want)
		}
	}
}
