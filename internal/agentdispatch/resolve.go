package agentdispatch

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os/exec"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

type resolution struct {
	Target targetMeta
	Notice string
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
		selected, err := chooseRandomTarget(cfg, caller, callerKnown, req.LookPath, req.ChooseRandom)
		if err != nil {
			return resolution{}, err
		}
		target, _ := lookupTarget(selected)
		return resolution{
			Target: target,
			Notice: fmt.Sprintf(messages.DispatchTargetRandomFmt, selected),
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

func chooseRandomTarget(cfg config.Config, caller string, callerKnown bool, lookPath func(string) (string, error), chooser RandomChooser) (string, error) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	var pool []string
	for _, target := range targetRegistry() {
		if callerKnown && target.Name == caller {
			continue
		}
		if !targetEnabled(cfg, target.Name) {
			continue
		}
		if _, err := lookPath(target.Binary); err != nil {
			continue
		}
		pool = append(pool, target.Name)
	}
	if len(pool) == 0 {
		return "", exitError(ExitUnavailable, messages.DispatchEmptyRandomPool)
	}
	if chooser == nil {
		chooser = defaultRandomChooser
	}
	selected, err := chooser(pool)
	if err != nil {
		return "", wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchInternalErrorFmt, err), err)
	}
	if _, ok := lookupTarget(selected); !ok {
		err := fmt.Errorf("random chooser returned invalid target %q", selected)
		return "", wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchInternalErrorFmt, err), err)
	}
	return selected, nil
}

func defaultRandomChooser(pool []string) (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(pool))))
	if err != nil {
		return "", err
	}
	return pool[n.Int64()], nil
}
