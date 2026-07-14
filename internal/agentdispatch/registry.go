package agentdispatch

import (
	"strings"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
)

const (
	answerTextPartial = "partial"
	answerTextFinal   = "final"
	progressPartial   = "partial"
	progressNone      = "none"
)

type targetMeta struct {
	Name               string
	Binary             string
	SkillPrefix        string
	SharedSkillProject bool
	AnswerText         string
	Progress           string
}

func targetRegistry() []targetMeta {
	return []targetMeta{
		{
			Name:               AgentCodex,
			Binary:             "codex",
			SkillPrefix:        "$",
			SharedSkillProject: true,
			AnswerText:         answerTextFinal,
			Progress:           progressPartial,
		},
		{
			Name:        AgentClaude,
			Binary:      AgentClaude,
			SkillPrefix: "/",
			AnswerText:  answerTextPartial,
			Progress:    progressPartial,
		},
		{
			Name:               AgentAntigravity,
			Binary:             "agy",
			SkillPrefix:        "/",
			SharedSkillProject: true,
			AnswerText:         answerTextPartial,
			Progress:           progressNone,
		},
	}
}

func lookupTarget(name string) (targetMeta, bool) {
	normalized := normalizeAgent(name)
	for _, target := range targetRegistry() {
		if target.Name == normalized {
			return target, true
		}
	}
	return targetMeta{}, false
}

func normalizeAgent(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func validTargetOrRandom(name string) bool {
	switch normalizeAgent(name) {
	case "", AgentRandom, AgentCodex, AgentClaude, AgentAntigravity:
		return true
	default:
		return false
	}
}

func knownCallerFromEnv(env []string) (string, bool) {
	value, ok := clients.GetEnv(env, clients.EnvDispatchCallerAgent)
	if !ok {
		return "", false
	}
	switch normalizeAgent(value) {
	case AgentCodex:
		return AgentCodex, true
	case AgentClaude:
		return AgentClaude, true
	case AgentAntigravity:
		return AgentAntigravity, true
	default:
		return "", false
	}
}

func targetEnabled(cfg config.Config, target string) bool {
	switch target {
	case AgentCodex:
		return config.IsAgentEnabled(cfg.Agents.Codex.Enabled)
	case AgentClaude:
		return config.IsAgentEnabled(cfg.Agents.Claude.Enabled)
	case AgentAntigravity:
		return config.IsAgentEnabled(cfg.Agents.Antigravity.Enabled)
	default:
		return false
	}
}

func dispatchDefaultForCaller(cfg config.Config, caller string) string {
	var value string
	switch caller {
	case AgentCodex:
		value = cfg.Agents.Codex.Dispatch.DefaultAgent
	case AgentClaude:
		value = cfg.Agents.Claude.Dispatch.DefaultAgent
	case AgentAntigravity:
		value = cfg.Agents.Antigravity.Dispatch.DefaultAgent
	}
	value = normalizeAgent(value)
	if value == "" {
		return AgentRandom
	}
	return value
}
