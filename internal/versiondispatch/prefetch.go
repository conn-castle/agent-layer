package versiondispatch

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/version"
)

// PrefetchVersion ensures the requested release binary is cached locally.
// It validates the version, resolves the cache root, downloads the binary when missing,
// and writes download progress to progressOut. Canceling ctx stops a pending
// download or cache-lock wait.
func PrefetchVersion(ctx context.Context, versionInput string, progressOut io.Writer) error {
	normalized, err := version.Normalize(strings.TrimSpace(versionInput))
	if err != nil {
		return fmt.Errorf(messages.DispatchInvalidEnvVersionFmt, "version", err)
	}
	sys := RealSystem{}
	cacheRoot, err := cacheRootDir(sys)
	if err != nil {
		return err
	}
	_, err = ensureCachedBinaryWithSystem(ctx, sys, cacheRoot, normalized, progressOut)
	if err != nil {
		return err
	}
	return ctx.Err()
}
