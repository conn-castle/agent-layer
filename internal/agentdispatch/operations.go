package agentdispatch

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Inspection is the stable factual shape emitted by inspect. It deliberately
// reports process transport facts without inferring provider health.
type Inspection struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	Agent                string     `json:"agent"`
	State                string     `json:"state"`
	Mode                 string     `json:"mode"`
	Attempt              int        `json:"attempt"`
	Process              string     `json:"process"`
	StartedAt            time.Time  `json:"started_at"`
	LastOutputAt         *time.Time `json:"last_output_at,omitempty"`
	ProviderConversation string     `json:"provider_conversation,omitempty"`
	NotResumable         bool       `json:"not_resumable,omitempty"`
	TerminalReason       string     `json:"terminal_reason,omitempty"`
	Artifacts            Artifacts  `json:"artifacts"`
}

// Artifacts identifies private diagnostic evidence without embedding it in
// normal command output.
type Artifacts struct {
	Answer      string `json:"answer"`
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	Events      string `json:"events,omitempty"`
	ProviderLog string `json:"provider_log,omitempty"`
}

// Inspect writes one factual dispatch inspection by current name or immutable
// run UUID. It does not mutate dispatch state or provider processes.
func Inspect(request InspectionRequest) error {
	stdout := writerOrDiscard(request.Stdout)
	inspection, err := resolveInspection(request.Root, strings.TrimSpace(request.ID))
	if err != nil {
		return err
	}
	if request.JSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(inspection)
	}
	_, err = fmt.Fprintf(stdout, "Dispatch: %s\nAgent: %s\nState: %s\nProcess: %s\nAttempt: %d\nStarted: %s\n", inspection.Name, inspection.Agent, inspection.State, inspection.Process, inspection.Attempt, inspection.StartedAt.Format(time.RFC3339))
	if err != nil {
		return err
	}
	if inspection.LastOutputAt != nil {
		if _, err := fmt.Fprintf(stdout, "Last output: %s\n", inspection.LastOutputAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	conversation := inspection.ProviderConversation
	if conversation == "" {
		conversation = "pending"
	}
	if _, err := fmt.Fprintf(stdout, "Provider conversation: %s\n", conversation); err != nil {
		return err
	}
	if inspection.NotResumable {
		if _, err := fmt.Fprintln(stdout, "Provider status: not resumable"); err != nil {
			return err
		}
	}
	if inspection.TerminalReason != "" {
		if _, err := fmt.Fprintf(stdout, "Terminal reason: %s\n", inspection.TerminalReason); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(stdout, "Diagnostics: stdout=%s stderr=%s", inspection.Artifacts.Stdout, inspection.Artifacts.Stderr)
	if inspection.Artifacts.ProviderLog != "" {
		_, err = fmt.Fprintf(stdout, " provider_log=%s", inspection.Artifacts.ProviderLog)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout)
	return err
}

func resolveInspection(root string, id string) (Inspection, error) {
	if id == "" {
		return Inspection{}, exitError(ExitUsage, "dispatch inspect requires a friendly name or run UUID")
	}
	var record RunRecord
	if err := parseUUID(id); err == nil {
		loaded, loadErr := loadRunRecord(root, id)
		if loadErr != nil {
			return Inspection{}, loadErr
		}
		record = loaded
	} else {
		session, loadErr := loadSession(root, id)
		if loadErr != nil {
			return Inspection{}, loadErr
		}
		if session.RunID == "" {
			return Inspection{}, exitError(ExitConfig, fmt.Sprintf("dispatch session %q has no run record", session.Name))
		}
		loaded, recordErr := loadRunRecord(root, session.RunID)
		if recordErr != nil {
			return Inspection{}, recordErr
		}
		record = loaded
	}
	return inspectionFromRecord(record), nil
}

func inspectionFromRecord(record RunRecord) Inspection {
	return Inspection{
		ID:                   record.ID,
		Name:                 record.Name,
		Agent:                record.Agent,
		State:                record.State,
		Mode:                 record.Mode,
		Attempt:              record.Attempt,
		Process:              processAlive(record.PID),
		StartedAt:            record.StartedAt,
		LastOutputAt:         record.LastOutputAt,
		ProviderConversation: record.ProviderSessionID,
		NotResumable:         record.NotResumable,
		TerminalReason:       record.TerminalReason,
		Artifacts: Artifacts{
			Answer:      record.AnswerPath,
			Stdout:      record.StdoutPath,
			Stderr:      record.StderrPath,
			Events:      record.EventsPath,
			ProviderLog: record.ProviderLogPath,
		},
	}
}

// List writes all current name-keyed reservations and durable conversations.
func List(request ListRequest) error {
	stdout := writerOrDiscard(request.Stdout)
	sessions, err := listSessions(request.Root)
	if err != nil {
		return err
	}
	if request.JSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(sessions)
	}
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(stdout, "No dispatch sessions.")
		return err
	}
	for _, session := range sessions {
		if _, err := fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", session.Name, session.Agent, session.State, session.LastUsedAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

// Delete releases an inactive Agent Layer mapping. Provider conversations and
// transcripts are intentionally untouched.
func Delete(root string, name string) error {
	name = strings.TrimSpace(name)
	session, err := loadSession(root, name)
	if err != nil {
		return err
	}
	if session.RunID != "" {
		record, recordErr := loadRunRecord(root, session.RunID)
		if recordErr == nil && (record.State == "pending" || record.State == "starting" || (record.State == "running" && processAlive(record.PID) == "alive")) {
			return exitError(ExitUnavailable, fmt.Sprintf("dispatch session %q is active; inspect it or wait for terminal completion before deleting the mapping", name))
		}
	}
	return deleteSession(root, name)
}
