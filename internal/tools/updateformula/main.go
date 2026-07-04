//go:build tools
// +build tools

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const (
	releaseAssetURL = "https://github.com/conn-castle/agent-layer/releases/download"
)

var formulaTemplate = template.Must(template.New("formula").Parse(`class AgentLayer < Formula
  desc "Config-first CLI for keeping coding agents in sync"
  homepage "https://github.com/conn-castle/agent-layer"
  version "{{ .Version }}"
  license "MIT"

  on_macos do
    on_arm do
      url "{{ .ReleaseBaseURL }}/al-darwin-arm64", using: :nounzip
      sha256 "{{ .DarwinARM64SHA }}"
    end

    on_intel do
      url "{{ .ReleaseBaseURL }}/al-darwin-amd64", using: :nounzip
      sha256 "{{ .DarwinAMD64SHA }}"
    end
  end

  on_linux do
    on_arm do
      url "{{ .ReleaseBaseURL }}/al-linux-arm64", using: :nounzip
      sha256 "{{ .LinuxARM64SHA }}"
    end

    on_intel do
      url "{{ .ReleaseBaseURL }}/al-linux-amd64", using: :nounzip
      sha256 "{{ .LinuxAMD64SHA }}"
    end
  end

  def install
    bin.install Dir["al-*"].first => "al"
    chmod 0555, bin/"al" # generate_completions_from_executable fails otherwise
    generate_completions_from_executable(bin/"al", "completion")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/al --version")
  end
end
`))

type formulaData struct {
	Version        string
	ReleaseBaseURL string
	DarwinARM64SHA string
	DarwinAMD64SHA string
	LinuxARM64SHA  string
	LinuxAMD64SHA  string
}

func main() {
	os.Exit(run(os.Args, os.Stderr))
}

// run executes the Homebrew formula updater CLI.
// args are the CLI arguments (including argv0). errOut is the error output stream.
// It renders the binary formula for the provided release tag and checksum file.
func run(args []string, errOut io.Writer) int {
	if len(args) != 4 {
		fmt.Fprintf(errOut, messages.UpdateFormulaUsageFmt, args[0])
		return 1
	}

	formulaPath := args[1]
	tag := args[2]
	checksumsPath := args[3]

	info, err := os.Stat(formulaPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(errOut, messages.UpdateFormulaFileMissingFmt, formulaPath)
			return 1
		}
		fmt.Fprintf(errOut, messages.UpdateFormulaStatFailedFmt, formulaPath, err)
		return 1
	}

	checksums, err := readChecksums(checksumsPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(errOut, messages.UpdateFormulaFileMissingFmt, checksumsPath)
			return 1
		}
		fmt.Fprintf(errOut, messages.UpdateFormulaReadFailedFmt, checksumsPath, err)
		return 1
	}

	darwinARM64SHA, ok := checksums["al-darwin-arm64"]
	if !ok {
		fmt.Fprintf(errOut, messages.UpdateFormulaChecksumMissingFmt, "al-darwin-arm64", checksumsPath)
		return 1
	}
	darwinAMD64SHA, ok := checksums["al-darwin-amd64"]
	if !ok {
		fmt.Fprintf(errOut, messages.UpdateFormulaChecksumMissingFmt, "al-darwin-amd64", checksumsPath)
		return 1
	}
	linuxARM64SHA, ok := checksums["al-linux-arm64"]
	if !ok {
		fmt.Fprintf(errOut, messages.UpdateFormulaChecksumMissingFmt, "al-linux-arm64", checksumsPath)
		return 1
	}
	linuxAMD64SHA, ok := checksums["al-linux-amd64"]
	if !ok {
		fmt.Fprintf(errOut, messages.UpdateFormulaChecksumMissingFmt, "al-linux-amd64", checksumsPath)
		return 1
	}

	data := formulaData{
		Version:        strings.TrimPrefix(tag, "v"),
		ReleaseBaseURL: releaseAssetURL + "/" + tag,
		DarwinARM64SHA: darwinARM64SHA,
		DarwinAMD64SHA: darwinAMD64SHA,
		LinuxARM64SHA:  linuxARM64SHA,
		LinuxAMD64SHA:  linuxAMD64SHA,
	}

	var rendered bytes.Buffer
	if err := formulaTemplate.Execute(&rendered, data); err != nil {
		fmt.Fprintf(errOut, messages.UpdateFormulaRenderFailedFmt, err)
		return 1
	}

	if err := fsutil.WriteFileAtomic(formulaPath, rendered.Bytes(), info.Mode()); err != nil {
		fmt.Fprintf(errOut, messages.UpdateFormulaWriteFailedFmt, formulaPath, err)
		return 1
	}

	return 0
}

func readChecksums(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	checksums := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		// Strip the binary-mode marker ("*") and a leading "./" so names match
		// the release asset keys regardless of how sha256sum/shasum emitted them,
		// consistent with the extractchecksum tool.
		filename := strings.TrimPrefix(strings.TrimPrefix(fields[1], "*"), "./")
		checksums[filename] = fields[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return checksums, nil
}
