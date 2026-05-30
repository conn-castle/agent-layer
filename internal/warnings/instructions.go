package warnings

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// MeasureInstructions returns the estimated token count of the combined instruction
// payload along with the source subject (e.g. "AGENTS.md" or ".agent-layer/instructions/*").
// It is the single source for instruction sizing used by both CheckInstructions and the
// doctor size summary. It returns an error only if the payload cannot be read.
func MeasureInstructions(rootDir string) (int, string, error) {
	content, subject, err := getInstructionPayload(rootDir)
	if err != nil {
		return 0, "", err
	}
	return EstimateTokens(content), subject, nil
}

// CheckInstructions checks if the combined instruction payload exceeds the threshold.
// rootDir is the project root directory; threshold is the max token count (nil disables warnings).
// It returns any warnings and an error if the payload cannot be read.
func CheckInstructions(rootDir string, threshold *int) ([]Warning, error) {
	if threshold == nil {
		return nil, nil
	}
	tokens, subject, err := MeasureInstructions(rootDir)
	if err != nil {
		return nil, err
	}

	if tokens > *threshold {
		return []Warning{{
			Code:              CodeInstructionsTooLarge,
			Subject:           subject,
			Message:           fmt.Sprintf(messages.WarningsInstructionsTooLargeFmt, *threshold, tokens, *threshold),
			Fix:               messages.WarningsInstructionsTooLargeFix,
			Source:            SourceInternal,
			Severity:          SeverityWarning,
			NoiseSuppressible: true,
		}}, nil
	}

	return nil, nil
}

// getInstructionPayload returns the combined instruction content and the source subject.
func getInstructionPayload(rootDir string) (string, string, error) {
	// 1. Try AGENTS.md
	agentsPath := filepath.Join(rootDir, "AGENTS.md")
	if _, err := os.Stat(agentsPath); err == nil {
		content, err := os.ReadFile(agentsPath) // #nosec G304 -- path is rootDir/AGENTS.md, joined from caller-resolved repo root.
		if err != nil {
			return "", "", err
		}
		return string(content), "AGENTS.md", nil
	}

	// 2. Fallback to .agent-layer/instructions/*.md
	instructionsDir := filepath.Join(rootDir, ".agent-layer", "instructions")
	files, err := os.ReadDir(instructionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// If neither exists, empty payload
			return "", ".agent-layer/instructions/*", nil
		}
		return "", "", err
	}

	var filenames []string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
			filenames = append(filenames, f.Name())
		}
	}
	sort.Strings(filenames)

	var sb strings.Builder
	for i, name := range filenames {
		path := filepath.Join(instructionsDir, name)
		content, err := os.ReadFile(path) // #nosec G304 -- path joins rootDir/.agent-layer/instructions/ with a filename produced by os.ReadDir of that same directory; all components are caller-resolved.
		if err != nil {
			return "", "", err
		}
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.Write(content)
	}

	return sb.String(), ".agent-layer/instructions/*", nil
}
