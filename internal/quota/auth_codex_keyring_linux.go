package quota

import (
	"context"

	"golang.org/x/sys/unix"
)

func readCodexKeyring(ctx context.Context, service, account string) ([]byte, failure) {
	// Codex's keyring-rs first searches the kernel's session keyring, which
	// can already link its persistent keyring. Search without writing a
	// password or explicitly linking a keyring.
	description := "keyring-rs:" + account + "@" + service
	if id, err := unix.KeyctlSearch(unix.KEY_SPEC_SESSION_KEYRING, "user", description, 0); err == nil {
		size, err := unix.KeyctlBuffer(unix.KEYCTL_READ, id, nil, 0)
		if err == nil && size > 0 && size <= maxPayloadSize {
			key := make([]byte, size)
			n, err := unix.KeyctlBuffer(unix.KEYCTL_READ, id, key, 0)
			if err == nil && n > 0 && n <= len(key) {
				return key[:n], failure{}
			}
		}
	}
	return readKeyring(ctx, service, account)
}
