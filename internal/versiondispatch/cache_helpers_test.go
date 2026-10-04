package versiondispatch

import (
	"context"
	"io"
	"os"
	"runtime"
)

// ensureCachedBinary returns the cached binary path, downloading and verifying it if missing.
// Progress lines are written to progressOut when a download is required.
func ensureCachedBinary(ctx context.Context, cacheRoot string, version string, progressOut io.Writer) (string, error) {
	return ensureCachedBinaryWithSystem(ctx, RealSystem{}, cacheRoot, version, progressOut)
}

// platformStrings returns the supported OS and architecture strings for release assets.
func platformStrings() (string, string, error) {
	return checkPlatform(runtime.GOOS, runtime.GOARCH)
}

// downloadToFile fetches url and writes it to dest.
func downloadToFile(ctx context.Context, url string, dest *os.File) error {
	return downloadToFileWithSystem(ctx, RealSystem{}, url, dest)
}

// fetchChecksum retrieves the expected checksum for the asset from checksums.txt.
func fetchChecksum(ctx context.Context, version string, asset string) (string, error) {
	return fetchChecksumWithSystem(ctx, RealSystem{}, version, asset)
}
