//go:build linux

package quota

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaCodexSessionBusOnlyUsesLocalSockets(t *testing.T) {
	address := "tcp:host=fixture.example,port=1234;autolaunch:;unix:path=/run/user/1/bus,guid=abc;unix:abstract=fixture%2Cbus"
	assert.Equal(t, []string{"/run/user/1/bus", "@fixture,bus"}, codexSessionBusSockets(address, "/tmp/runtime-1", 1))
}

func TestQuotaCodexSessionBusSockets(t *testing.T) {
	for _, tc := range []struct {
		name          string
		address       string
		xdgRuntimeDir string
		uid           int
		want          []string
	}{
		{
			name:          "empty address prefers XDG_RUNTIME_DIR before /run/user",
			address:       "",
			xdgRuntimeDir: "/tmp/runtime-1000",
			uid:           1000,
			want:          []string{"/tmp/runtime-1000/bus", "/run/user/1000/bus"},
		},
		{
			name:          "empty address deduplicates matching XDG_RUNTIME_DIR",
			address:       "",
			xdgRuntimeDir: "/run/user/1000",
			uid:           1000,
			want:          []string{"/run/user/1000/bus"},
		},
		{
			name:          "relative XDG_RUNTIME_DIR ignored",
			address:       "",
			xdgRuntimeDir: "relative/runtime",
			uid:           1000,
			want:          []string{"/run/user/1000/bus"},
		},
		{
			name:          "parameterized autolaunch falls back to local runtime sockets",
			address:       "autolaunch:scope=install-path",
			xdgRuntimeDir: "/tmp/runtime-1000",
			uid:           1000,
			want:          []string{"/tmp/runtime-1000/bus", "/run/user/1000/bus"},
		},
		{
			name:          "unix runtime=yes uses local runtime sockets",
			address:       "unix:runtime=yes,guid=1234",
			xdgRuntimeDir: "/tmp/runtime-1000",
			uid:           1000,
			want:          []string{"/tmp/runtime-1000/bus", "/run/user/1000/bus"},
		},
		{
			name:          "explicit unix path and abstract sockets",
			address:       "unix:path=/tmp/custom%20bus;unix:abstract=test-bus",
			xdgRuntimeDir: "/tmp/runtime-1000",
			uid:           1000,
			want:          []string{"/tmp/custom bus", "@test-bus"},
		},
		{
			name:          "non-unix address does not fall back to local runtime sockets",
			address:       "tcp:host=127.0.0.1,port=12345",
			xdgRuntimeDir: "/tmp/runtime-1000",
			uid:           1000,
			want:          nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, codexSessionBusSockets(tc.address, tc.xdgRuntimeDir, tc.uid))
		})
	}
}

func TestQuotaCodexSessionBusAuthenticationDeadline(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bus")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+dbus.EscapeBusAddressValue(socket))
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Hold the connection open without answering the authentication request.
		_, _ = io.Copy(io.Discard, conn)
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = connectCodexSessionBus(ctx)
	require.Error(t, err)
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("the expired authentication connection did not close")
	}
}
