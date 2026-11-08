package skillimport

import (
	"fmt"
	"os"
	"path"

	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
)

// instructionAddConfig prepares exact-file adoption for the shared add transaction.
func instructionAddConfig(st *state, opts AddOptions, block config.SkillImport) (string, error) {
	name := path.Base(opts.Selectors[0])
	old := skilljournal.LegacyInstruction(name)
	if old != "" {
		source := filepath.Join(st.paths.InstructionsDir, old)
		if _, err := os.Lstat(source); err == nil {
			return "", fmt.Errorf(messages.LegacyInstructionConflictFmt, source)
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return config.AddInstructionImport(st.configRaw, block, opts.Order)
}
