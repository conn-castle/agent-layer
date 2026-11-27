//go:build linux

package quota

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	secretServiceBus           = "org.freedesktop.secrets"
	secretServicePath          = dbus.ObjectPath("/org/freedesktop/secrets")
	secretServiceIface         = "org.freedesktop.Secret.Service"
	secretCollectionIface      = "org.freedesktop.Secret.Collection"
	secretItemIface            = "org.freedesktop.Secret.Item"
	secretSessionIface         = "org.freedesktop.Secret.Session"
	dbusPropertiesGetMethod    = "org.freedesktop.DBus.Properties.Get"
	codexKeyringLookupTimeout  = 2 * time.Second
	codexKeyringSessionCleanup = 250 * time.Millisecond
)

type dbusSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

type dbusSecretService struct {
	conn    *dbus.Conn
	session dbus.ObjectPath
}

func readCodexKeyring(ctx context.Context, service, account string) ([]byte, failure) {
	if ctx.Err() != nil {
		return nil, contextFailure(ctx)
	}
	callCtx, cancel := context.WithTimeout(ctx, codexKeyringLookupTimeout)
	defer cancel()

	conn, err := connectCodexSessionBus(callCtx)
	if err != nil {
		return nil, secretServiceFailure(ctx, callCtx)
	}
	defer func() { _ = conn.Close() }()

	var output dbus.Variant
	var session dbus.ObjectPath
	svc := conn.Object(secretServiceBus, secretServicePath)
	if err := svc.CallWithContext(callCtx, secretServiceIface+".OpenSession", dbus.FlagNoAutoStart, "plain", dbus.MakeVariant("")).Store(&output, &session); err != nil {
		return nil, secretServiceFailure(ctx, callCtx)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), codexKeyringSessionCleanup)
		defer closeCancel()
		_ = conn.Object(secretServiceBus, session).CallWithContext(closeCtx, secretSessionIface+".Close", dbus.FlagNoAutoStart).Err
	}()

	return readCodexSecretService(ctx, callCtx, &dbusSecretService{conn: conn, session: session}, service, account)
}

// Only connect to existing local Unix session buses. The generic connector
// can launch a D-Bus daemon or dial TCP before applying its context.
func connectCodexSessionBus(ctx context.Context) (*dbus.Conn, error) {
	address := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if address == "" || address == "autolaunch:" {
		address = fmt.Sprintf("unix:path=/run/user/%d/bus", os.Getuid())
	}
	var dialer net.Dialer
	for _, socket := range codexSessionBusSockets(address) {
		raw, err := dialer.DialContext(ctx, "unix", socket)
		if err != nil {
			continue
		}
		deadline, _ := ctx.Deadline()
		if err := raw.SetDeadline(deadline); err != nil {
			_ = raw.Close()
			continue
		}
		conn, err := dbus.ConnectUnix(raw.(*net.UnixConn), dbus.WithContext(ctx))
		if err == nil {
			return conn, nil
		}
		_ = raw.Close()
	}
	return nil, errors.New("local Secret Service bus unavailable")
}

func codexSessionBusSockets(address string) []string {
	var sockets []string
	for _, entry := range strings.Split(address, ";") {
		params, ok := strings.CutPrefix(entry, "unix:")
		if !ok {
			continue
		}
		for _, parameter := range strings.Split(params, ",") {
			key, value, _ := strings.Cut(parameter, "=")
			if key != "path" && key != "abstract" {
				continue
			}
			value, err := dbus.UnescapeBusAddressValue(value)
			if err != nil || value == "" {
				continue
			}
			if key == "abstract" {
				value = "@" + value
			}
			sockets = append(sockets, value)
			break
		}
	}
	return sockets
}

func (s *dbusSecretService) searchItems(ctx context.Context, attrs map[string]string) (unlocked, locked []string, err error) {
	var rawUnlocked, rawLocked []dbus.ObjectPath
	err = s.conn.Object(secretServiceBus, secretServicePath).
		CallWithContext(ctx, secretServiceIface+".SearchItems", dbus.FlagNoAutoStart, attrs).
		Store(&rawUnlocked, &rawLocked)
	if err != nil {
		return nil, nil, err
	}
	return objectPathStrings(rawUnlocked), objectPathStrings(rawLocked), nil
}

func (s *dbusSecretService) readDefaultCollection(ctx context.Context) (string, error) {
	var collection dbus.ObjectPath
	err := s.conn.Object(secretServiceBus, secretServicePath).
		CallWithContext(ctx, secretServiceIface+".ReadAlias", dbus.FlagNoAutoStart, "default").
		Store(&collection)
	return string(collection), err
}

func (s *dbusSecretService) collectionLocked(ctx context.Context, collection string) (bool, error) {
	return s.boolProperty(ctx, dbus.ObjectPath(collection), secretCollectionIface, "Locked")
}

func (s *dbusSecretService) collectionSearchItems(ctx context.Context, collection string, attrs map[string]string) ([]string, error) {
	var items []dbus.ObjectPath
	err := s.conn.Object(secretServiceBus, dbus.ObjectPath(collection)).
		CallWithContext(ctx, secretCollectionIface+".SearchItems", dbus.FlagNoAutoStart, attrs).
		Store(&items)
	if err != nil {
		return nil, err
	}
	return objectPathStrings(items), nil
}

func (s *dbusSecretService) itemAttributes(ctx context.Context, item string) (map[string]string, error) {
	var raw dbus.Variant
	err := s.conn.Object(secretServiceBus, dbus.ObjectPath(item)).
		CallWithContext(ctx, dbusPropertiesGetMethod, dbus.FlagNoAutoStart, secretItemIface, "Attributes").
		Store(&raw)
	if err != nil {
		return nil, err
	}
	attrs, ok := raw.Value().(map[string]string)
	if !ok {
		return nil, errors.New("unexpected Secret Service attributes type")
	}
	return attrs, nil
}

func (s *dbusSecretService) itemLocked(ctx context.Context, item string) (bool, error) {
	return s.boolProperty(ctx, dbus.ObjectPath(item), secretItemIface, "Locked")
}

func (s *dbusSecretService) itemSecret(ctx context.Context, item string) ([]byte, error) {
	var secret dbusSecret
	err := s.conn.Object(secretServiceBus, dbus.ObjectPath(item)).
		CallWithContext(ctx, secretItemIface+".GetSecret", dbus.FlagNoAutoStart, s.session).
		Store(&secret)
	if err != nil {
		return nil, err
	}
	return secret.Value, nil
}

func (s *dbusSecretService) boolProperty(ctx context.Context, path dbus.ObjectPath, iface, prop string) (bool, error) {
	var raw dbus.Variant
	err := s.conn.Object(secretServiceBus, path).
		CallWithContext(ctx, dbusPropertiesGetMethod, dbus.FlagNoAutoStart, iface, prop).
		Store(&raw)
	if err != nil {
		return false, err
	}
	value, ok := raw.Value().(bool)
	if !ok {
		return false, errors.New("unexpected Secret Service boolean property type")
	}
	return value, nil
}

func objectPathStrings(paths []dbus.ObjectPath) []string {
	out := make([]string, len(paths))
	for i, path := range paths {
		out[i] = string(path)
	}
	return out
}
