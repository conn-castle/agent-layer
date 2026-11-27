package quota

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	claudeOAuth        = "https://api.anthropic.com/api/oauth/"
	claudeOpusWeekly   = "seven_day_opus"
	claudeSonnetWeekly = "seven_day_sonnet"
)

type claudeUsageResponse struct {
	FiveHour          *claudeWindow            `json:"five_hour"`
	SevenDay          *claudeWindow            `json:"seven_day"`
	SevenDayOpus      *claudeWindow            `json:"seven_day_opus"`
	SevenDaySonnet    *claudeWindow            `json:"seven_day_sonnet"`
	SevenDayCowork    *claudeWindow            `json:"seven_day_cowork"`
	SevenDayOAuthApps *claudeWindow            `json:"seven_day_oauth_apps"`
	Limits            []claudeLimit            `json:"limits"`
	Extra             *claudeExtra             `json:"extra_usage"`
	LegacyCounters    map[string]*claudeWindow `json:"-"`
	fiveHourReported  bool
	sevenDayReported  bool
}

type claudeWindow struct {
	Utilization  *float64        `json:"utilization"`
	Reset        json.RawMessage `json:"resets_at"`
	LockedReason string          `json:"locked_reason"`
	State        string          `json:"window_state"`
}

type claudeLimit struct {
	Kind         string          `json:"kind"`
	Group        string          `json:"group"`
	Percent      *float64        `json:"percent"`
	Severity     string          `json:"severity"`
	Reset        json.RawMessage `json:"resets_at"`
	LockedReason string          `json:"locked_reason"`
	State        string          `json:"window_state"`
	Scope        struct {
		Surface string `json:"surface"`
		Model   struct {
			ID      string `json:"id"`
			Display string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

type claudeExtra struct {
	Enabled       *bool  `json:"is_enabled"`
	UserDisabled  bool   `json:"user_disabled"`
	SpendLimit    bool   `json:"spend_limit_reached"`
	CreditsCanPay string `json:"credits_can_pay"`
}

func readClaude(ctx context.Context, h *httpReader, auth credential, r *Result) failure {
	headers := http.Header{}
	headers.Set("anthropic-beta", "oauth-2025-04-20")
	headers.Set(userAgentHeader, "claude-code/2.1.207")
	var data claudeUsageResponse
	at, fail := h.fetch(ctx, auth, http.MethodGet, claudeOAuth+"usage", "", headers, &data)
	r.ObservedAt = at
	if fail.status != "" {
		return fail
	}
	normalizeClaude(data, r)
	var profile struct {
		Account struct {
			ID    string `json:"uuid"`
			Email string `json:"email"`
		} `json:"account"`
		Organization struct {
			ID   string `json:"uuid"`
			Type string `json:"organization_type"`
		} `json:"organization"`
	}
	_, fail = h.fetch(ctx, auth, http.MethodGet, claudeOAuth+"profile", "", headers, &profile)
	if fail.status != "" || profile.Account.ID == "" || profile.Organization.ID == "" {
		r.identityFailure = fail
		r.Missing = append(r.Missing, "account_identity")
	} else {
		tier := profile.Organization.Type
		if r.Subscription != nil {
			tier = textFrom(r.Subscription.Tier, tier)
		}
		principal := claudePrincipal(profile.Organization.ID, profile.Account.ID)
		aliases := []string{principal}
		switch profile.Organization.Type {
		case "claude_free", "claude_pro", "claude_max":
			aliases = append(aliases, profile.Account.ID, profile.Account.Email)
		}
		r.Subscription = subscription(agentClaude, principal, tier, true, aliases...)
	}
	return failure{}
}

func normalizeClaude(data claudeUsageResponse, r *Result) {
	r.ModelCoverage = coveragePartial
	r.Pools = append(r.Pools, Pool{ID: agentClaude, Scope: scopeAll, Windows: []Window{}})
	for _, item := range []struct {
		id, model, scope string
		duration         time.Duration
		window           *claudeWindow
		reported         bool
	}{
		{fiveHourID, "", scopeAll, hours5, data.FiveHour, data.fiveHourReported},
		{"seven_day", "", scopeAll, week, data.SevenDay, data.sevenDayReported},
		{claudeOpusWeekly, "opus", scopeModels, week, data.SevenDayOpus, true},
		{claudeSonnetWeekly, "sonnet", scopeModels, week, data.SevenDaySonnet, true},
		{"seven_day_cowork", "", scopeOther, week, data.SevenDayCowork, true},
		{"seven_day_oauth_apps", "", scopeOther, week, data.SevenDayOAuthApps, true},
	} {
		if item.window == nil {
			if !item.reported {
				w := newWindow(item.id, nil, item.duration, nil, windowUnknown)
				r.Pools[0].Windows = append(r.Pools[0].Windows, w)
			}
			continue
		}
		window := makeClaudeWindow(item.id, item.window.Utilization, item.duration, optionalTime(item.window.Reset), item.window.State,
			item.window.LockedReason, "", data.Extra, item.scope != scopeAll)
		if item.scope == scopeAll {
			r.Pools[0].Windows = append(r.Pools[0].Windows, window)
		} else {
			pool := Pool{ID: item.id, Scope: item.scope, Windows: []Window{window}}
			if item.model != "" {
				pool.Models = []string{item.model}
			}
			r.Pools = append(r.Pools, pool)
		}
	}
	keys := make([]string, 0, len(data.LegacyCounters))
	for id := range data.LegacyCounters {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	for _, id := range keys {
		raw := data.LegacyCounters[id]
		window := makeClaudeWindow(id, raw.Utilization, week, optionalTime(raw.Reset), raw.State, raw.LockedReason, "", data.Extra, true)
		r.Pools = append(r.Pools, Pool{ID: id, Scope: scopeUnknown, Windows: []Window{window}})
	}
	for _, limit := range data.Limits {
		addClaudeLimit(r, limit, data.Extra)
	}
	for i := range r.Pools {
		r.Pools[i].Admission = windowAdmission(r.Pools[i].Windows)
	}
	r.Admission = r.Pools[0].Admission
	if r.Blocked != nil && !slices.ContainsFunc(r.Pools, func(pool Pool) bool { return pool.Scope == scopeUnknown }) {
		r.ModelCoverage = coverageComplete
	}
}

func addClaudeLimit(r *Result, limit claudeLimit, extra *claudeExtra) {
	id, model := limit.Kind, textFrom(limit.Scope.Model.ID, modelName(limit.Scope.Model.Display))
	if id == "" {
		return
	}
	duration := claudeDuration(limit.Kind, limit.Group)
	if limit.Scope.Surface == "" && (limit.Kind == "weekly_all" || limit.Kind == "session_all" || limit.Kind == fiveHourID) {
		windowID := fiveHourID
		if limit.Kind == "weekly_all" {
			windowID = "seven_day"
		}
		window := makeClaudeWindow(windowID, limit.Percent, duration, optionalTime(limit.Reset), limit.State, limit.LockedReason, limit.Severity, extra, false)
		rejected := claudeWindowRejected(limit.LockedReason, limit.Severity)
		for i := range r.Pools[0].Windows {
			if r.Pools[0].Windows[i].ID == windowID {
				r.Pools[0].Windows[i] = mergeClaudeWindow(r.Pools[0].Windows[i], window, rejected)
				return
			}
		}
		r.Pools[0].Windows = append(r.Pools[0].Windows, window)
		return
	}
	scope, models := scopeUnknown, []string(nil)
	if model != "" {
		scope, models = scopeModels, []string{model}
		id += ":" + model
	}
	if limit.Scope.Surface != "" {
		id += ":" + limit.Scope.Surface
		// A named surface may not cover ordinary dispatch turns.
		scope = scopeUnknown
	}
	window := makeClaudeWindow(limit.Kind, limit.Percent, duration, optionalTime(limit.Reset), limit.State, limit.LockedReason, limit.Severity, extra, true)
	rejected := claudeWindowRejected(limit.LockedReason, limit.Severity)
	for i := 1; i < len(r.Pools); i++ {
		pool := &r.Pools[i]
		legacy := (pool.ID == claudeOpusWeekly || pool.ID == claudeSonnetWeekly) && pool.Windows[0].ID == pool.ID
		if pool.ID == id || legacy && limit.Kind == "weekly_scoped" && limit.Scope.Surface == "" &&
			slices.ContainsFunc(pool.Models, func(scope string) bool { return claudeScopeMatches(scope, model) }) {
			pool.Windows[0] = mergeClaudeWindow(pool.Windows[0], window, rejected)
			pool.Models = models
			return
		}
	}
	r.Pools = append(r.Pools, Pool{ID: id, Scope: scope, Models: models, Windows: []Window{window}})
}

func mergeClaudeWindow(legacy, structured Window, structuredRejected bool) Window {
	structuredFull := full(structured.UsedPercent)
	if structured.UsedPercent == nil {
		structured.UsedPercent = legacy.UsedPercent
	}
	if structured.ResetsAt == nil && structured.State != windowPending {
		structured.ResetsAt = legacy.ResetsAt
		if structured.State != windowRolling {
			structured.StartsAt = legacy.StartsAt
		}
	}
	if structured.Blocked == nil {
		if !structuredRejected && !structuredFull {
			structured.Admission = legacy.Admission
		}
	} else if *structured.Blocked {
		structured.ClearsAt = structured.ResetsAt
	}
	return structured
}

func claudeWindowRejected(locked, severity string) bool {
	return locked != "" || severity == "rejected" || severity == "locked" || severity == "blocked"
}

func makeClaudeWindow(id string, used *float64, length time.Duration, reset *time.Time, reportedState, locked, severity string, extra *claudeExtra, scoped bool) Window {
	state := windowFixed
	switch reportedState {
	case windowPending, windowRolling:
		state = reportedState
	}
	w := newWindow(id, used, length, reset, state)
	enforced := extra != nil && (extra.Enabled != nil && !*extra.Enabled || extra.UserDisabled || extra.SpendLimit || extra.CreditsCanPay == "no")
	canPay := !scoped && extra != nil && extra.Enabled != nil && *extra.Enabled && !extra.UserDisabled && !extra.SpendLimit && extra.CreditsCanPay == "yes"
	rejected := claudeWindowRejected(locked, severity)
	switch {
	case state == windowPending, canPay:
		w.Admission = knownAllowed()
	case enforced && (rejected || full(w.UsedPercent)):
		w.Admission = knownBlocked(reset)
	case !rejected && (severity == "normal" || severity == "warning" || w.UsedPercent != nil && !full(w.UsedPercent)):
		w.Admission = knownAllowed()
	}
	return w
}

func claudeDuration(kind, group string) time.Duration {
	switch {
	case strings.HasPrefix(kind, weeklyID), group == weeklyID:
		return week
	case kind == "session_all", kind == fiveHourID, group == "session":
		return hours5
	default:
		return 0
	}
}

func modelName(display string) string { return strings.ToLower(strings.TrimSpace(display)) }

var claudeFamilies = []string{"opus", "sonnet", "haiku", "fable"}

func claudeScopeMatches(scope, id string) bool {
	scope = modelName(scope)
	id, _, _ = strings.Cut(modelName(id), "[")
	if scope == id {
		return true
	}
	for _, family := range claudeFamilies {
		if scope == family && claudeFamilyModel(id, family) || id == family && claudeFamilyModel(scope, family) {
			return true
		}
	}
	return false
}

func claudeFamilyModel(id, family string) bool {
	id = strings.TrimPrefix(id, "claude-")
	return id == family || strings.HasPrefix(id, family+"-") || strings.HasPrefix(id, family+" ")
}

func claudeAmbiguousAlias(id string, pools []Pool) string {
	family, _, _ := strings.Cut(modelName(id), "[")
	if !slices.Contains(claudeFamilies, family) {
		return ""
	}
	var version string
	for _, pool := range pools {
		if pool.Scope != scopeModels {
			continue
		}
		for _, scope := range pool.Models {
			model := modelName(scope)
			if model == family || !claudeFamilyModel(model, family) {
				continue
			}
			if version != "" && version != model {
				return family
			}
			version = model
		}
	}
	return ""
}

// Keep unfamiliar weekly counters with their native identity while leaving
// their model or surface applicability unknown.
func (d *claudeUsageResponse) UnmarshalJSON(data []byte) error {
	type wire claudeUsageResponse
	if err := json.Unmarshal(data, (*wire)(d)); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_, d.fiveHourReported = raw[fiveHourID]
	_, d.sevenDayReported = raw["seven_day"]
	for id, value := range raw {
		if !strings.HasPrefix(id, "seven_day_") {
			continue
		}
		switch id {
		case claudeOpusWeekly, claudeSonnetWeekly, "seven_day_cowork", "seven_day_oauth_apps", "seven_day_breakdown":
			continue
		}
		var window *claudeWindow
		if json.Unmarshal(value, &window) == nil && window != nil &&
			(window.Utilization != nil || len(window.Reset) != 0 || window.LockedReason != "" || window.State != "") {
			if d.LegacyCounters == nil {
				d.LegacyCounters = make(map[string]*claudeWindow)
			}
			d.LegacyCounters[id] = window
		}
	}
	return nil
}
