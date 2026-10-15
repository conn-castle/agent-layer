package agentdispatch

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/conn-castle/agent-layer/internal/executableimage"
)

type agentLayerBuildIdentity struct {
	Executable    string `json:"executable,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	ModuleVersion string `json:"module_version,omitempty"`
	GoVersion     string `json:"go_version,omitempty"`
	Revision      string `json:"revision,omitempty"`
	Modified      string `json:"modified,omitempty"`
}

var buildIdentityOnce sync.Once
var executingBuild agentLayerBuildIdentity

func currentBuildIdentity() agentLayerBuildIdentity {
	buildIdentityOnce.Do(func() {
		executingBuild.Executable, _ = os.Executable()
		executable, _ := executableimage.Self()
		file, err := os.Open(executable) // #nosec G304 -- own executable, no caller-controlled path.
		if err == nil {
			h := sha256.New()
			if _, err := io.Copy(h, file); err == nil {
				executingBuild.SHA256 = hex.EncodeToString(h.Sum(nil))
			}
			_ = file.Close()
		}
		if info, ok := debug.ReadBuildInfo(); ok {
			executingBuild.ModuleVersion = info.Main.Version
			executingBuild.GoVersion = info.GoVersion
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					executingBuild.Revision = setting.Value
				case "vcs.modified":
					executingBuild.Modified = setting.Value
				}
			}
		}
	})
	return executingBuild
}

// Fixed categories, IDs and local observations only. Never copy a provider
// error, prompt, answer, command arguments or tool payload into this snapshot.
type providerLifecycleSnapshot struct {
	ObservedAt           time.Time               `json:"observed_at"`
	Trigger              string                  `json:"trigger"`
	TriggeredAt          *time.Time              `json:"triggered_at,omitempty"`
	Deadline             *time.Time              `json:"deadline,omitempty"`
	SemanticTerminal     bool                    `json:"semantic_terminal"`
	PendingWorkflow      bool                    `json:"pending_workflow"`
	LeaderReaped         bool                    `json:"leader_reaped"`
	LeaderReapedKnown    bool                    `json:"leader_reaped_known"`
	LeaderObservation    string                  `json:"leader_observation"`
	ExitStatus           string                  `json:"exit_status,omitempty"`
	GroupProof           string                  `json:"group_proof,omitempty"`
	GroupObservation     string                  `json:"group_observation"`
	TerminationRequested bool                    `json:"termination_requested"`
	StdoutWritersClosed  bool                    `json:"stdout_writers_closed"`
	StderrWritersClosed  bool                    `json:"stderr_writers_closed"`
	StdoutDrained        bool                    `json:"stdout_drained"`
	StdoutReaderDone     bool                    `json:"stdout_reader_done"`
	StderrDrained        bool                    `json:"stderr_drained"`
	StderrReaderDone     bool                    `json:"stderr_reader_done"`
	LastReceivedAt       *time.Time              `json:"last_received_at,omitempty"`
	RootID               string                  `json:"root_id,omitempty"`
	CandidateRootID      string                  `json:"candidate_root_id,omitempty"`
	BackgroundID         string                  `json:"background_id,omitempty"`
	Cursor               string                  `json:"cursor,omitempty"`
	Sequence             string                  `json:"sequence,omitempty"`
	LineageSource        string                  `json:"lineage_source,omitempty"`
	Recovery             *museRecoveryBaseline   `json:"recovery,omitempty"`
	RootEvidence         *museRecoveryBaseline   `json:"root_evidence,omitempty"`
	Build                agentLayerBuildIdentity `json:"agent_layer_build"`
}

func processStatus(pid int, start string) string {
	if pid <= 0 || start == "" {
		return statusUnknown
	}
	current := processStartIdentity(pid)
	if current == "" {
		if processAlive(pid) == processStatusDead {
			return processStatusDead
		}
		return statusUnknown
	}
	if current != start {
		return processStatusDead
	}
	if processIsZombie(pid) {
		return "zombie"
	}
	return processStatusAlive
}
