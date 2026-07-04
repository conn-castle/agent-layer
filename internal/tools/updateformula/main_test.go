//go:build tools
// +build tools

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunRendersBinaryFormula(t *testing.T) {
	dir := t.TempDir()
	formulaPath := filepath.Join(dir, "agent-layer.rb")
	checksumsPath := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(formulaPath, []byte("old formula\n"), 0o644); err != nil {
		t.Fatalf("write formula: %v", err)
	}
	if err := os.WriteFile(checksumsPath, []byte(`1111111111111111111111111111111111111111111111111111111111111111  ./al-darwin-arm64
4444444444444444444444444444444444444444444444444444444444444444  al-darwin-amd64
2222222222222222222222222222222222222222222222222222222222222222  al-linux-arm64
3333333333333333333333333333333333333333333333333333333333333333  al-linux-amd64
`), 0o644); err != nil {
		t.Fatalf("write checksums: %v", err)
	}

	var stderr bytes.Buffer
	if code := run([]string{"updateformula", formulaPath, "v1.2.3", checksumsPath}, &stderr); code != 0 {
		t.Fatalf("run exit code %d, stderr: %s", code, stderr.String())
	}

	got, err := os.ReadFile(formulaPath)
	if err != nil {
		t.Fatalf("read formula: %v", err)
	}
	want := `class AgentLayer < Formula
  desc "Config-first CLI for keeping coding agents in sync"
  homepage "https://github.com/conn-castle/agent-layer"
  version "1.2.3"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/conn-castle/agent-layer/releases/download/v1.2.3/al-darwin-arm64", using: :nounzip
      sha256 "1111111111111111111111111111111111111111111111111111111111111111"
    end

    on_intel do
      url "https://github.com/conn-castle/agent-layer/releases/download/v1.2.3/al-darwin-amd64", using: :nounzip
      sha256 "4444444444444444444444444444444444444444444444444444444444444444"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/conn-castle/agent-layer/releases/download/v1.2.3/al-linux-arm64", using: :nounzip
      sha256 "2222222222222222222222222222222222222222222222222222222222222222"
    end

    on_intel do
      url "https://github.com/conn-castle/agent-layer/releases/download/v1.2.3/al-linux-amd64", using: :nounzip
      sha256 "3333333333333333333333333333333333333333333333333333333333333333"
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
`
	if string(got) != want {
		t.Fatalf("unexpected formula:\n%s", string(got))
	}
}

func TestRunFailsWhenChecksumIsMissing(t *testing.T) {
	dir := t.TempDir()
	formulaPath := filepath.Join(dir, "agent-layer.rb")
	checksumsPath := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(formulaPath, []byte("old formula\n"), 0o644); err != nil {
		t.Fatalf("write formula: %v", err)
	}
	if err := os.WriteFile(checksumsPath, []byte(`1111111111111111111111111111111111111111111111111111111111111111  al-darwin-arm64
4444444444444444444444444444444444444444444444444444444444444444  al-darwin-amd64
2222222222222222222222222222222222222222222222222222222222222222  al-linux-arm64
`), 0o644); err != nil {
		t.Fatalf("write checksums: %v", err)
	}

	var stderr bytes.Buffer
	if code := run([]string{"updateformula", formulaPath, "v1.2.3", checksumsPath}, &stderr); code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("checksum for al-linux-amd64 not found")) {
		t.Fatalf("expected missing checksum error, got %s", stderr.String())
	}
}
