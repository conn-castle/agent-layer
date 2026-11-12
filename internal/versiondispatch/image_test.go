package versiondispatch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/imagelease"
	"github.com/conn-castle/agent-layer/internal/messages"
)

type imageHandoffFixture struct {
	root, cache, source string
	env                 map[string]string
	stderr              bytes.Buffer
	execPath            string
	execArgs, execEnv   []string
	execErr             error
	running             string
	runningErr          error
}

func newImageHandoffFixture(t *testing.T, pin string) *imageHandoffFixture {
	t.Helper()
	f := &imageHandoffFixture{root: t.TempDir(), cache: t.TempDir(), env: map[string]string{}}
	if err := os.MkdirAll(filepath.Join(f.root, ".agent-layer"), 0o700); err != nil {
		t.Fatal(err)
	}
	if pin != "" {
		if err := os.WriteFile(filepath.Join(f.root, ".agent-layer", "al.version"), []byte(pin+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.source = filepath.Join(t.TempDir(), "al")
	if err := os.WriteFile(f.source, []byte("running image"), 0o700); err != nil { // #nosec G306 -- synthetic executable.
		t.Fatal(err)
	}
	f.running = f.source
	f.env[EnvCacheDir] = f.cache
	return f
}

func (f *imageHandoffFixture) system() *testSystem {
	return &testSystem{
		GetenvFunc:  func(key string) string { return f.env[key] },
		EnvironFunc: func() []string { return []string{"KEEP=1"} },
		StderrFunc:  func() io.Writer { return &f.stderr },
		ExecBinaryFunc: func(path string, args []string, env []string) error {
			f.execPath, f.execArgs, f.execEnv = path, args, env
			return f.execErr
		},
	}
}

func (f *imageHandoffFixture) run(t *testing.T, current string) error {
	t.Helper()
	return maybeExecWithOptions(context.Background(), f.system(), []string{"/install/bin/al", "dispatch", "mcp-server"}, current, f.root, execOptions{
		runningImage: func() (string, string, error) { return f.source, f.running, f.runningErr },
	})
}

func (f *imageHandoffFixture) imagePath(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.source)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(f.cache, imagesDirName, fmt.Sprintf("%x", sha256.Sum256(data)), imageName)
}

func TestImageHandoffForPinAndOverride(t *testing.T) {
	for _, tc := range []struct{ name, pin, override string }{
		{name: "pin", pin: "1.2.3"},
		{name: "override", override: "1.2.3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newImageHandoffFixture(t, tc.pin)
			f.env[EnvVersionOverride] = tc.override
			if err := f.run(t, "v1.2.3"); !errors.Is(err, ErrDispatched) {
				t.Fatalf("expected handoff, got %v", err)
			}
			want := f.imagePath(t)
			if f.execPath != want || !slices.Equal(f.execArgs, []string{want, "dispatch", "mcp-server"}) {
				t.Fatalf("exec %s %v, want %s", f.execPath, f.execArgs, want)
			}
			if !slices.Contains(f.execEnv, EnvShimActive+"=1") || !slices.Contains(f.execEnv, EnvInstalledExecutable+"=/install/bin/al") || !slices.Contains(f.execEnv, "KEEP=1") {
				t.Fatalf("handoff env: %v", f.execEnv)
			}
			if data, err := os.ReadFile(want); err != nil || string(data) != "running image" { // #nosec G304 -- test cache path.
				t.Fatalf("image copy: %q %v", data, err)
			}
			if info, err := os.Stat(want); err != nil || info.Mode().Perm() != 0o755 {
				t.Fatalf("image mode: %v %v", info, err)
			}
			if _, err := os.Stat(filepath.Join(f.cache, "versions")); !os.IsNotExist(err) {
				t.Fatalf("handoff wrote the versions cache: %v", err)
			}
			if f.stderr.Len() != 0 {
				t.Fatalf("handoff printed before the image process: %q", f.stderr.String())
			}
		})
	}
}

func TestImageHandoffSkipped(t *testing.T) {
	for _, tc := range []struct {
		name, pin, current string
		setup              func(*imageHandoffFixture)
		plain              bool
	}{
		{name: "unpinned current selection", current: "1.2.3"},
		{name: "shim active", pin: "1.2.3", current: "1.2.3", setup: func(f *imageHandoffFixture) { f.env[EnvShimActive] = "1" }},
		{name: "already in image cache", pin: "1.2.3", current: "1.2.3", setup: func(f *imageHandoffFixture) {
			f.running = filepath.Join(f.cache, imagesDirName, "abc", imageName)
		}},
		{name: "other commands", pin: "1.2.3", current: "1.2.3", plain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newImageHandoffFixture(t, tc.pin)
			if tc.setup != nil {
				tc.setup(f)
			}
			var err error
			if tc.plain {
				err = maybeExec(context.Background(), f.system(), []string{"/install/bin/al", "dispatch", "start"}, tc.current, f.root)
			} else {
				err = f.run(t, tc.current)
			}
			if err != nil || f.execPath != "" {
				t.Fatalf("unexpected handoff to %q: %v", f.execPath, err)
			}
			if _, err := os.Stat(filepath.Join(f.cache, imagesDirName)); !os.IsNotExist(err) {
				t.Fatalf("skipped handoff wrote the image cache: %v", err)
			}
			if strings.Contains(f.stderr.String(), "immutable image") {
				t.Fatalf("skipped handoff printed a fallback: %q", f.stderr.String())
			}
		})
	}
}

func TestImageHandoffReusesOnlyVerifiedEntries(t *testing.T) {
	f := newImageHandoffFixture(t, "1.2.3")
	if err := f.run(t, "1.2.3"); !errors.Is(err, ErrDispatched) {
		t.Fatal(err)
	}
	path := f.imagePath(t)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.run(t, "1.2.3"); !errors.Is(err, ErrDispatched) {
		t.Fatal(err)
	}
	if after, err := os.Stat(path); err != nil || !os.SameFile(before, after) {
		t.Fatalf("verified entry was not reused: %v", err)
	}
	// A corrupted entry is replaced by a fresh verified copy, never executed.
	if err := os.WriteFile(path, []byte("tampered"), 0o700); err != nil { // #nosec G306 G703 -- test cache path.
		t.Fatal(err)
	}
	if err := f.run(t, "1.2.3"); !errors.Is(err, ErrDispatched) {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "running image" { // #nosec G304 -- test cache path.
		t.Fatalf("corrupted entry was reused: %q %v", data, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), imageName+".tmp-*")); len(matches) != 0 {
		t.Fatalf("temporary copies left behind: %v", matches)
	}
}

func TestImageHandoffFailureRunsInPlace(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*imageHandoffFixture, *testSystem)
	}{
		{name: "running image unavailable", setup: func(f *imageHandoffFixture, _ *testSystem) { f.runningErr = errors.New("no image") }},
		{name: "copy fails", setup: func(_ *imageHandoffFixture, sys *testSystem) {
			sys.CreateTempFunc = func(string, string) (*os.File, error) { return nil, errors.New("disk full") }
		}},
		{name: "rename fails", setup: func(_ *imageHandoffFixture, sys *testSystem) {
			sys.RenameFunc = func(string, string) error { return errors.New("read-only") }
		}},
		{name: "exec fails", setup: func(f *imageHandoffFixture, _ *testSystem) { f.execErr = errors.New("exec format error") }},
		{name: "cache root unresolved", setup: func(f *imageHandoffFixture, sys *testSystem) {
			delete(f.env, EnvCacheDir)
			sys.UserCacheDirFunc = func() (string, error) { return "", errors.New("no home") }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newImageHandoffFixture(t, "1.2.3")
			sys := f.system()
			tc.setup(f, sys)
			err := maybeExecWithOptions(context.Background(), sys, []string{"/install/bin/al", "dispatch", "mcp-server"}, "1.2.3", f.root, execOptions{
				runningImage: func() (string, string, error) { return f.source, f.running, f.runningErr },
			})
			if err != nil {
				t.Fatalf("fallback must run in place: %v", err)
			}
			out := f.stderr.String()
			if strings.Count(out, "immutable image copy") != 1 || !strings.Contains(out, "Agent Layer version source: 1.2.3 (pin)") {
				t.Fatalf("expected one fallback diagnostic then the version source: %q", out)
			}
			if matches, _ := filepath.Glob(filepath.Join(f.cache, imagesDirName, "*", imageName+".tmp-*")); len(matches) != 0 {
				t.Fatalf("failed handoff left temporary copies: %v", matches)
			}
			if _, err := os.Stat(f.imagePath(t)); tc.name != "exec fails" && !os.IsNotExist(err) {
				t.Fatalf("failed handoff published an image: %v", err)
			}
		})
	}
}

func TestImageHandoffCanceledDoesNotRunInPlace(t *testing.T) {
	f := newImageHandoffFixture(t, "1.2.3")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := maybeExecWithOptions(ctx, f.system(), []string{"/install/bin/al", "dispatch", "mcp-server"}, "1.2.3", f.root, execOptions{
		runningImage: func() (string, string, error) { return f.source, f.running, nil },
	})
	if !errors.Is(err, context.Canceled) || f.execPath != "" {
		t.Fatalf("canceled handoff: exec %q, err %v", f.execPath, err)
	}
}

func TestImageHandoffRequiresReleaseBuild(t *testing.T) {
	sys := &testSystem{GetenvFunc: func(string) string { return "" }}
	opts := execOptions{runningImage: runningImage}
	if wantsImageHandoff(sys, opts, sourcePin, "dev") || !wantsImageHandoff(sys, opts, sourcePin, "1.2.3") || wantsImageHandoff(sys, execOptions{}, sourcePin, "1.2.3") {
		t.Fatal("image handoff must require a release build and the MCP server option")
	}
}

func TestDevelopmentImageHandoff(t *testing.T) {
	for _, mode := range []string{"handoff", "already snapshot", "different image", "different root", "other command", "copy fails", "exec fails", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			f := newImageHandoffFixture(t, "0.22.0")
			f.env[EnvDevelopmentBypassVersionDispatch] = "1"
			f.env[EnvDevelopmentSourceRoot] = f.root
			f.env[EnvDevelopmentExecutable] = f.source
			f.env[EnvVersionOverride] = "1.2.3"
			env := []string{"KEEP=1", "PATH=/dev/bin:/usr/bin", EnvDevelopmentBypassVersionDispatch + "=1", EnvDevelopmentSourceRoot + "=" + f.root, EnvDevelopmentExecutable + "=" + f.source, EnvShimActive + "=inherited", EnvInstalledExecutable + "=/inherited/al"}
			sys := f.system()
			sys.EnvironFunc = func() []string { return env }
			opts := execOptions{
				runningImage: func() (string, string, error) { return f.source, f.running, f.runningErr },
				sameAs: func(path string) error {
					if path != f.source {
						t.Fatalf("identity check of %q", path)
					}
					if mode == "different image" {
						return errors.New("different image")
					}
					return nil
				},
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "already snapshot":
				f.running = f.imagePath(t)
			case "different root":
				f.env[EnvDevelopmentSourceRoot] = t.TempDir()
			case "other command":
				opts = execOptions{}
			case "copy fails":
				sys.CreateTempFunc = func(string, string) (*os.File, error) { return nil, errors.New("disk full") }
			case "exec fails":
				f.execErr = errors.New("exec failed")
			case "canceled":
				cancel()
			}
			current := "dev"
			args := []string{f.source, "dispatch", "mcp-server"}
			if mode == "different root" {
				current = "1.2.3"
			}
			if mode == "other command" {
				args[2] = "options"
			}
			err := maybeExecWithOptions(ctx, sys, args, current, f.root, opts)
			switch mode {
			case "handoff":
				if !errors.Is(err, ErrDispatched) || f.execPath != f.imagePath(t) {
					t.Fatalf("handoff: %q %v", f.execPath, err)
				}
				if !slices.Equal(withoutEnv(f.execEnv, imagelease.EnvFD), env) || len(f.execEnv) != len(env)+1 {
					t.Fatalf("environment changed: %v", f.execEnv)
				}
				// Simulate the exec'd process. Its image is no longer the selected
				// mutable dev entry, and the same inherited env must not loop.
				opts.sameAs = func(string) error { return errors.New("snapshot") }
				f.execPath = ""
				if err := maybeExecWithOptions(ctx, sys, f.execArgs, current, f.root, opts); err != nil || f.execPath != "" {
					t.Fatalf("second handoff: %q %v", f.execPath, err)
				}
			case "different root":
				// Normal release selection remains in force outside the dev root.
				if !errors.Is(err, ErrDispatched) || f.execPath != f.imagePath(t) {
					t.Fatalf("release handoff: %q %v", f.execPath, err)
				}
				if !slices.Contains(f.execEnv, EnvShimActive+"=1") {
					t.Fatalf("not release handoff: %v", f.execEnv)
				}
			case "copy fails", "exec fails":
				if err != nil || strings.Count(f.stderr.String(), "immutable image copy") != 1 {
					t.Fatalf("fallback: %v %s", err, f.stderr.String())
				}
				// Exec failure has a deterministic cause, so verify that dev mode
				// selects the dev-specific diagnostic rather than the release one.
				if mode == "exec fails" && f.stderr.String() != fmt.Sprintf(messages.DispatchMCPDevImageFallbackFmt, f.execErr) {
					t.Fatalf("wrong dev fallback diagnostic: %s", f.stderr.String())
				}
			case "canceled":
				if !errors.Is(err, context.Canceled) || f.execPath != "" {
					t.Fatalf("cancellation: %q %v", f.execPath, err)
				}
			default:
				if err != nil || f.execPath != "" {
					t.Fatalf("unexpected handoff: %q %v", f.execPath, err)
				}
			}
			if entries, _ := os.ReadDir(filepath.Join(f.cache, "versions")); len(entries) != 0 {
				t.Fatalf("wrote release versions: %v", entries)
			}
		})
	}
}
