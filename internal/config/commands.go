package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// LoadCommandsAllow reads .agent-layer/commands.allow into a slice of prefixes.
func LoadCommandsAllow(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingCommandsAllowlistFmt, path, err)
	}

	var commands []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		commands = append(commands, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf(messages.ConfigFailedReadCommandsAllowlistFmt, path, err)
	}

	return commands, nil
}
