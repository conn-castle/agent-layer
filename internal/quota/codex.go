package quota

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"
)

const codexUsage = "https://chatgpt.com/backend-api/wham/usage"
const codexIncludedLimitReached = "rate_limit_reached"

type codexUsageResponse struct {
	AccountID  string                `json:"account_id"`
	UserID     string                `json:"user_id"`
	Email      string                `json:"email"`
	Tier       string                `json:"plan_type"`
	Standard   *codexLimit           `json:"rate_limit"`
	Review     *codexLimit           `json:"code_review_rate_limit"`
	Additional codexAdditionalLimits `json:"additional_rate_limits"`
	ModelUsage map[string]struct {
		Available          *bool           `json:"available"`
		AvailableAt        json.RawMessage `json:"available_at"`
		CreditsWouldEnable bool            `json:"credits_would_enable"`
	} `json:"model_usage"`
	Credits struct {
		HasCredits   *bool `json:"has_credits"`
		Unlimited    bool  `json:"unlimited"`
		OverageLimit bool  `json:"overage_limit_reached"`
	} `json:"credits"`
	SpendControl struct {
		Reached    bool `json:"reached"`
		Individual *struct {
			Reset json.RawMessage `json:"reset_at"`
		} `json:"individual_limit"`
	} `json:"spend_control"`
	LimitReached *struct {
		Kind string `json:"type"`
	} `json:"rate_limit_reached_type"`
}

type codexAdditionalLimits struct {
	reported bool
	limits   []struct {
		Meter string      `json:"metered_feature"`
		ID    string      `json:"limit_id"`
		Name  string      `json:"limit_name"`
		Model string      `json:"normal_model_slug"`
		Limit *codexLimit `json:"rate_limit"`
	}
}

func (a *codexAdditionalLimits) UnmarshalJSON(data []byte) error {
	a.reported = true
	return json.Unmarshal(data, &a.limits)
}

type codexLimit struct {
	Allowed      *bool        `json:"allowed"`
	LimitReached *bool        `json:"limit_reached"`
	Primary      *codexWindow `json:"primary_window"`
	Secondary    *codexWindow `json:"secondary_window"`
}

type codexWindow struct {
	Used     *float64        `json:"used_percent"`
	Duration json.RawMessage `json:"limit_window_seconds"`
	Reset    json.RawMessage `json:"reset_at"`
	Rolling  bool            `json:"is_rolling"`
	Started  *bool           `json:"has_started"`
}

func readCodex(ctx context.Context, h *httpReader, auth credential, r *Result) failure {
	headers := http.Header{userAgentHeader: {"codex_cli_rs"}}
	if auth.account != "" {
		headers.Set("ChatGPT-Account-Id", auth.account)
	}
	var data codexUsageResponse
	at, fail := h.fetch(ctx, auth, http.MethodGet, codexUsage, "", headers, &data)
	r.ObservedAt = at
	if fail.status != "" {
		return fail
	}
	r.Subscription = codexSubscription(data, auth.identity)
	if data.AccountID != "" && auth.account != "" && data.AccountID != auth.account {
		// The service reached a different workspace than the selected launch.
		return failure{status: statusMismatch, reason: reasonScopeMismatch, reauth: ptr(false)}
	}
	r.ModelCoverage = coverageComplete
	if !data.Additional.reported {
		r.ModelCoverage = coveragePartial
	}
	kind := ""
	if data.LimitReached != nil {
		kind = data.LimitReached.Kind
	}
	if data.Standard != nil {
		standard := *data.Standard
		if kind == codexIncludedLimitReached && standard.Allowed == nil {
			standard.LimitReached = ptr(true)
		}
		pool := makeCodexPool(agentCodex, scopeAll, nil, &standard)
		r.Admission = pool.Admission
		r.Pools = append(r.Pools, pool)
	} else {
		r.ModelCoverage = coveragePartial
		r.Missing = append(r.Missing, "included_limits")
	}
	if data.Review != nil {
		// The review surface is not selected by dispatch's model flag.
		r.Pools = append(r.Pools, makeCodexPool("code_review", scopeOther, nil, data.Review))
	}
	for _, limit := range data.Additional.limits {
		if limit.Limit == nil {
			r.ModelCoverage = coveragePartial
			continue
		}
		scope, models := scopeUnknown, []string(nil)
		if limit.Model != "" {
			scope, models = scopeModels, []string{limit.Model}
		} else {
			r.ModelCoverage = coveragePartial
		}
		pool := makeCodexPool(textFrom(limit.Meter, limit.ID, limit.Name, "additional"), scope, models, limit.Limit)
		pool.Label = limit.Name
		r.Pools = append(r.Pools, pool)
	}
	credits := codexCreditsUsable(data, kind)
	for id, state := range data.ModelUsage {
		gate := Admission{}
		modelCoverage := r.ModelCoverage
		bypassesIncluded := false
		if state.Available != nil {
			modelCoverage = coverageComplete
			switch {
			case *state.Available:
				gate = knownAllowed()
			case state.CreditsWouldEnable && credits != nil && *credits:
				gate = knownAllowed()
				bypassesIncluded = true
			case state.CreditsWouldEnable && credits == nil:
				bypassesIncluded = true
			default:
				gate = knownBlocked(optionalTime(state.AvailableAt))
			}
		}
		r.Models = append(r.Models, Model{
			ID:               id,
			Coverage:         modelCoverage,
			Admission:        gate,
			bypassesIncluded: bypassesIncluded,
		})
	}
	applyCodexAdmission(data, kind, r)
	return failure{}
}

func codexCreditsUsable(data codexUsageResponse, kind string) *bool {
	switch {
	case data.SpendControl.Reached, data.Credits.OverageLimit:
		return ptr(false)
	case kind != "" && kind != codexIncludedLimitReached:
		return ptr(false)
	case data.Credits.Unlimited, data.Credits.HasCredits != nil && *data.Credits.HasCredits:
		return ptr(true)
	case data.Credits.HasCredits != nil && !*data.Credits.HasCredits:
		return ptr(false)
	default:
		return nil
	}
}

func applyCodexAdmission(data codexUsageResponse, kind string, r *Result) {
	var clear *time.Time
	switch kind {
	case codexIncludedLimitReached:
		if r.Blocked != nil && !*r.Blocked {
			return
		}
		if r.Blocked == nil {
			r.Admission = knownBlocked(nil)
		}
		r.BlockReason = kind
		return
	case "workspace_member_usage_limit_reached":
		if data.SpendControl.Reached && data.SpendControl.Individual != nil {
			clear = optionalTime(data.SpendControl.Individual.Reset)
		}
	case "workspace_owner_credits_depleted", "workspace_member_credits_depleted", "workspace_owner_usage_limit_reached":
	default:
		return
	}
	// Billing and workspace caps are independent of included window resets.
	// An unknown expiry must survive even when an included window also blocks.
	block := knownBlocked(clear)
	r.independentBlock = aggregate(r.independentBlock, block)
	r.Admission = aggregate(r.Admission, block)
	r.BlockReason = kind
}

func codexSubscription(data codexUsageResponse, cached *Subscription) *Subscription {
	if data.AccountID == "" && cached != nil {
		return &Subscription{ID: cached.ID, Tier: data.Tier}
	}
	switch data.Tier {
	case "team", "business", "self_serve_business_prolite", "self_serve_business_usage_based", "free_workspace",
		"enterprise", "ent26", "enterprise_cbp_automation", "enterprise_cbp_usage_based",
		"education", "quorum", "k12", "edu", "edu_plus", "edu_pro":
		// Included allowances are seat-specific even when the workspace
		// pays for eligible overage through a shared credit balance.
		if data.AccountID == "" || data.UserID == "" {
			return subscription(agentCodex, data.AccountID, data.Tier, false)
		}
		seat := data.AccountID + "/" + data.UserID
		return subscription(agentCodex, seat, data.Tier, true, seat)
	case "guest", "free", "go", "plus", "pro", "prolite", "promax":
		return subscription(agentCodex, data.AccountID, data.Tier, true, data.AccountID, data.UserID, data.Email)
	default:
		principal := data.AccountID
		if principal != "" && data.UserID != "" {
			principal += "/" + data.UserID
		}
		return subscription(agentCodex, principal, data.Tier, false)
	}
}

func makeCodexPool(id, scope string, models []string, limit *codexLimit) Pool {
	p := Pool{ID: id, Scope: scope, Models: models, Windows: []Window{}}
	for i, raw := range []*codexWindow{limit.Primary, limit.Secondary} {
		if raw == nil {
			continue
		}
		state := windowFixed
		switch {
		case raw.Rolling:
			state = windowRolling
		case raw.Started != nil && !*raw.Started:
			state = windowPending
		}
		id := []string{"primary", "secondary"}[i]
		p.Windows = append(p.Windows, newWindow(id, raw.Used, durationSeconds(optionalInt64(raw.Duration)), optionalTime(raw.Reset), state))
	}
	switch {
	case limit.Allowed != nil && *limit.Allowed:
		p.Admission = knownAllowed()
	case limit.Allowed != nil && !*limit.Allowed || limit.LimitReached != nil && *limit.LimitReached:
		p.Admission = knownBlocked(nil)
	case limit.LimitReached != nil && !*limit.LimitReached:
		p.Admission = knownAllowed()
	}
	for i := range p.Windows {
		w := &p.Windows[i]
		switch {
		case p.Blocked == nil:
		case !*p.Blocked, w.State == windowPending, w.UsedPercent != nil && !full(w.UsedPercent):
			w.Admission = knownAllowed()
		case full(w.UsedPercent):
			w.Admission = knownBlocked(w.ResetsAt)
		}
	}
	if p.Blocked != nil && *p.Blocked && !slices.ContainsFunc(p.Windows, func(w Window) bool {
		return w.UsedPercent == nil && w.State != windowPending
	}) {
		if gates := windowAdmission(p.Windows); gates.Blocked != nil && *gates.Blocked {
			p.Admission = gates
		}
	}
	return p
}
