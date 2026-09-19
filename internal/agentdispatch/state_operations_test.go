package agentdispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestStateRejectsMalformedMappingsAndRecords(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"", "Upper", "two_words", "../escape"} {
		if _, err := sessionPath(root, name); err == nil {
			t.Fatalf("sessionPath accepted %q", name)
		}
	}

	stateDir := dispatchStatePath(root)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create state: %v", err)
	}
	name := "tiny-round-capacitor"
	if err := os.WriteFile(filepath.Join(stateDir, name+".json"), []byte(`{"name":"tiny-round-capacitor","agent":"codex","extra":true}`), 0o600); err != nil {
		t.Fatalf("write malformed mapping: %v", err)
	}
	if _, err := loadSession(root, name); err == nil {
		t.Fatal("loadSession accepted unknown JSON fields")
	} else {
		requireDispatchExitCode(t, err, ExitConfig)
	}
	if err := os.Remove(filepath.Join(stateDir, name+".json")); err != nil {
		t.Fatalf("remove malformed mapping: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "INVALID.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write invalid filename: %v", err)
	}
	if _, err := listSessions(root); err == nil {
		t.Fatal("listSessions accepted an invalid state filename")
	}

	runID := "11111111-1111-4111-8111-111111111111"
	runDir := filepath.Join(dispatchRunPath(root), runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("create run directory: %v", err)
	}
	if err := writeJSONAtomic(filepath.Join(runDir, dispatchRunFile), RunRecord{ID: "22222222-2222-4222-8222-222222222222"}); err != nil {
		t.Fatalf("write mismatched record: %v", err)
	}
	if _, err := loadRunRecord(root, runID); err == nil {
		t.Fatal("loadRunRecord accepted a mismatched ID")
	} else {
		requireDispatchExitCode(t, err, ExitConfig)
	}
}

func TestRunEvidencePersistsTerminalMetadataOnlyChange(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run.Record.State = dispatchStateFailed
	run.Record.CompletedAt = &now
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	updated, err := updateRunEvidence(run.Dir, func(record *RunRecord) error {
		record.RecoveryState = recoveryAcceptanceUnknown
		record.TerminalReason = "provider termination was not proven"
		record.TerminalExitCode = ExitTargetFailure
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	durable, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if durable.Revision != updated.Revision || durable.RecoveryState != recoveryAcceptanceUnknown || durable.TerminalReason != "provider termination was not proven" || durable.TerminalExitCode != ExitTargetFailure {
		t.Fatalf("terminal metadata was not persisted: %#v", durable)
	}
}

func TestReservationDoesNotOverwriteCollidingName(t *testing.T) {
	root := t.TempDir()
	originalSizes, originalShapes, originalElectrical := nameSizes, nameShapes, nameElectrical
	t.Cleanup(func() { nameSizes, nameShapes, nameElectrical = originalSizes, originalShapes, originalElectrical })
	nameSizes, nameShapes, nameElectrical = []string{"x"}, []string{"y"}, []string{"z"}

	stateDir := dispatchStatePath(root)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create state: %v", err)
	}
	collision := filepath.Join(stateDir, "x-y-z.json")
	const original = `{"name":"x-y-z","agent":"codex"}`
	if err := os.WriteFile(collision, []byte(original), 0o600); err != nil {
		t.Fatalf("write collision: %v", err)
	}
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], "fresh")
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	if _, err := reserveSession(root, run, testDispatchSessionRetention); err == nil {
		t.Fatal("reserveSession overwrote an existing mapping")
	} else {
		requireDispatchExitCode(t, err, ExitConfig)
		message := err.Error()
		for _, want := range []string{
			"could not allocate a unique dispatch name",
			"1 retained conversations",
			"0 active executions",
			"1-name pool",
			config.DispatchSessionRetentionDaysFieldKey,
		} {
			if !strings.Contains(message, want) {
				t.Fatalf("exhaustion error %q missing %q", message, want)
			}
		}
	}
	data, err := os.ReadFile(collision) // #nosec G304 -- collision is a test-controlled path inside t.TempDir.
	if err != nil || string(data) != original {
		t.Fatalf("collision changed: %q, %v", data, err)
	}
}

func TestGrokSessionRoundTripsThroughStateStore(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentGrok, supportedProviderVersions[AgentGrok], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run, testDispatchSessionRetention)
	if err != nil {
		t.Fatalf("reserve session: %v", err)
	}
	loaded, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.Agent != AgentGrok || loaded.Name != session.Name {
		t.Fatalf("loaded session = %#v", loaded)
	}
	sessions, err := listSessions(root)
	if err != nil || len(sessions) != 1 || sessions[0].Name != session.Name {
		t.Fatalf("list sessions = %#v, %v", sessions, err)
	}
}

func TestNewDispatchRunAdvertisesEventsAndOnlyApplicableLineage(t *testing.T) {
	root := t.TempDir()
	structured, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new structured run: %v", err)
	}
	if structured.Record.EventsPath != filepath.Join(structured.Dir, "provider.events") {
		t.Fatalf("structured events path = %q", structured.Record.EventsPath)
	}
	if structured.Record.LineagePath != "" {
		t.Fatalf("Codex advertised Claude lineage path %q", structured.Record.LineagePath)
	}
	capableClaude, err := newDispatchRun(root, AgentClaude, "2.1.211", dispatchModeFresh)
	if err != nil {
		t.Fatalf("new capable Claude run: %v", err)
	}
	if capableClaude.Record.LineagePath != filepath.Join(capableClaude.Dir, "provider.lineage") {
		t.Fatalf("Claude lineage path = %q", capableClaude.Record.LineagePath)
	}
	oldClaude, err := newDispatchRun(root, AgentClaude, "2.1.210", dispatchModeFresh)
	if err != nil {
		t.Fatalf("new old Claude run: %v", err)
	}
	if oldClaude.Record.LineagePath != "" {
		t.Fatalf("old Claude advertised lineage path %q", oldClaude.Record.LineagePath)
	}

	antigravity, err := newDispatchRun(root, AgentAntigravity, supportedProviderVersions[AgentAntigravity], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new Antigravity run: %v", err)
	}
	if antigravity.Record.EventsPath != filepath.Join(antigravity.Dir, "provider.events") {
		t.Fatalf("Antigravity events path = %q", antigravity.Record.EventsPath)
	}
	data, err := os.ReadFile(filepath.Join(antigravity.Dir, dispatchRunFile)) // #nosec G304 -- test-owned run path.
	if err != nil {
		t.Fatalf("read Antigravity run record: %v", err)
	}
	if !strings.Contains(string(data), `"events_path"`) {
		t.Fatalf("Antigravity run record omitted its events artifact: %s", data)
	}
}

func TestDispatchSessionRetentionPrunesOnlyExpiredInactiveMappings(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	old := now.Add(-testDispatchSessionRetention - time.Hour)

	expired := Session{Name: "tiny-round-capacitor", Agent: AgentCodex, State: "durable", ProviderSessionID: runtimeSessionID, CreatedAt: old, LastUsedAt: old}
	current := Session{Name: "small-bright-resistor", Agent: AgentClaude, State: "durable", ProviderSessionID: runtimeSessionID, CreatedAt: old, LastUsedAt: now.Add(-time.Hour)}
	active := Session{Name: "large-steady-relay", Agent: AgentCodex, State: "durable", ProviderSessionID: runtimeSessionID, CreatedAt: old, LastUsedAt: old, RunID: runtimeSessionID}
	cancelledRunID := "22222222-2222-4222-8222-222222222222"
	cancelled := Session{Name: "short-curved-diode", Agent: AgentCodex, State: "durable", ProviderSessionID: runtimeSessionID, CreatedAt: old, LastUsedAt: old, RunID: cancelledRunID}
	for _, session := range []Session{expired, current, active, cancelled} {
		if err := persistSession(root, session); err != nil {
			t.Fatalf("persist %s: %v", session.Name, err)
		}
	}
	runDir := filepath.Join(dispatchRunPath(root), runtimeSessionID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("create active run: %v", err)
	}
	if err := writeJSONAtomic(filepath.Join(runDir, dispatchRunFile), RunRecord{ID: runtimeSessionID, State: dispatchStateRunning, RecoveryState: recoveryAcceptanceUnknown, PID: os.Getpid(), ProcessStartIdentity: processStartIdentity(os.Getpid())}); err != nil {
		t.Fatalf("write active run: %v", err)
	}
	cancelledRunDir := filepath.Join(dispatchRunPath(root), cancelledRunID)
	if err := os.MkdirAll(cancelledRunDir, 0o700); err != nil {
		t.Fatalf("create cancelled run: %v", err)
	}
	if err := writeJSONAtomic(filepath.Join(cancelledRunDir, dispatchRunFile), RunRecord{ID: cancelledRunID, State: dispatchStateCancelled, RecoveryState: recoveryAcceptanceUnknown, CompletedAt: &old}); err != nil {
		t.Fatalf("write cancelled run: %v", err)
	}
	if err := pruneExpiredSessions(root, now, testDispatchSessionRetention); err != nil {
		t.Fatalf("pruneExpiredSessions: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dispatchStatePath(root), expired.Name+".json")); !os.IsNotExist(err) {
		t.Fatalf("expired inactive mapping remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dispatchStatePath(root), cancelled.Name+".json")); err != nil {
		t.Fatalf("unconfirmed cancelled mapping was pruned: %v", err)
	}
	for _, path := range []string{
		filepath.Join(dispatchStatePath(root), current.Name+".json"),
		filepath.Join(dispatchStatePath(root), active.Name+".json"),
		filepath.Join(dispatchStatePath(root), cancelled.Name+".json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("retention removed preserved mapping %s: %v", path, err)
		}
	}

	corruptPath := filepath.Join(dispatchStatePath(root), "calm-amber-switch.json")
	if err := os.WriteFile(corruptPath, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt mapping: %v", err)
	}
	if err := pruneExpiredSessions(root, now, testDispatchSessionRetention); err == nil {
		t.Fatal("retention hid a corrupt mapping")
	}
	if _, err := os.Stat(corruptPath); err != nil {
		t.Fatalf("retention removed corrupt mapping: %v", err)
	}
}

func TestDispatchNameVocabularyStaysValidAndLargeEnough(t *testing.T) {
	assertList := func(t *testing.T, label string, words []string) {
		t.Helper()
		seen := make(map[string]bool, len(words))
		for _, word := range words {
			if word == "" || strings.Contains(word, "-") {
				t.Fatalf("%s token %q is empty or contains a hyphen", label, word)
			}
			for _, r := range word {
				if r < 'a' || r > 'z' {
					t.Fatalf("%s token %q is not [a-z]+", label, word)
				}
			}
			if seen[word] {
				t.Fatalf("%s duplicate token %q", label, word)
			}
			seen[word] = true
		}
	}
	assertList(t, "nameSizes", nameSizes)
	assertList(t, "nameShapes", nameShapes)
	assertList(t, "nameElectrical", nameElectrical)
	if len(nameSizes) != 37 {
		t.Fatalf("nameSizes length = %d, want 37", len(nameSizes))
	}
	if len(nameShapes) <= 37 {
		t.Fatalf("nameShapes length = %d, want colors and non-size adjectives beyond the original 37", len(nameShapes))
	}
	if len(nameElectrical) <= 37 {
		t.Fatalf("nameElectrical length = %d, want the approved extra parts", len(nameElectrical))
	}
	for _, word := range []string{"red", "blue", "gold", "quirky", "lucky"} {
		if !containsString(nameShapes, word) {
			t.Fatalf("nameShapes omitted requested non-size token %q", word)
		}
	}
	for _, word := range []string{
		"mosfet", "triac", "diac", "zener", "led", "photodiode", "optocoupler",
		"memristor", "comparator", "multiplexer", "battery", "servo", "stepper",
		"speaker", "microphone", "buzzer", "crystal", "antenna", "ferrite", "balun",
		"connector",
	} {
		if !containsString(nameElectrical, word) {
			t.Fatalf("nameElectrical omitted approved part %q", word)
		}
	}
	capacity := len(nameSizes) * len(nameShapes) * len(nameElectrical)
	if capacity < 50000 {
		t.Fatalf("name pool capacity %d is below 50000", capacity)
	}
	name, err := randomDispatchName()
	if err != nil {
		t.Fatalf("randomDispatchName: %v", err)
	}
	if !validDispatchName(name) {
		t.Fatalf("randomDispatchName returned invalid name %q", name)
	}
	if strings.Count(name, "-") != 2 {
		t.Fatalf("randomDispatchName %q does not have exactly two hyphens", name)
	}
}

func TestConfiguredRetentionPrunesInactiveMappingsEarlierThanThirtyDays(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	retention := 2 * 24 * time.Hour
	expired := Session{Name: "tiny-round-capacitor", Agent: AgentCodex, State: "durable", ProviderSessionID: runtimeSessionID, CreatedAt: now.Add(-10 * 24 * time.Hour), LastUsedAt: now.Add(-10 * 24 * time.Hour)}
	current := Session{Name: "small-bright-resistor", Agent: AgentClaude, State: "durable", ProviderSessionID: runtimeSessionID, CreatedAt: now.Add(-time.Hour), LastUsedAt: now.Add(-time.Hour)}
	for _, session := range []Session{expired, current} {
		if err := persistSession(root, session); err != nil {
			t.Fatalf("persist %s: %v", session.Name, err)
		}
	}
	if err := pruneDispatchEvidence(root, now, retention); err != nil {
		t.Fatalf("pruneDispatchEvidence: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dispatchStatePath(root), expired.Name+".json")); !os.IsNotExist(err) {
		t.Fatalf("mapping older than the configured window remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dispatchStatePath(root), current.Name+".json")); err != nil {
		t.Fatalf("mapping inside the configured window was pruned: %v", err)
	}
}

func TestConfiguredRetentionPrunesConfirmedTerminalEvidenceEarlierThanThirtyDays(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	retention := 2 * 24 * time.Hour
	old := now.Add(-10 * 24 * time.Hour)
	recent := now.Add(-time.Hour)
	makeRecord := func(state string, completed time.Time, confirmed bool) *dispatchRun {
		t.Helper()
		run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
		if err != nil {
			t.Fatal(err)
		}
		run.Record.State = state
		if state == dispatchStateCompleted {
			run.Record.RecoveryState = recoveryResumeRequired
		} else {
			run.Record.RecoveryState = recoveryAcceptanceUnknown
		}
		run.Record.CompletedAt = &completed
		if confirmed {
			run.Record.LaunchFenced = true
			run.Record.TerminationConfirmed = true
			run.Record.TerminationConfirmedAt = &completed
		}
		if err := writeJSONAtomic(filepath.Join(run.Dir, dispatchRunFile), run.Record); err != nil {
			t.Fatal(err)
		}
		return run
	}
	expired := makeRecord(dispatchStateCompleted, old, true)
	unconfirmed := makeRecord(dispatchStateCompleted, old, false)
	insideWindow := makeRecord(dispatchStateCompleted, recent, true)
	if err := pruneDispatchEvidence(root, now, retention); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(expired.Dir); !os.IsNotExist(err) {
		t.Fatalf("confirmed terminal evidence older than the configured window remains: %v", err)
	}
	for _, dir := range []string{unconfirmed.Dir, insideWindow.Dir} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("preserved evidence removed: %v", err)
		}
	}
}

func TestReserveSessionReportsClassifiedExhaustionForActiveAndRetainedOccupants(t *testing.T) {
	root := t.TempDir()
	originalSizes, originalShapes, originalElectrical := nameSizes, nameShapes, nameElectrical
	t.Cleanup(func() { nameSizes, nameShapes, nameElectrical = originalSizes, originalShapes, originalElectrical })
	nameSizes, nameShapes, nameElectrical = []string{"a", "b"}, []string{"x"}, []string{"y"}

	now := time.Now().UTC()
	activeRunID := runtimeSessionID
	active := Session{Name: "a-x-y", Agent: AgentCodex, State: "durable", ProviderSessionID: runtimeSessionID, CreatedAt: now, LastUsedAt: now, RunID: activeRunID, ActiveRunID: activeRunID, ActiveClaimKnown: true}
	retained := Session{Name: "b-x-y", Agent: AgentCodex, State: "durable", ProviderSessionID: runtimeSessionID, CreatedAt: now, LastUsedAt: now}
	for _, session := range []Session{active, retained} {
		if err := persistSession(root, session); err != nil {
			t.Fatalf("persist %s: %v", session.Name, err)
		}
	}
	runDir := filepath.Join(dispatchRunPath(root), activeRunID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("create active run: %v", err)
	}
	if err := writeJSONAtomic(filepath.Join(runDir, dispatchRunFile), RunRecord{ID: activeRunID, State: dispatchStateRunning, RecoveryState: recoveryAcceptanceUnknown, PID: os.Getpid(), ProcessStartIdentity: processStartIdentity(os.Getpid())}); err != nil {
		t.Fatalf("write active run: %v", err)
	}
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	_, err = reserveSession(root, run, testDispatchSessionRetention)
	if err == nil {
		t.Fatal("reserveSession allocated a name from a full in-pool set")
	}
	requireDispatchExitCode(t, err, ExitConfig)
	message := err.Error()
	for _, want := range []string{
		"could not allocate a unique dispatch name",
		"1 retained conversations",
		"1 active executions",
		"2-name pool",
		config.DispatchSessionRetentionDaysFieldKey,
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("exhaustion error %q missing %q", message, want)
		}
	}
}

func TestReserveSessionIgnoresOutOfPoolMappingsWhenAllocating(t *testing.T) {
	root := t.TempDir()
	originalSizes, originalShapes, originalElectrical := nameSizes, nameShapes, nameElectrical
	t.Cleanup(func() { nameSizes, nameShapes, nameElectrical = originalSizes, originalShapes, originalElectrical })
	nameSizes, nameShapes, nameElectrical = []string{"x"}, []string{"y"}, []string{"z"}

	stateDir := dispatchStatePath(root)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create state: %v", err)
	}
	outsider := filepath.Join(stateDir, "other-valid-name.json")
	if err := os.WriteFile(outsider, []byte(`{"name":"other-valid-name","agent":"codex"}`), 0o600); err != nil {
		t.Fatalf("write out-of-pool mapping: %v", err)
	}
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run, testDispatchSessionRetention)
	if err != nil {
		t.Fatalf("reserveSession treated an out-of-pool mapping as occupying the pool: %v", err)
	}
	if session.Name != "x-y-z" {
		t.Fatalf("reserved %q, want x-y-z", session.Name)
	}
}

func TestReserveSessionCountsUnreadableOccupantsWithoutFailingClosed(t *testing.T) {
	root := t.TempDir()
	originalSizes, originalShapes, originalElectrical := nameSizes, nameShapes, nameElectrical
	t.Cleanup(func() { nameSizes, nameShapes, nameElectrical = originalSizes, originalShapes, originalElectrical })
	nameSizes, nameShapes, nameElectrical = []string{"x"}, []string{"y"}, []string{"z"}

	stateDir := dispatchStatePath(root)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "x-y-z.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write unreadable mapping: %v", err)
	}
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	_, err = reserveSession(root, run, testDispatchSessionRetention)
	if err == nil {
		t.Fatal("reserveSession overwrote an unreadable mapping")
	}
	requireDispatchExitCode(t, err, ExitConfig)
	message := err.Error()
	for _, want := range []string{
		"could not allocate a unique dispatch name",
		"0 retained conversations",
		"0 active executions",
		"1 unreadable occupants",
		"1-name pool",
		config.DispatchSessionRetentionDaysFieldKey,
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("exhaustion error %q missing %q", message, want)
		}
	}
}
