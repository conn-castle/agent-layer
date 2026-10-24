package sync

import "github.com/conn-castle/agent-layer/internal/fsutil"

func readRegularFile(sys System, path string) ([]byte, error) {
	info, err := sys.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := fsutil.RequireRegularFile(info, path); err != nil {
		return nil, err
	}
	return sys.ReadFile(path)
}
