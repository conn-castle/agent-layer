package install

// templateManager owns template surface discovery, matching, diffing, and writes.
type templateManager struct {
	*installer
}

// ownershipClassifier owns baseline-backed ownership classification decisions.
type ownershipClassifier struct {
	*installer
}

// upgradeOrchestrator owns multi-step upgrade transaction coordination.
type upgradeOrchestrator struct {
	*installer
}

func (inst *installer) templates() templateManager {
	return templateManager{installer: inst}
}

func (inst *installer) ownership() ownershipClassifier {
	return ownershipClassifier{installer: inst}
}

func (inst *installer) upgrades() upgradeOrchestrator {
	return upgradeOrchestrator{installer: inst}
}
