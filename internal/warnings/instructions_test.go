package warnings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckInstructions_AgentsMD(t *testing.T) {
	tmpDir := t.TempDir()
	threshold := 4000

	// Create AGENTS.md with small content
	err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("small content"), 0644)
	require.NoError(t, err)

	warnings, err := CheckInstructions(tmpDir, &threshold)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	// Create AGENTS.md with large content
	// Need > 4000 tokens. 4000 tokens ~= 12000 bytes (very roughly).
	// Let's make it 20000 bytes to be safe.
	largeContent := make([]byte, 20000)
	for i := range largeContent {
		largeContent[i] = 'a'
	}
	err = os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), largeContent, 0644)
	require.NoError(t, err)

	warnings, err = CheckInstructions(tmpDir, &threshold)
	require.NoError(t, err)
	assert.Len(t, warnings, 1)
	assert.Equal(t, CodeInstructionsTooLarge, warnings[0].Code)
	assert.Equal(t, "AGENTS.md", warnings[0].Subject)
}

func TestCheckInstructions_Fallback(t *testing.T) {
	tmpDir := t.TempDir()
	threshold := 4000
	instDir := filepath.Join(tmpDir, ".agent-layer", "instructions")
	err := os.MkdirAll(instDir, 0755)
	require.NoError(t, err)

	// file A
	err = os.WriteFile(filepath.Join(instDir, "01_a.md"), []byte("part one"), 0644)
	require.NoError(t, err)
	// file B
	err = os.WriteFile(filepath.Join(instDir, "02_b.md"), []byte("part two"), 0644)
	require.NoError(t, err)

	warnings, err := CheckInstructions(tmpDir, &threshold)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	// Make file B huge
	largeContent := make([]byte, 20000)
	for i := range largeContent {
		largeContent[i] = 'b'
	}
	err = os.WriteFile(filepath.Join(instDir, "02_b.md"), largeContent, 0644)
	require.NoError(t, err)

	warnings, err = CheckInstructions(tmpDir, &threshold)
	require.NoError(t, err)
	assert.Len(t, warnings, 1)
	assert.Equal(t, CodeInstructionsTooLarge, warnings[0].Code)
	assert.Equal(t, ".agent-layer/instructions/*", warnings[0].Subject)
}

func TestCheckInstructions_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	largeContent := make([]byte, 20000)
	for i := range largeContent {
		largeContent[i] = 'a'
	}
	err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), largeContent, 0644)
	require.NoError(t, err)

	warnings, err := CheckInstructions(tmpDir, nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestCheckInstructions_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()
	threshold := 4000
	warnings, err := CheckInstructions(tmpDir, &threshold)
	require.NoError(t, err)
	assert.Empty(t, warnings)
}

func TestCheckInstructions_AgentsMDReadError(t *testing.T) {
	tmpDir := t.TempDir()
	threshold := 4000
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")

	// Create a directory with the same name to cause ReadFile to fail
	err := os.Mkdir(agentsPath, 0755)
	require.NoError(t, err)

	_, err = CheckInstructions(tmpDir, &threshold)
	require.Error(t, err)
}

func TestCheckInstructions_InstructionsDirReadError(t *testing.T) {
	tmpDir := t.TempDir()
	threshold := 4000
	instDir := filepath.Join(tmpDir, ".agent-layer", "instructions")

	// Create a file instead of a directory to cause ReadDir to fail
	err := os.MkdirAll(filepath.Join(tmpDir, ".agent-layer"), 0755)
	require.NoError(t, err)
	err = os.WriteFile(instDir, []byte("not a directory"), 0644)
	require.NoError(t, err)

	_, err = CheckInstructions(tmpDir, &threshold)
	require.Error(t, err)
}
