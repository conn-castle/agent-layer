package warnings

import (
	"fmt"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

const (
	// NoiseModeDefault keeps all warnings.
	NoiseModeDefault = "default"
	// NoiseModeReduce hides suppressible non-critical warnings.
	NoiseModeReduce = "reduce"
)

// ApplyNoiseControl applies a conservative noise filter to warning output.
// mode is the warnings.noise_mode value from config.
func ApplyNoiseControl(items []Warning, mode string) []Warning {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" || normalized == NoiseModeDefault {
		if len(items) == 0 {
			return nil
		}
		return append([]Warning(nil), items...)
	}
	if normalized == NoiseModeReduce {
		filtered := make([]Warning, 0, len(items))
		for _, item := range items {
			if item.severityOrDefault() == SeverityCritical {
				filtered = append(filtered, item)
				continue
			}
			if item.NoiseSuppressible {
				continue
			}
			filtered = append(filtered, item)
		}
		return filtered
	}

	out := append([]Warning(nil), items...)
	out = append(out, Warning{
		Code:     CodeWarningNoiseModeInvalid,
		Subject:  "warnings.noise_mode",
		Message:  fmt.Sprintf(messages.WarningsNoiseModeInvalidFmt, mode, NoiseModeDefault, NoiseModeReduce),
		Fix:      messages.WarningsNoiseModeInvalidFix,
		Source:   SourceInternal,
		Severity: SeverityCritical,
	})
	return out
}
