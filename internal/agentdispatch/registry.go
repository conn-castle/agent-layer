package agentdispatch

import (
	"strings"

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
			Binary:             AgentCodex,
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
