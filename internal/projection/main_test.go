package projection

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

// The projected source fixture follows al's development environment setup and
// invokes a nested al through PATH. No installed al or provider is involved.
func TestMain(m *testing.M) {
	if os.Getenv("AL_TEST_PROJECTED_SOURCE") == "1" {
		if err := versiondispatch.ConfigureDevelopmentEnvironment(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if len(os.Args) > 1 && os.Args[1] == "sync" {
			fmt.Print("source")
			os.Exit(0)
		}
		if versiondispatch.DevelopmentExecutable(os.Getenv("AL_DEV_SOURCE_ROOT")) == "" {
			os.Exit(2)
		}
		cmd := exec.Command("al", "sync")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
