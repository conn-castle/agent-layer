//go:build !unix

package agentdispatch

import (
	"context"
	"os"
)

func readPromptFile(ctx context.Context, path string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, promptPreparationCancelled(ctx)
	}
	file, err := os.Open(path) // #nosec G304 -- caller explicitly selected the prompt file.
	if err != nil {
		return nil, wrapExitError(ExitUsage, "read dispatch prompt file", err)
	}
	defer func() { _ = file.Close() }()
	stat, err := file.Stat()
	if err != nil {
		return nil, wrapExitError(ExitUsage, "read dispatch prompt file", err)
	}
	var size int64
	if stat.Mode().IsRegular() {
		if err := checkPromptFileSize(stat.Size()); err != nil {
			return nil, err
		}
		size = stat.Size()
	}
	return readPromptBytes(ctx, path, size, file.Read)
}
