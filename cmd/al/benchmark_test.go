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
	originalExec, originalLookPath, originalReadVersion := benchmarkExec, benchmarkLookPath, benchmarkReadVersion
	t.Cleanup(func() {
		benchmarkExec, benchmarkLookPath, benchmarkReadVersion = originalExec, originalLookPath, originalReadVersion
	})
	benchmarkReadVersion = func(context.Context, string) (string, error) { return Version, nil }
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
	originalVersion := Version
	Version = "v1.1.0"
	t.Cleanup(func() { Version = originalVersion })
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
	body := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo dev; exit 0; fi\nprintf '%s\\n' \"$PWD\" \"$*\"\ncat\nprintf 'companion diagnostic\\n' >&2\nexit 23\n"
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

func TestBenchmarkRequiresMatchingActiveVersion(t *testing.T) {
	originalVersion, originalExec, originalLookPath := Version, benchmarkExec, benchmarkLookPath
	t.Cleanup(func() { Version, benchmarkExec, benchmarkLookPath = originalVersion, originalExec, originalLookPath })
	Version = "v1.1.0"
	for _, test := range []struct {
		name, response string
		match          bool
	}{
		{"matching", "echo v1.1.0", true},
		{"normalized", "echo 1.1.0", true},
		{"stale", "echo v1.0.0", false},
		{"development", "echo dev", false},
		{"malformed", "echo 'v1.1.0 unexpected text'", false},
		{"failed probe", "exit 2", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "al-benchmark")
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+test.response+"\n"), 0o700); err != nil { // #nosec G306 -- executable version-only test fixture.
				t.Fatal(err)
			}
			benchmarkLookPath = func(string) (string, error) { return path, nil }
			called := false
			benchmarkExec = func(string, []string, []string) error { called = true; return nil }
			command := newRootCmd()
			command.SetArgs([]string{"benchmark", "run", "study.toml"})
			err := command.Execute()
			if called != test.match || (err == nil) != test.match {
				t.Fatalf("handoff = %v, error = %v, want match %v", called, err, test.match)
			}
			if !test.match && test.name != "failed probe" && !strings.Contains(err.Error(), "@v1.1.0") {
				t.Fatalf("missing matching-version installation guidance: %v", err)
			}
		})
	}
}
