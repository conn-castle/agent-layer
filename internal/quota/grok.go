package quota

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type grokAccount struct {
	HasBuildAccess *bool             `json:"hasGrokCodeAccess"`
	Tier           string            `json:"subscriptionTier"`
	User           string            `json:"userId"`
	Principal      string            `json:"principalId"`
	Team           string            `json:"teamId"`
	Email          string            `json:"email"`
	UserBlock      json.RawMessage   `json:"userBlockedReason"`
	TeamBlocks     []json.RawMessage `json:"teamBlockedReasons"`
}

type grokBilling struct {
	Period *struct {
		Type  string          `json:"type"`
		Start json.RawMessage `json:"start"`
		End   json.RawMessage `json:"end"`
	} `json:"currentPeriod"`
	Percent       *float64        `json:"creditUsagePercent"`
	Unified       *bool           `json:"isUnifiedBillingUser"`
	Prepaid       *grokAmount     `json:"prepaidBalance"`
	TopUpMethod   string          `json:"topUpMethod"`
	MonthlyLimit  *grokAmount     `json:"monthlyLimit"`
	IncludedUsed  *grokAmount     `json:"includedUsed"`
	BillingStart  json.RawMessage `json:"billingPeriodStart"`
	BillingEnd    json.RawMessage `json:"billingPeriodEnd"`
	BillingCycle  string          `json:"billingCycle"`
	OnDemand      *bool           `json:"on_demand_enabled"`
	OnDemandCamel *bool           `json:"onDemandEnabled"`
	OnDemandCap   *grokAmount     `json:"onDemandCap"`
	OnDemandUsed  *grokAmount     `json:"onDemandUsed"`
	SpendCapped   *bool           `json:"spendLimitReached"`
}

type grokAmount struct {
	Amount *float64 `json:"val"`
}

func (v *grokAmount) UnmarshalJSON(raw []byte) error {
	var scalar float64
	if json.Unmarshal(raw, &scalar) == nil {
		v.Amount = &scalar
		return nil
	}
	type value grokAmount
	return json.Unmarshal(raw, (*value)(v))
}

type grokTopUp struct {
	Enabled    *bool `json:"enabled"`
	CapReached *bool `json:"capReached"`
}

func readGrok(ctx context.Context, h *httpReader, auth credential, r *Result) failure {
	headers := http.Header{userAgentHeader: {"grok-build"}}
	var billing struct {
		Config *grokBilling `json:"config"`
	}
	at, fail := h.fetch(ctx, auth, http.MethodGet, grokBase+"/billing?format=credits", "", headers, &billing)
	r.ObservedAt = at
	if fail.isAuth() {
		return fail
	}
	var user grokAccount
	_, accountFail := h.fetch(ctx, auth, http.MethodGet, grokBase+"/user?include=subscription", "", headers, &user)
	accountRead := accountFail.status == ""
	var accountAdmission Admission
	if accountRead {
		accountAdmission = grokAccountAdmission(user)
	}
	if principal := grokPrincipal(user.User, user.Principal, user.Team); accountRead && principal != "" {
		// An email or bare user ID cannot distinguish seats on different teams.
		aliases := []string{grokPrincipal(user.User, user.Principal, user.Team)}
		if user.Team == "" {
			aliases = append(aliases, user.User, user.Principal, user.Email)
		}
		r.Subscription = subscription(r.Agent, principal, user.Tier, true, aliases...)
	} else {
		r.identityFailure = accountFail
		r.Missing = append(r.Missing, "account_identity")
	}
	if fail.status != "" || billing.Config == nil {
		if accountAdmission.Blocked != nil && *accountAdmission.Blocked {
			r.Admission = accountAdmission
		}
		if fail.status == "" {
			fail = unavailable(reasonNoUsage)
		}
		return fail
	}
	cfg := billing.Config
	window := grokIncludedWindow(cfg)
	extra := grokOnDemand(cfg)
	if cfg.Unified != nil && *cfg.Unified || window.ID == weeklyID {
		extra = grokPrepaid(ctx, h, auth, cfg, headers, full(window.UsedPercent), r)
	}
	switch {
	case window.UsedPercent == nil:
	case !full(window.UsedPercent), extra != nil && *extra:
		window.Admission = knownAllowed()
	case extra != nil && !*extra:
		window.Admission = knownBlocked(window.ResetsAt)
	}
	pool := Pool{ID: "included", Scope: scopeAll, Admission: window.Admission, Windows: []Window{window}}
	r.Pools = append(r.Pools, pool)
	r.Admission = aggregate(accountAdmission, pool.Admission)
	r.ModelCoverage = coveragePartial
	if accountAdmission.Blocked != nil {
		r.ModelCoverage = coverageComplete
	}
	return failure{}
}

func grokAccountAdmission(user grokAccount) Admission {
	gates := []Admission{grokBlockAdmission(user.UserBlock)}
	for _, reason := range user.TeamBlocks {
		gates = append(gates, grokBlockAdmission(reason))
	}
	var access Admission
	if user.HasBuildAccess != nil {
		access.Blocked = ptr(!*user.HasBuildAccess)
	}
	return aggregate(append(gates, access)...)
}

func grokBlockAdmission(reason json.RawMessage) Admission {
	switch string(reason) {
	case "", "null", "0", `""`, `"NONE"`, `"UNSPECIFIED"`, `"USER_BLOCKED_REASON_UNSPECIFIED"`, `"TEAM_BLOCKED_REASON_UNSPECIFIED"`:
		return knownAllowed()
	}
	var enum int64
	if json.Unmarshal(reason, &enum) == nil {
		return Admission{Blocked: ptr(enum != 0)}
	}
	var name string
	if json.Unmarshal(reason, &name) == nil && name != "" {
		return knownBlocked(nil)
	}
	return Admission{}
}

func grokIncludedWindow(cfg *grokBilling) Window {
	id, state, length := "included", windowUnknown, time.Duration(0)
	var start, end *time.Time
	used := cfg.Percent
	if cfg.Period != nil {
		start, end = optionalTime(cfg.Period.Start), optionalTime(cfg.Period.End)
		switch cfg.Period.Type {
		case "USAGE_PERIOD_TYPE_WEEKLY", weeklyID:
			id, state, length = weeklyID, windowFixed, week
		case "USAGE_PERIOD_TYPE_MONTHLY", monthlyID:
			id, state = monthlyID, windowFixed
		case "USAGE_PERIOD_TYPE_ROLLING", "rolling":
			state = windowRolling
		}
	} else if (cfg.Unified == nil || !*cfg.Unified) && (strings.EqualFold(cfg.BillingCycle, monthlyID) || cfg.MonthlyLimit != nil) {
		id, state = monthlyID, windowFixed
		start, end = optionalTime(cfg.BillingStart), optionalTime(cfg.BillingEnd)
		if used == nil && cfg.MonthlyLimit != nil && cfg.MonthlyLimit.Amount != nil && *cfg.MonthlyLimit.Amount > 0 &&
			cfg.IncludedUsed != nil && cfg.IncludedUsed.Amount != nil {
			used = ptr(*cfg.IncludedUsed.Amount / *cfg.MonthlyLimit.Amount * 100)
		}
	}
	if start != nil && end != nil && end.After(*start) {
		length = end.Sub(*start)
	}
	window := newWindow(id, used, length, end, state)
	if window.State == windowFixed && window.StartsAt == nil && start != nil {
		window.StartsAt = start
	}
	return window
}

func grokOnDemand(cfg *grokBilling) *bool {
	enabled := cfg.OnDemand
	if enabled == nil {
		enabled = cfg.OnDemandCamel
	}
	switch {
	case enabled == nil || !*enabled:
		return enabled
	case cfg.OnDemandCap == nil || cfg.OnDemandCap.Amount == nil || cfg.OnDemandUsed == nil || cfg.OnDemandUsed.Amount == nil:
		return nil
	default:
		return ptr(*cfg.OnDemandUsed.Amount < *cfg.OnDemandCap.Amount)
	}
}

func grokPrepaid(ctx context.Context, h *httpReader, auth credential, cfg *grokBilling, headers http.Header, exhausted bool, r *Result) *bool {
	if cfg.SpendCapped != nil && *cfg.SpendCapped {
		// The cap can clear on a different schedule than included usage.
		return nil
	}
	if cfg.Prepaid != nil && cfg.Prepaid.Amount != nil && *cfg.Prepaid.Amount > 0 {
		if cfg.SpendCapped != nil {
			return ptr(true)
		}
		// A positive balance alone cannot establish that spending is permitted.
		return nil
	}
	if !exhausted {
		return nil
	}
	var rule *grokTopUp
	_, fail := h.fetch(ctx, auth, http.MethodGet, grokBase+"/auto-topup-rule", "", headers, &rule)
	if fail.status != "" {
		r.Missing = append(r.Missing, "extra_usage")
		return nil
	}
	if cfg.Prepaid != nil && cfg.Prepaid.Amount != nil && *cfg.Prepaid.Amount == 0 &&
		(rule == nil || rule.Enabled != nil && !*rule.Enabled) {
		return ptr(false)
	}
	return nil
}
