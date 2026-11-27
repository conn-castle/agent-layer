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
	assert.Equal(t, []string{"/run/user/1/bus", "@fixture,bus"}, codexSessionBusSockets(address))
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
