//go:build !linux

package quota

import "context"

func readCodexKeyring(ctx context.Context, service, account string) ([]byte, failure) {
	return readKeyring(ctx, service, account)
}
