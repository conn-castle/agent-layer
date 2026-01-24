//go:build tools
// +build tools

package main

import (
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
)

var (
	urlPattern = regexp.MustCompile(`(?m)^(\s*url\s+").*("\s*)$`)
	shaPattern = regexp.MustCompile(`(?m)^(\s*sha256\s+").*("\s*)$`)
)

func main() {
	os.Exit(run(os.Args, os.Stderr))
}

// run executes the Homebrew formula updater CLI.
// args are the CLI arguments (including argv0). errOut is the error output stream.
// It returns an exit code compatible with the original Python script.
func run(args []string, errOut io.Writer) int {
	if len(args) != 4 {
		fmt.Fprintf(errOut, messages.UpdateFormulaUsageFmt, args[0])
		return 1
	}

	formulaPath := args[1]
	newURL := args[2]
	newSHA := args[3]

	info, err := os.Stat(formulaPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(errOut, messages.UpdateFormulaFileMissingFmt, formulaPath)
			return 1
		}
		fmt.Fprintf(errOut, messages.UpdateFormulaStatFailedFmt, formulaPath, err)
		return 1
	}

	content, err := os.ReadFile(formulaPath)
	if err != nil {
		fmt.Fprintf(errOut, messages.UpdateFormulaReadFailedFmt, formulaPath, err)
		return 1
	}

	text := string(content)
	urlMatches := urlPattern.FindAllStringSubmatch(text, -1)
	if len(urlMatches) != 1 {
		fmt.Fprintf(errOut, messages.UpdateFormulaURLCountFmt, len(urlMatches))
		return 1
	}
	shaMatches := shaPattern.FindAllStringSubmatch(text, -1)
	if len(shaMatches) != 1 {
		fmt.Fprintf(errOut, messages.UpdateFormulaSHACountFmt, len(shaMatches))
		return 1
	}

	text = replaceLine(urlPattern, text, newURL)
	text = replaceLine(shaPattern, text, newSHA)

	if err := fsutil.WriteFileAtomic(formulaPath, []byte(text), info.Mode()); err != nil {
		fmt.Fprintf(errOut, messages.UpdateFormulaWriteFailedFmt, formulaPath, err)
		return 1
	}

	return 0
}

// replaceLine replaces the value inside a matched quoted line while preserving indentation.
// pattern must capture the prefix and suffix as submatches; value is inserted between them.
func replaceLine(pattern *regexp.Regexp, text string, value string) string {
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := pattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return parts[1] + value + parts[2]
	})
}
