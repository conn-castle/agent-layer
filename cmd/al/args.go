package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

const (
	flagQuiet       = "--quiet"
	flagQuietShort  = "-q"
	flagQuietPrefix = "--quiet="
)

// stripArgsSeparator removes a standalone "--" and returns the args that should be
// forwarded to the underlying client. Arguments before "--" are preserved.
func stripArgsSeparator(args []string) []string {
	for i, arg := range args {
		if arg == "--" {
			passArgs := make([]string, 0, len(args)-1)
			passArgs = append(passArgs, args[:i]...)
			passArgs = append(passArgs, args[i+1:]...)
			return passArgs
		}
	}
	return append([]string{}, args...)
}

// splitQuietArgs parses --quiet/-q from pass-through args and returns quiet along
// with the args that should be forwarded to the underlying client.
func splitQuietArgs(args []string) (bool, []string, error) {
	quiet := false
	passArgs := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			passArgs = append(passArgs, args[i+1:]...)
			break
		}
		if arg == flagQuiet || arg == flagQuietShort {
			quiet = true
			continue
		}
		if strings.HasPrefix(arg, flagQuietPrefix) {
			value := strings.TrimPrefix(arg, flagQuietPrefix)
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return false, nil, fmt.Errorf(messages.QuietInvalidFmt, value)
			}
			quiet = parsed
			continue
		}
		passArgs = append(passArgs, arg)
	}
	return quiet, passArgs, nil
}
