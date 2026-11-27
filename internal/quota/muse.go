package quota

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type museUsage struct {
	Active *bool  `json:"is_subs_active"`
	Pay    *bool  `json:"require_payment"`
	Email  string `json:"user_email"`
	Tier   string `json:"subs_tier_name"`
	Usage  *struct {
		Short  *museWindow `json:"window"`
		Weekly *museWindow `json:"weekly"`
		Tier   string      `json:"tier"`
	} `json:"subs_usage"`
}

type museWindow struct {
	Used    *float64        `json:"used_percent"`
	Minutes json.RawMessage `json:"window_duration_mins"`
	Reset   json.RawMessage `json:"resets_at"`
	State   string          `json:"window_state"`
}

func readMuse(ctx context.Context, h *httpReader, auth credential, r *Result) failure {
	headers := http.Header{userAgentHeader: {"muse-build/0.1.0"}}
	headers.Set("x-api-version", "1.0.0")
	// The enrolled-account lookup returns the existing API key. The
	// response struct intentionally excludes it and all payment details.
	var data museUsage
	at, fail := h.fetch(ctx, auth, http.MethodPost, museBase+"/muse-code/key", `{"onboard":false}`, headers, &data)
	r.ObservedAt = at
	if fail.status != "" {
		return fail
	}
	if data.Email != "" {
		tier := data.Tier
		if data.Usage != nil {
			tier = textFrom(tier, data.Usage.Tier)
		}
		r.Subscription = subscription(r.Agent, strings.ToLower(data.Email), tier, true, data.Email)
	}
	if data.Pay != nil && *data.Pay {
		r.Admission = knownBlocked(nil)
	}
	if data.Active != nil && !*data.Active {
		return nonSubscription("subscription_inactive")
	}
	if data.Usage == nil {
		return unavailable(reasonNoUsage)
	}
	pool := Pool{ID: agentMuse, Scope: scopeAll, Windows: []Window{
		makeMuseWindow(fiveHourID, data.Usage.Short, hours5),
		makeMuseWindow(weeklyID, data.Usage.Weekly, week),
	}}
	pool.Admission = windowAdmission(pool.Windows)
	r.Pools = append(r.Pools, pool)
	if r.Blocked == nil {
		r.Admission = pool.Admission
	}
	r.ModelCoverage = coverageComplete
	return failure{}
}

func makeMuseWindow(id string, raw *museWindow, length time.Duration) Window {
	if raw == nil {
		return newWindow(id, nil, length, nil, windowUnknown)
	}
	if reported := durationSeconds(optionalInt64(raw.Minutes)); reported > 0 && reported <= time.Duration(1<<63-1)/60 {
		length = reported * 60
	}
	state := windowFixed
	switch raw.State {
	case windowPending, windowRolling:
		state = raw.State
	}
	w := newWindow(id, raw.Used, length, optionalTime(raw.Reset), state)
	switch {
	case state == windowPending:
		w.Admission = knownAllowed()
	case full(w.UsedPercent):
		// The subscription key has both hard allowances. Pay-as-you-go
		// uses a separate key rather than bypassing them on this login.
		w.Admission = knownBlocked(w.ResetsAt)
	case w.UsedPercent != nil:
		w.Admission = knownAllowed()
	}
	return w
}
