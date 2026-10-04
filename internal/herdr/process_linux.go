//go:build linux

package herdr

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func processLineage(pid int) (int, string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, "", err
	}
	closeParen := strings.LastIndex(string(data), ")")
	if closeParen < 0 {
		return 0, "", fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(data)[closeParen+1:])
	if len(fields) <= 19 {
		return 0, "", fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, "", fmt.Errorf("parse /proc/%d/stat parent: %w", pid, err)
	}
	return parent, "linux:" + fields[19], nil
}
