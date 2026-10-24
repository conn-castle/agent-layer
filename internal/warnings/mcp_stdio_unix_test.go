//go:build darwin || linux

package warnings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/ownedprocess"
	"github.com/conn-castle/agent-layer/internal/projection"
)

const (
	stdioFixtureModeEnv    = "AL_TEST_MCP_STDIO_MODE"
	stdioFixturePIDEnv     = "AL_TEST_MCP_CHILD_PID_FILE"
	stdioFixtureReleaseEnv = "AL_TEST_MCP_CHILD_RELEASE_FILE"
)

// TestMCPStdioFixture is a stdio MCP server that leaves a child holding its
// output. The child ignores SIGINT and SIGTERM until its release file appears.
// In linger mode the server outlives end of input and records SIGTERM without
// exiting.
func TestMCPStdioFixture(t *testing.T) {
	mode := os.Getenv(stdioFixtureModeEnv)
	if mode == "" {
		t.Skip("stdio MCP fixture helper")
	}
	pidFile := os.Getenv(stdioFixturePIDEnv)
	terminated := make(chan os.Signal, 1)
	if mode == "linger" {
		signal.Notify(terminated, syscall.SIGTERM)
	}
	if mode == "child" {
		signal.Ignore(syscall.SIGINT, syscall.SIGTERM)
		if os.WriteFile(pidFile+".tmp", []byte(strconv.Itoa(os.Getpid())), 0o600) != nil || os.Rename(pidFile+".tmp", pidFile) != nil { // #nosec G703 -- test-owned PID path.
			os.Exit(2)
		}
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(os.Getenv(stdioFixtureReleaseEnv)); err == nil { // #nosec G703 -- test-owned release path.
				break
			}
		}
		os.Exit(0)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestMCPStdioFixture$") // #nosec G204 G702 -- re-exec this test binary as its owned child.
	child.Env = append(os.Environ(), stdioFixtureModeEnv+"=child")
	child.Stdout = os.Stdout
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, err := os.Stat(pidFile); err == nil { // #nosec G703 -- test-owned PID path.
			break
		}
		if time.Now().After(deadline) {
			os.Exit(4)
		}
	}
	if mode == "hang" {
		_, _ = io.Copy(io.Discard, os.Stdin)
	} else {
		server := mcp.NewServer(&mcp.Implementation{Name: "stdio-fixture", Version: "1"}, nil)
		server.AddTool(&mcp.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
		_ = server.Run(context.Background(), &mcp.StdioTransport{})
	}
	if mode == "linger" {
		if os.WriteFile(pidFile+".eof", nil, 0o600) != nil { // #nosec G703 -- test-owned marker path.
			os.Exit(5)
		}
		<-terminated
		if os.WriteFile(pidFile+".term", nil, 0o600) != nil { // #nosec G703 -- test-owned marker path.
			os.Exit(6)
		}
		time.Sleep(30 * time.Second)
	}
	os.Exit(0)
}

// stdioFixtureServer configures the fixture and releases its child on cleanup.
func stdioFixtureServer(t *testing.T, mode string) (projection.ResolvedMCPServer, string) {
	t.Helper()
	dir := t.TempDir()
	pidFile, release := filepath.Join(dir, "child"), filepath.Join(dir, "release")
	t.Cleanup(func() {
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			t.Error(err)
		}
		for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			pid, err := stdioFixturePID(pidFile)
			if err != nil || stdioFixtureStopped(pid) {
				return
			}
			if time.Now().After(deadline) {
				t.Error("fixture child did not stop after release")
				return
			}
		}
	})
	return projection.ResolvedMCPServer{
		ID: "owned-stdio", Transport: config.TransportStdio, Command: os.Args[0],
		Args: []string{"-test.run=^TestMCPStdioFixture$"},
		Env:  map[string]string{stdioFixtureModeEnv: mode, stdioFixturePIDEnv: pidFile, stdioFixtureReleaseEnv: release},
	}, pidFile
}

func TestMCPDiscoveryStopsStdioDescendants(t *testing.T) {
	for _, mode := range []string{"hang", "serve"} {
		t.Run(mode, func(t *testing.T) {
			server, pidFile := stdioFixtureServer(t, mode)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "hang" {
				go func() {
					for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
						if _, err := stdioFixturePID(pidFile); err == nil {
							break
						}
					}
					cancel()
				}()
			}
			result := make(chan DiscoveryResult, 1)
			go func() { result <- (&RealConnector{}).ConnectAndDiscover(ctx, server) }()
			var got DiscoveryResult
			select {
			case got = <-result:
			case <-time.After(5 * time.Second):
				t.Fatal("MCP discovery did not return")
			}
			assertStdioFixtureStopped(t, pidFile)
			// Cancellation may surface as the SDK's broken-pipe or EOF error.
			if mode == "hang" && got.Error == nil {
				t.Fatal("cancelled discovery succeeded")
			}
			if mode == "serve" && (got.Error != nil || len(got.Tools) != 1) {
				t.Fatalf("successful discovery lost: %+v", got)
			}
		})
	}
}

func TestMCPDiscoveryStopsLingeringStdioServer(t *testing.T) {
	t.Run("terminate", func(t *testing.T) {
		realGrace := mcpStdioShutdownGrace
		t.Cleanup(func() { mcpStdioShutdownGrace = realGrace })
		mcpStdioShutdownGrace = 50 * time.Millisecond
		server, pidFile := stdioFixtureServer(t, "linger")
		got := (&RealConnector{}).ConnectAndDiscover(context.Background(), server)
		if got.Error != nil || len(got.Tools) != 1 {
			t.Fatalf("successful discovery lost: %+v", got)
		}
		if _, err := os.Stat(pidFile + ".term"); err != nil {
			t.Fatalf("server did not receive SIGTERM after the grace: %v", err)
		}
		assertStdioFixtureStopped(t, pidFile)
	})
	t.Run("cancel-during-grace", func(t *testing.T) {
		server, pidFile := stdioFixtureServer(t, "linger")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan DiscoveryResult, 1)
		go func() { result <- (&RealConnector{}).ConnectAndDiscover(ctx, server) }()
		for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(5 * time.Millisecond) {
			if _, err := os.Stat(pidFile + ".eof"); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("server never reached end of input")
			}
		}
		cancel()
		select {
		case <-result:
		case <-time.After(mcpStdioShutdownGrace / 2):
			t.Fatal("cancellation waited for the shutdown grace")
		}
		assertStdioFixtureStopped(t, pidFile)
	})
}

func TestMCPDiscoveryReportsStdioCleanupFailure(t *testing.T) {
	realStart := startOwnedProcess
	t.Cleanup(func() { startOwnedProcess = realStart })
	startOwnedProcess = func(ctx context.Context, cmd *exec.Cmd) (func() error, error) {
		wait, err := realStart(ctx, cmd)
		if err != nil {
			return nil, err
		}
		return func() error {
			return errors.Join(wait(), &ownedprocess.CleanupError{Err: errors.New("injected")})
		}, nil
	}
	server, _ := stdioFixtureServer(t, "serve")
	got := (&RealConnector{}).ConnectAndDiscover(context.Background(), server)
	var cleanupErr *ownedprocess.CleanupError
	if !errors.As(got.Error, &cleanupErr) {
		t.Fatalf("cleanup failure hidden behind catalog: %+v", got)
	}
}

func assertStdioFixtureStopped(t *testing.T, pidFile string) {
	t.Helper()
	pid, err := stdioFixturePID(pidFile)
	if err != nil {
		t.Fatalf("fixture child readiness: %v", err)
	}
	if !stdioFixtureStopped(pid) {
		t.Errorf("child %d still running when discovery returned", pid)
	}
}

func stdioFixturePID(path string) (int, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- test-owned PID path.
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid fixture PID %q", data)
	}
	return pid, nil
}

// stdioFixtureStopped accepts vanished, zombie and dead fixture processes.
func stdioFixtureStopped(pid int) bool {
	state, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		var exit *exec.ExitError
		return errors.As(err, &exit) && exit.ExitCode() == 1
	}
	stat := strings.TrimSpace(string(state))
	return stat == "" || strings.ContainsAny(stat[:1], "ZXx")
}
