package warnings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyNoiseControl_Default(t *testing.T) {
	items := []Warning{
		{Code: CodeMCPTooManyToolsTotal, NoiseSuppressible: true, Severity: SeverityWarning},
		{Code: CodeMCPServerUnreachable, Severity: SeverityCritical},
	}
	filtered := ApplyNoiseControl(items, "")
	require.Len(t, filtered, 2)
}

func TestApplyNoiseControl_Reduce(t *testing.T) {
	items := []Warning{
		{Code: CodeMCPTooManyToolsTotal, NoiseSuppressible: true, Severity: SeverityWarning},
		{Code: CodeMCPServerUnreachable, NoiseSuppressible: true, Severity: SeverityCritical},
		{Code: CodePolicyCapabilityMismatch, Severity: SeverityWarning},
	}
	filtered := ApplyNoiseControl(items, NoiseModeReduce)
	require.Len(t, filtered, 2)
	require.Equal(t, CodeMCPServerUnreachable, filtered[0].Code)
	require.Equal(t, CodePolicyCapabilityMismatch, filtered[1].Code)
}

func TestApplyNoiseControl_Quiet(t *testing.T) {
	items := []Warning{
		{Code: CodeMCPTooManyToolsTotal, NoiseSuppressible: true, Severity: SeverityWarning},
		{Code: CodeMCPServerUnreachable, Severity: SeverityCritical},
	}
	filtered := ApplyNoiseControl(items, NoiseModeQuiet)
	require.Nil(t, filtered)
}

func TestApplyNoiseControl_UnknownMode(t *testing.T) {
	items := []Warning{
		{Code: CodeMCPTooManyToolsTotal, NoiseSuppressible: true, Severity: SeverityWarning},
	}
	filtered := ApplyNoiseControl(items, "unknown")
	require.Len(t, filtered, 2)
	require.Equal(t, CodeMCPTooManyToolsTotal, filtered[0].Code)
	require.Equal(t, CodeWarningNoiseModeInvalid, filtered[1].Code)
	require.Equal(t, SeverityCritical, filtered[1].Severity)
	require.Equal(t, "warnings.noise_mode", filtered[1].Subject)
}

func TestApplyNoiseControl_UnknownModeNoItemsStillWarns(t *testing.T) {
	filtered := ApplyNoiseControl(nil, "unknown")
	require.Len(t, filtered, 1)
	require.Equal(t, CodeWarningNoiseModeInvalid, filtered[0].Code)
}

func TestApplyNoiseControl_DefaultNoItemsReturnsNil(t *testing.T) {
	require.Nil(t, ApplyNoiseControl(nil, NoiseModeDefault))
}
