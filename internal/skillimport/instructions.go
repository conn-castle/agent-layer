package skillimport

import (
	"fmt"
	"os"
	"path"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// instructionAddConfig prepares exact-file adoption for the shared add transaction.
func instructionAddConfig(st *state, opts AddOptions, block config.SkillImport, legacy map[string]skilltree.Tree) (string, error) {
	name := path.Base(opts.Selectors[0])
	localName := ""
	old := skilljournal.LegacyInstruction(name)
	if old != "" {
		source := pathSetFor(st).LocalPath(name)
		if _, err := os.Lstat(source); err == nil {
			if !opts.AdoptLegacy {
				return "", fmt.Errorf("legacy %s exists; run al wizard to adopt it without duplicating instructions", old)
			}
			tree, err := skilltree.ReadFileNode(skilltree.OSFS{}, source, name)
			if err != nil {
				return "", err
			}
			localName = old
			legacy[name] = tree
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return config.AddInstructionImport(st.configRaw, block, opts.Order, localName)
}
