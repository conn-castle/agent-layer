//go:build !darwin && !linux

package herdr

import "fmt"

func processLineage(pid int) (int, string, error) {
	return 0, "", fmt.Errorf("process start identity is unsupported on this platform")
}
