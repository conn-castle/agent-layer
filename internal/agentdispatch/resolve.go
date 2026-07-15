package agentdispatch

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os/exec"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

type resolution struct {
	Target  targetMeta
	Version string
	Notice  string
}

func resolveTarget(cfg config.Config, req RunOptions, caller string, callerKnown bool) (resolution, error) {
	requested := normalizeAgent(req.Agent)
	if !validTargetOrRandom(requested) {
		return resolution{}, exitError(ExitUsage, fmt.Sprintf(messages.DispatchUnknownTargetFmt, req.Agent))
	}
	if requested == "" {
		if !callerKnown {
			return resolution{}, exitError(ExitUsage, messages.DispatchUnknownCallerRequiresAgent)
		}
		requested = dispatchDefaultForCaller(cfg, caller)
	}
	if requested == AgentRandom {
		selected, err := chooseRandomTarget(cfg, req.Root, caller, callerKnown, req.LookPath, req.VersionLookup, req.ChooseRandom)
		if err != nil {
			return resolution{}, err
		}
		return resolution{
			Target:  selected.Target,
			Version: selected.InstalledVersion,
			Notice:  fmt.Sprintf(messages.DispatchTargetRandomFmt, selected.Target.Name),
		}, nil
	}
	target, ok := lookupTarget(requested)
	if !ok {
		return resolution{}, exitError(ExitUsage, fmt.Sprintf(messages.DispatchUnknownTargetFmt, req.Agent))
	}
	resolved := resolution{Target: target}
	if req.Agent == "" {
		resolved.Notice = fmt.Sprintf(messages.DispatchTargetImplicitFmt, target.Name, caller)
	}
	return resolved, nil
}

func chooseRandomTarget(cfg config.Config, root string, caller string, callerKnown bool, lookPath func(string) (string, error), versionLookup func(string, string) (string, error), chooser RandomChooser) (targetDiscovery, error) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	pool := make([]string, 0, len(targetRegistry()))
	discovered := make(map[string]targetDiscovery, len(targetRegistry()))
	discoverVersion := rawTargetVersionDiscovery(versionLookup)
	if versionLookup == nil {
		discoverVersion = func(path string, target targetMeta) (string, string, error) {
			_, installed, err := compatibleTargetVersionCached(root, path, target, nil)
			if err != nil {
				// Unsupported versions are intentionally absent from the cache.
				// Read them raw so random exclusion still reports the installed
				// version and canonical compatibility reason.
				return rawTargetVersionDiscovery(nil)(path, target)
			}
			warning, err := providerVersionCompatibility(target.Name, installed)
			return installed, warning, err
		}
	}
	for _, target := range targetRegistry() {
		if callerKnown && target.Name == caller {
			continue
		}
		if !targetEnabled(cfg, target.Name) {
			continue
		}
		facts := discoverTarget(cfg, target, caller, callerKnown, lookPath, discoverVersion)
		if facts.RandomEligible {
			pool = append(pool, target.Name)
			discovered[target.Name] = facts
		}
	}
	if len(pool) == 0 {
		return targetDiscovery{}, exitError(ExitUnavailable, messages.DispatchEmptyRandomPool)
	}
	if chooser == nil {
		chooser = defaultRandomChooser
	}
	selected, err := chooser(pool)
	if err != nil {
		return targetDiscovery{}, wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchInternalErrorFmt, err), err)
	}
	normalized := normalizeAgent(selected)
	if _, ok := lookupTarget(normalized); !ok {
		invalidErr := fmt.Errorf("random chooser returned invalid target %q", selected)
		return targetDiscovery{}, wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchInternalErrorFmt, invalidErr), invalidErr)
	}
	// A custom chooser must stay inside the pool the resolver computed so
	// it can't bypass caller-exclusion or availability constraints by
	// returning some other registered target name.
	inPool := false
	for _, candidate := range pool {
		if candidate == normalized {
			inPool = true
			break
		}
	}
	if !inPool {
		ineligibleErr := fmt.Errorf("random chooser returned target %q which is not in the eligible pool", selected)
		return targetDiscovery{}, wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchInternalErrorFmt, ineligibleErr), ineligibleErr)
	}
	return discovered[normalized], nil
}

func defaultRandomChooser(pool []string) (string, error) {
	if len(pool) == 0 {
		return "", errors.New("random chooser requires at least one eligible target")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(pool))))
	if err != nil {
		return "", err
	}
	return pool[n.Int64()], nil
}
