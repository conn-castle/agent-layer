package quota

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

const agyBase = "https://daily-cloudcode-pa.googleapis.com/v1internal:"

// The service negotiates the CLI quota groups and model catalog from the
// versioned client identifier. A bare "antigravity" gets the legacy projection.
const agyUserAgent = "antigravity/cli/1.5.18"

type agyQuota struct {
	ID        string          `json:"bucketId"`
	Model     string          `json:"modelId"`
	TokenType string          `json:"tokenType"`
	Window    string          `json:"window"`
	Remaining *float64        `json:"remainingFraction"`
	Reset     json.RawMessage `json:"resetTime"`
	Disabled  *bool           `json:"disabled"`
	Allowed   *bool           `json:"allowed"`
	Started   *bool           `json:"hasStarted"`
	Blocked   *bool           `json:"blocked"`
	Exhausted *bool           `json:"isExhausted"`
	Pending   *bool           `json:"notStarted"`
	Clears    json.RawMessage `json:"lockoutClearTime"`
	ModelIDs  []string        `json:"modelIds"`
}

type agySummary struct {
	Buckets []agyQuota `json:"buckets"`
	Groups  []struct {
		ID          string     `json:"groupId"`
		Name        string     `json:"displayName"`
		Description string     `json:"description"`
		ModelIDs    []string   `json:"modelIds"`
		Buckets     []agyQuota `json:"buckets"`
	} `json:"groups"`
}

type agyLegacy struct {
	Buckets []agyQuota `json:"buckets"`
}

type agyCatalog struct {
	Models map[string]struct {
		ID     string `json:"model"`
		Name   string `json:"displayName"`
		Beyond struct {
			QuotaBucket string `json:"quotaBucketName"`
		} `json:"beyondModelInfo"`
	} `json:"models"`
	Groups map[string]struct {
		Name   string `json:"displayName"`
		Levels []struct {
			Suffix string `json:"thinkingSuffix"`
		} `json:"supportedThinkingLevels"`
	} `json:"groupedModels"`
	Deprecated map[string]json.RawMessage `json:"deprecatedModelIds"`
}

type agyCredits struct {
	Tier struct {
		ID string `json:"id"`
	} `json:"currentTier"`
	PaidTier struct {
		ID string `json:"id"`
	} `json:"paidTier"`
	CanUse    *bool    `json:"canUseG1Credits"`
	Remaining *float64 `json:"availableG1Credits"`
}

type agyUser struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Verified bool   `json:"verified_email"`
}

func readAntigravity(ctx context.Context, h *httpReader, auth credential, r *Result) failure {
	headers := http.Header{userAgentHeader: {agyUserAgent}}
	var summary agySummary
	var perModel agyLegacy
	var catalog agyCatalog
	var account agyUser
	var credits agyCredits
	var reads [5]struct {
		at   *time.Time
		fail failure
	}
	var workers sync.WaitGroup
	for i, request := range []struct {
		endpoint, method, body string
		dst                    any
	}{
		{agyBase + "retrieveUserQuotaSummary", http.MethodPost, "{}", &summary},
		{agyBase + "retrieveUserQuota", http.MethodPost, "{}", &perModel},
		{agyBase + "fetchAvailableModels", http.MethodPost, "{}", &catalog},
		{"https://www.googleapis.com/oauth2/v2/userinfo", http.MethodGet, "", &account},
		{agyBase + "loadCodeAssist", http.MethodPost, "{}", &credits},
	} {
		workers.Go(func() {
			reads[i].at, reads[i].fail = h.fetch(ctx, auth, request.method, request.endpoint, request.body, headers, request.dst)
		})
	}
	workers.Wait()
	for i, component := range []string{"quota_summary", "model_quotas", "model_catalog", "account_identity", "credits"} {
		read := reads[i]
		if i >= 2 {
			read.at = nil
		}
		retainRead(r, read.at, read.fail, component)
	}
	if account.ID != "" && reads[3].fail.status == "" {
		aliases := []string{account.ID}
		if account.Verified {
			aliases = append(aliases, account.Email)
		}
		r.Subscription = subscription(r.Agent, account.ID, textFrom(credits.PaidTier.ID, credits.Tier.ID), true, aliases...)
	}
	r.identityFailure = reads[3].fail
	if reads[0].fail.status != "" && reads[1].fail.status != "" {
		return antigravityQuotaFailure(reads[0].fail, reads[1].fail)
	}
	normalizeAntigravity(summary, perModel, catalog, credits, auth, r)
	return failure{}
}

func antigravityQuotaFailure(summary, perModel failure) failure {
	if summary.isAuth() {
		return summary
	}
	if perModel.isAuth() {
		return perModel
	}
	// Report the primary summary error while respecting the later server hint.
	if summary.retryable && perModel.retryAt != nil &&
		(summary.retryAt == nil || perModel.retryAt.After(*summary.retryAt)) {
		summary.retryAt = perModel.retryAt
	}
	return summary
}

func normalizeAntigravity(summary agySummary, legacy agyLegacy, catalog agyCatalog, credits agyCredits, auth credential, r *Result) {
	r.ModelCoverage = coveragePartial
	creditBypass := agyCreditBypass(credits, auth.useCredits)
	covered := make(map[string]bool)
	explicitAllowed := make(map[string]bool)
	addSummary := func(group, description string, modelIDs []string, bucket agyQuota) {
		ids := agyBucketModels(description, modelIDs, bucket, catalog)
		if slices.Contains(ids, bucket.ID) {
			group = bucket.ID
		}
		duration, state := agyWindowDuration(bucket.Window)
		if agySummaryWindowPending(bucket) {
			state = windowPending
		}
		window := agyWindow(bucket.ID, bucket, duration, state, creditBypass)
		if window.UsedPercent != nil || window.Disabled != nil && *window.Disabled {
			for _, id := range ids {
				covered[id] = true
			}
		} else if bucket.Allowed != nil && *bucket.Allowed || bucket.Blocked != nil && !*bucket.Blocked {
			for _, id := range ids {
				explicitAllowed[id] = true
			}
		}
		addAgyBucket(r, group, ids, window)
	}
	flat := make(map[string]agyQuota, len(summary.Buckets))
	for _, bucket := range summary.Buckets {
		if bucket.ID != "" {
			flat[bucket.ID] = bucket
		}
	}
	for _, group := range summary.Groups {
		for _, bucket := range group.Buckets {
			if bucket.ID == "" {
				continue
			}
			if peer, ok := flat[bucket.ID]; ok {
				bucket = mergeAgySummaryBucket(bucket, peer)
				delete(flat, bucket.ID)
			}
			addSummary(textFrom(group.ID, group.Name, bucket.ID), group.Description, group.ModelIDs, bucket)
		}
	}
	for _, bucket := range summary.Buckets {
		if _, ok := flat[bucket.ID]; ok {
			addSummary(bucket.ID, "", nil, bucket)
			delete(flat, bucket.ID)
		}
	}
	for _, bucket := range legacy.Buckets {
		if bucket.Model == "" || covered[bucket.Model] {
			continue
		}
		ids := agyAliases(catalog, []string{bucket.Model})
		window := agyWindow(textFrom(bucket.TokenType, "provider_window"), bucket, 0, windowUnknown, creditBypass)
		if explicitAllowed[bucket.Model] && bucket.Blocked == nil && bucket.Allowed == nil {
			window.Admission = knownAllowed()
		}
		if fillAgyNamedModelMeter(r, bucket.Model, ids, window) {
			continue
		}
		addAgyBucketWithGroup(r, "legacy:"+bucket.Model, bucket.Model, ids, window)
	}
	addAgyModels(r, catalog)
}

func fillAgyNamedModelMeter(r *Result, model string, ids []string, legacy Window) bool {
	for i := range r.Pools {
		pool := &r.Pools[i]
		if pool.ID != model || len(pool.Windows) != 1 || pool.Windows[0].ID != pool.ID || !slices.Equal(pool.Models, ids) {
			continue
		}
		w := &pool.Windows[0]
		if w.UsedPercent != nil || legacy.UsedPercent == nil {
			continue
		}
		w.UsedPercent = legacy.UsedPercent
		if w.ResetsAt == nil {
			w.ResetsAt = legacy.ResetsAt
		}
		if w.Blocked == nil {
			w.Admission = legacy.Admission
		}
		pool.Admission = windowAdmission(pool.Windows)
		return true
	}
	return false
}

func addAgyModels(r *Result, catalog agyCatalog) {
	coverage := make(map[string]string)
	r.modelAliases = make(map[string]string)
	for id, model := range catalog.Models {
		coverage[id] = scopeUnknown
		if model.Name != "" {
			if other, exists := r.modelAliases[model.Name]; !exists {
				r.modelAliases[model.Name] = id
			} else if other != id {
				r.modelAliases[model.Name] = ""
			}
		}
	}
	for id := range catalog.Groups {
		coverage[id] = scopeUnknown
	}
	for _, pool := range r.Pools {
		namedModelMeter := len(pool.Windows) == 1 && pool.Windows[0].ID == pool.ID && slices.Contains(pool.Models, pool.ID)
		if pool.Scope == scopeModels && (namedModelMeter || agyHasWindow(pool.Windows, hours5) && agyHasWindow(pool.Windows, week)) {
			for _, id := range pool.Models {
				coverage[id] = coverageComplete
			}
		}
	}
	for id, modelCoverage := range coverage {
		r.Models = append(r.Models, Model{ID: id, Coverage: modelCoverage})
	}
}

func agyHasWindow(windows []Window, duration time.Duration) bool {
	return slices.ContainsFunc(windows, func(window Window) bool {
		return window.DurationSeconds != nil && *window.DurationSeconds == int64(duration/time.Second)
	})
}

func rawJSONAbsent(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null"
}

func mergeAgySummaryBucket(group, flat agyQuota) agyQuota {
	group.Model = textFrom(group.Model, flat.Model)
	group.Window = textFrom(group.Window, flat.Window)
	group.Remaining = cmp.Or(group.Remaining, flat.Remaining)
	group.Disabled = cmp.Or(group.Disabled, flat.Disabled)
	group.Allowed = cmp.Or(group.Allowed, flat.Allowed)
	group.Started = cmp.Or(group.Started, flat.Started)
	group.Blocked = cmp.Or(group.Blocked, flat.Blocked)
	group.Exhausted = cmp.Or(group.Exhausted, flat.Exhausted)
	group.Pending = cmp.Or(group.Pending, flat.Pending)
	if rawJSONAbsent(group.Reset) {
		group.Reset = flat.Reset
	}
	if rawJSONAbsent(group.Clears) {
		group.Clears = flat.Clears
	}
	if len(group.ModelIDs) == 0 {
		group.ModelIDs = flat.ModelIDs
	}
	return group
}

func agySummaryWindowPending(bucket agyQuota) bool {
	if bucket.Disabled != nil && *bucket.Disabled {
		return false
	}
	if bucket.Pending != nil {
		return *bucket.Pending
	}
	if bucket.Started != nil {
		return !*bucket.Started
	}
	// The structured five-hour counter has a full balance and omits the
	// timer before its first use. The legacy counters can synthesize timers
	// and do not establish an unstarted window.
	return bucket.Window == "5h" && bucket.Remaining != nil && *bucket.Remaining == 1 &&
		rawJSONAbsent(bucket.Reset)
}

func addAgyBucket(r *Result, group string, ids []string, window Window) {
	addAgyBucketWithGroup(r, group, group, ids, window)
}

func addAgyBucketWithGroup(r *Result, nativeGroup, displayGroup string, ids []string, window Window) {
	for i := range r.Pools {
		pool := &r.Pools[i]
		if pool.nativeGroup == nativeGroup && slices.Equal(pool.Models, ids) {
			pool.Windows = append(pool.Windows, window)
			pool.Admission = windowAdmission(pool.Windows)
			return
		}
	}
	poolID := displayGroup
	if agyHasPoolID(r.Pools, poolID) {
		base := displayGroup + ":" + window.ID
		poolID = base
		for n := 2; agyHasPoolID(r.Pools, poolID); n++ {
			poolID = fmt.Sprintf("%s:%d", base, n)
		}
	}
	scope := scopeUnknown
	if len(ids) > 0 {
		scope = scopeModels
	}
	r.Pools = append(r.Pools, Pool{ID: poolID, Scope: scope, Models: ids, Windows: []Window{window}, Admission: window.Admission, nativeGroup: nativeGroup})
}

func agyHasPoolID(pools []Pool, id string) bool {
	return slices.ContainsFunc(pools, func(pool Pool) bool { return pool.ID == id })
}

func agyBucketModels(description string, groupIDs []string, bucket agyQuota, catalog agyCatalog) []string {
	ids := append([]string(nil), bucket.ModelIDs...)
	if bucket.Model != "" {
		ids = append(ids, bucket.Model)
	}
	if len(ids) == 0 {
		ids = append(ids, groupIDs...)
	}
	if len(ids) == 0 {
		// Workspace subscriptions can report a separate counter keyed by
		// the exact model slug in the catalog instead of shared windows.
		if _, knownModel := catalog.Models[bucket.ID]; knownModel {
			return agyAliases(catalog, []string{bucket.ID})
		}
	}
	names, _ := strings.CutPrefix(description, "Models within this group: ")
	members := strings.Split(names, ", ")
	for id, model := range catalog.Models {
		name, _, _ := strings.Cut(model.Name, " (")
		if model.Beyond.QuotaBucket == bucket.ID || slices.Contains(ids, model.ID) ||
			len(bucket.ModelIDs) == 0 && len(groupIDs) == 0 && name != "" &&
				(slices.Contains(members, model.Name) || slices.Contains(members, name)) {
			ids = append(ids, id)
		}
	}
	// Group descriptions name base models and thinking-tier variants are
	// bound by the provider's groupedModels record.
	if len(bucket.ModelIDs) == 0 && len(groupIDs) == 0 {
		for id, group := range catalog.Groups {
			if slices.Contains(members, group.Name) {
				ids = append(ids, id)
			}
		}
	}
	return agyAliases(catalog, ids)
}

func agyAliases(catalog agyCatalog, ids []string) []string {
	all := append([]string(nil), ids...)
	for id, group := range catalog.Groups {
		if slices.Contains(ids, id) {
			for _, level := range group.Levels {
				all = append(all, agyVariant(id, level.Suffix))
			}
		}
	}
	for alias, raw := range catalog.Deprecated {
		var replacement string
		if json.Unmarshal(raw, &replacement) == nil && slices.Contains(all, replacement) {
			all = append(all, alias)
		}
	}
	slices.Sort(all)
	return slices.Compact(all)
}

func agyVariant(id, suffix string) string {
	if suffix == "" {
		return id
	}
	return id + "-" + strings.TrimPrefix(suffix, "-")
}

func agyWindowDuration(value string) (time.Duration, string) {
	switch value {
	case "5h":
		return hours5, windowFixed
	case weeklyID:
		return week, windowFixed
	case windowRolling:
		return 0, windowRolling
	default:
		return 0, windowUnknown
	}
}

func agyWindow(id string, q agyQuota, length time.Duration, state string, creditBypass *bool) Window {
	var used *float64
	if q.Remaining != nil && *q.Remaining >= 0 && *q.Remaining <= 1 {
		used = ptr((1 - *q.Remaining) * 100)
	}
	if q.Pending != nil && *q.Pending {
		state = windowPending
	}
	w := newWindow(id, used, length, optionalTime(q.Reset), state)
	w.Disabled = q.Disabled
	switch {
	case q.Blocked != nil && *q.Blocked || q.Allowed != nil && !*q.Allowed:
		clear := optionalTime(q.Clears)
		if clear == nil && (q.Disabled == nil || !*q.Disabled) {
			clear = w.ResetsAt
		}
		w.Admission = knownBlocked(clear)
	case q.Blocked != nil, q.Allowed != nil, q.Disabled != nil && *q.Disabled, state == windowPending, creditBypass != nil && *creditBypass:
		w.Admission = knownAllowed()
	case (q.Exhausted != nil && *q.Exhausted || full(w.UsedPercent)) && creditBypass != nil && !*creditBypass:
		w.Admission = knownBlocked(w.ResetsAt)
	case (q.Exhausted == nil || !*q.Exhausted) && w.UsedPercent != nil && !full(w.UsedPercent):
		w.Admission = knownAllowed()
	}
	return w
}

func agyCreditBypass(credits agyCredits, selected *bool) *bool {
	if selected != nil && !*selected || credits.CanUse != nil && !*credits.CanUse {
		return ptr(false)
	}
	if credits.Remaining != nil && *credits.Remaining <= 0 {
		return ptr(false)
	}
	if selected != nil && *selected && credits.CanUse != nil && *credits.CanUse && credits.Remaining != nil && *credits.Remaining > 0 {
		return ptr(true)
	}
	return nil
}
