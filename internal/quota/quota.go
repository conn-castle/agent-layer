// Package quota reads first-party subscription telemetry without running providers.
package quota

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
)

const (
	agentCodex              = "codex"
	agentClaude             = "claude"
	agentGrok               = "grok"
	agentMuse               = "muse"
	agentAntigravity        = "antigravity"
	coverageComplete        = "complete"
	coveragePartial         = "partial"
	weeklyID                = "weekly"
	monthlyID               = "monthly"
	fiveHourID              = "five_hour"
	scopeOther              = "other_surface"
	userAgentHeader         = "User-Agent"
	reasonMissingCredential = "missing_credentials"
	platformDarwin          = "darwin"
	reasonAPIKey            = "api_key"
	reasonCustomEndpoint    = "custom_endpoint"
	reasonCredentialPath    = "credential_path_unavailable" // #nosec G101 -- sanitized status reason.
	statusOK                = "ok"
	statusUnsupported       = "unsupported"
	statusNonSubscription   = "non_subscription"
	statusCredentials       = "credentials_unavailable"
	statusLoginExpired      = "login_expired"
	statusMismatch          = "account_mismatch"
	statusUnavailable       = "unavailable"

	reasonCustomAuth = "custom_auth"
	reasonNoUsage    = "no_usage"
	scopeAll         = "all"
	scopeModels      = "models"
	scopeUnknown     = "unknown"
	windowFixed      = "fixed"
	windowRolling    = "rolling"
	windowPending    = "not_started"
	windowUnknown    = "unknown"
	hours5           = 5 * time.Hour
	week             = 7 * 24 * time.Hour
)

const (
	// SchemaVersion identifies the quota response schema.
	SchemaVersion = 1
	// ResponseComplete means every selected reading succeeded.
	ResponseComplete = coverageComplete
	// ResponsePartial means at least one selected reading is missing.
	ResponsePartial = coveragePartial
	// ResponseNoReadings means no usable reading was returned.
	ResponseNoReadings = "no_readings"
)

// Source describes either a project's launched account or a detached native credential file.
type Source struct {
	Selector        string
	Agent           string
	File            string
	Expected        string
	Models          []string
	ConfiguredModel string
	Disabled        bool
}

// Request carries already validated selectors and the launch context.
type Request struct {
	Project    *config.ProjectConfig
	WorkDir    string
	Env        []string
	Sources    []Source
	Offline    bool
	MaxRetries int
}

// Response contains one result per selector, in request order.
type Response struct {
	SchemaVersion int              `json:"schema_version"`
	Status        string           `json:"status"`
	Missing       []MissingReading `json:"missing_readings"`
	Results       []Result         `json:"results"`
}

// MissingReading summarizes a result whose quota data cannot be used.
type MissingReading struct {
	Selector string `json:"selector"`
	Agent    string `json:"agent"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
}

// Result is a subscription observation, or a sanitized explanation of its absence.
type Result struct {
	Selector     string        `json:"selector"`
	Agent        string        `json:"agent"`
	Status       string        `json:"status"`
	Reason       string        `json:"reason,omitempty"`
	AttemptedAt  time.Time     `json:"attempted_at"`
	ObservedAt   *time.Time    `json:"observed_at"`
	Reauth       *bool         `json:"reauth_required"`
	Retryable    bool          `json:"retryable"`
	Retries      int           `json:"retries"`
	RetryAt      *time.Time    `json:"retry_at,omitempty"`
	AccountCheck string        `json:"account_check"`
	Subscription *Subscription `json:"subscription"`
	Admission
	Pools            []Pool   `json:"pools"`
	Models           []Model  `json:"models"`
	ModelCoverage    string   `json:"model_coverage"`
	BlockReason      string   `json:"block_reason,omitempty"`
	Missing          []string `json:"missing,omitempty"`
	modelAuth        map[string]string
	modelAliases     map[string]string
	identityFailure  failure
	independentBlock Admission
}

// Subscription identifies a provider-side billing principal without returning personal identifiers.
type Subscription struct {
	ID       string `json:"id"`
	Verified bool   `json:"verified"`
	Tier     string `json:"tier,omitempty"`
	aliases  []string
}

// Admission reports whether a new request would be rejected and when all known blockers clear.
type Admission struct {
	Blocked  *bool      `json:"blocked"`
	ClearsAt *time.Time `json:"lockout_clears_at"`
}

// Pool describes a quota shared by the indicated models.
type Pool struct {
	ID     string   `json:"id"`
	Label  string   `json:"label,omitempty"`
	Scope  string   `json:"scope"`
	Models []string `json:"models,omitempty"`
	Admission
	Windows     []Window `json:"windows"`
	nativeGroup string
}

// Window holds one provider-reported utilization interval.
type Window struct {
	ID              string     `json:"id"`
	UsedPercent     *float64   `json:"used_percent"`
	DurationSeconds *int64     `json:"duration_seconds"`
	ResetsAt        *time.Time `json:"resets_at"`
	StartsAt        *time.Time `json:"starts_at"`
	State           string     `json:"state"`
	Disabled        *bool      `json:"disabled,omitempty"`
	Admission
}

// Model evaluates the applicability of the pools for a configured, requested or provider-reported model.
type Model struct {
	ID         string   `json:"id"`
	Coverage   string   `json:"coverage"`
	Pools      []string `json:"pools"`
	AuthStatus string   `json:"auth_status,omitempty"`
	Admission
	bypassesIncluded bool
}

type reader func(context.Context, *httpReader, credential, *Result) failure

func provider(agent string) reader {
	switch agent {
	case agentCodex:
		return readCodex
	case agentClaude:
		return readClaude
	case agentGrok:
		return readGrok
	case agentAntigravity:
		return readAntigravity
	case agentMuse:
		return readMuse
	default:
		return nil
	}
}

// Supported reports static quota capability without discovery or auth inspection.
func Supported(agent string) bool { return provider(agent) != nil }

// Query reads selected accounts concurrently and waits for every reader to exit.
func Query(ctx context.Context, req Request) Response {
	http := newHTTPReader()
	defer http.client.CloseIdleConnections()
	return query(ctx, req, http)
}

func query(ctx context.Context, req Request, http *httpReader) Response {
	out := Response{
		SchemaVersion: SchemaVersion,
		Missing:       []MissingReading{},
		Results:       make([]Result, len(req.Sources)),
	}
	var workers sync.WaitGroup
	for i, source := range req.Sources {
		workers.Go(func() { out.Results[i] = readSource(ctx, req, source, http) })
	}
	workers.Wait()
	for _, result := range out.Results {
		if result.Status != statusOK {
			out.Missing = append(out.Missing, MissingReading{
				Selector: result.Selector, Agent: result.Agent, Status: result.Status, Reason: result.Reason,
			})
		}
	}
	switch {
	case len(out.Missing) == len(out.Results):
		out.Status = ResponseNoReadings
	case len(out.Missing) == 0:
		out.Status = ResponseComplete
	default:
		out.Status = ResponsePartial
	}
	return out
}

func readSource(ctx context.Context, req Request, source Source, http *httpReader) Result {
	var retries atomic.Int32
	sourceHTTP := *http
	sourceHTTP.maxRetries = req.MaxRetries
	sourceHTTP.retries = &retries
	result := Result{
		Selector: source.Selector, Agent: source.Agent, AttemptedAt: http.now(),
		Pools: []Pool{}, Models: []Model{}, ModelCoverage: scopeUnknown,
		AccountCheck: "not_requested",
	}
	read := provider(source.Agent)
	var fail failure
	switch {
	case read == nil:
		fail = failure{status: statusUnsupported, reason: "provider_not_supported"}
	case source.Disabled:
		fail = unsupportedAuth("agent_disabled")
	case req.Offline:
		fail = unavailable("network_disabled")
	case ctx.Err() != nil:
		fail = contextFailure(ctx)
	default:
		fail = readWithCredential(ctx, req, source, read, &sourceHTTP, &result)
	}
	result.Status, result.Reason, result.Reauth = fail.status, fail.reason, fail.reauth
	if result.Status == "" {
		result.Status = statusOK
		result.Reauth = ptr(false)
		if !result.hasUsage() {
			result.Status, result.Reason = statusUnavailable, reasonNoUsage
		}
	}
	if result.Reason == reasonNoUsage {
		result.Reauth = ptr(false)
	}
	if result.Status != statusOK {
		result.Missing = nil
	}
	result.Retryable, result.RetryAt = fail.retryable, publishRetryAt(fail.retryAt, http.now())
	result.Retries = int(retries.Load())
	models := append([]string(nil), source.Models...)
	if source.ConfiguredModel != "" {
		models = append(models, source.ConfiguredModel)
	}
	result.evaluateModels(models)
	result.checkAccount(source.Expected, http.now())
	return result
}

func readWithCredential(ctx context.Context, req Request, source Source, read reader, http *httpReader, result *Result) failure {
	auth, fail := loadCredential(ctx, req, source)
	if fail.status != "" {
		return fail
	}
	result.Subscription = auth.identity
	wasExpired := auth.expired(http.now())
	if wasExpired {
		var renewal failure
		auth, renewal = renewCredential(ctx, req, source, auth, http.now)
		if renewal.status != "" {
			return renewal
		}
	}
	beforeRead := *result
	result.Subscription, result.modelAuth = auth.identity, auth.modelAuth
	fail = read(ctx, http, auth, result)
	if !fail.isAuth() || !fail.remote {
		return fail
	}
	next, reload := updatedCredential(ctx, req, source, auth, http.now, false)
	if reload.status != "" {
		return reload
	}
	if next.token == "" {
		if wasExpired || auth.native == nil || !auth.canRenew(http.now()) || fail.reauth != nil && *fail.reauth {
			return fail
		}
		next, reload = renewCredential(ctx, req, source, auth, http.now)
		if reload.status != "" {
			if reload.reason == reasonAccessExpired && fail.reason != reasonAccessExpired {
				fail.retryable = reload.retryable
				return fail
			}
			return reload
		}
	}
	*result = beforeRead
	result.Subscription, result.modelAuth = next.identity, next.modelAuth
	return read(ctx, http, next, result)
}

type failure struct {
	status    string
	reason    string
	reauth    *bool
	retryable bool
	retryAt   *time.Time
	remote    bool
}

func unavailable(reason string) failure { return failure{status: statusUnavailable, reason: reason} }

func transientFailure(reason string) failure {
	return failure{status: statusUnavailable, reason: reason, retryable: true}
}

func (f failure) isAuth() bool {
	return f.status == statusCredentials || f.status == statusLoginExpired
}

func (r *Result) hasUsage() bool {
	for _, pool := range r.Pools {
		for _, window := range pool.Windows {
			if window.UsedPercent != nil {
				return true
			}
		}
	}
	return false
}

func subscription(agent, principal, tier string, verified bool, aliases ...string) *Subscription {
	if principal == "" {
		return &Subscription{Tier: tier}
	}
	sum := sha256.Sum256([]byte(principal))
	return &Subscription{ID: fmt.Sprintf("%s:%x", agent, sum[:]), Verified: verified, Tier: tier, aliases: aliases}
}

func (r *Result) checkAccount(want string, now time.Time) {
	if want == "" {
		return
	}
	if r.Subscription == nil || !r.Subscription.Verified {
		r.AccountCheck = "unverified"
		if r.Status == statusOK {
			r.Status, r.Reason = statusMismatch, "account_unverified"
			r.Retryable = r.identityFailure.retryable
			r.RetryAt = publishRetryAt(r.identityFailure.retryAt, now)
		}
		return
	}
	if want == r.Subscription.ID || slices.ContainsFunc(r.Subscription.aliases, func(alias string) bool {
		return alias != "" && (alias == want || strings.Contains(alias, "@") && strings.EqualFold(alias, want))
	}) {
		r.AccountCheck = "matched"
		return
	}
	r.AccountCheck = "mismatch"
	r.Status, r.Reason = statusMismatch, "unexpected_account"
	r.Reauth, r.Retryable, r.RetryAt = ptr(false), false, nil
}

func ptr[T any](v T) *T { return &v }

func percent(value *float64) *float64 {
	if value == nil || *value < 0 || math.IsInf(*value, 0) || math.IsNaN(*value) {
		return nil
	}
	return value
}

func full(value *float64) bool { return value != nil && *value >= 100 }

func newWindow(id string, used *float64, length time.Duration, reset *time.Time, state string) Window {
	w := Window{ID: id, UsedPercent: percent(used), ResetsAt: reset, State: state}
	if length > 0 {
		w.DurationSeconds = ptr(int64(length / time.Second))
	}
	if state == windowFixed && reset != nil && length > 0 {
		w.StartsAt = ptr(reset.Add(-length))
	}
	return w
}

func knownBlocked(at *time.Time) Admission { return Admission{Blocked: ptr(true), ClearsAt: at} }
func knownAllowed() Admission              { return Admission{Blocked: ptr(false)} }

func aggregate(gates ...Admission) Admission {
	var latest *time.Time
	var blocked, unknownExpiry, unknownAdmission bool
	for _, gate := range gates {
		switch {
		case gate.Blocked == nil:
			unknownAdmission = true
		case *gate.Blocked:
			blocked = true
			if gate.ClearsAt == nil {
				unknownExpiry = true
			} else if latest == nil || gate.ClearsAt.After(*latest) {
				latest = gate.ClearsAt
			}
		}
	}
	switch {
	case blocked && unknownExpiry:
		return knownBlocked(nil)
	case blocked:
		return knownBlocked(latest)
	case unknownAdmission || len(gates) == 0:
		return Admission{}
	default:
		return knownAllowed()
	}
}

func windowAdmission(windows []Window) Admission {
	gates := make([]Admission, 0, len(windows))
	for _, window := range windows {
		gates = append(gates, window.Admission)
	}
	return aggregate(gates...)
}

func (r *Result) evaluateModels(requested []string) {
	ids := append([]string(nil), requested...)
	for _, p := range r.Pools {
		ids = append(ids, p.Models...)
	}
	for _, model := range r.Models {
		ids = append(ids, model.ID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	models := make([]Model, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		models = append(models, r.evaluateModel(id))
	}
	r.Models = models
}

func (r *Result) evaluateModel(id string) Model {
	m := Model{ID: id, Coverage: coverageComplete, Pools: []string{}}
	id = textFrom(r.modelAliases[id], id)
	if status := r.modelAuth[id]; status != "" {
		m.Coverage, m.AuthStatus = scopeUnknown, status
		return m
	}
	if r.ModelCoverage != coverageComplete {
		m.Coverage = scopeUnknown
	}
	var gates []Admission
	explicitAllowance := false
	bypassesIncluded := false
	for _, existing := range r.Models {
		if existing.ID == id {
			m.Coverage = existing.Coverage
			bypassesIncluded = existing.bypassesIncluded
			if existing.Blocked != nil {
				gates = append(gates, existing.Admission)
				explicitAllowance = !*existing.Blocked
			} else if bypassesIncluded {
				gates = append(gates, existing.Admission)
			}
			break
		}
	}
	var ambiguousAlias string
	if r.Agent == agentClaude {
		ambiguousAlias = claudeAmbiguousAlias(id, r.Pools)
		if ambiguousAlias != "" {
			m.Coverage = coveragePartial
		}
	}
	for _, pool := range r.Pools {
		if pool.Scope == scopeAll || pool.Scope == scopeModels && appliesTo(r.Agent, pool.Models, id) {
			if ambiguousAlias != "" && pool.Scope == scopeModels &&
				!slices.ContainsFunc(pool.Models, func(model string) bool { return strings.EqualFold(model, ambiguousAlias) }) {
				continue
			}
			m.Pools = append(m.Pools, pool.ID)
			if !bypassesIncluded || pool.Scope != scopeAll {
				gates = append(gates, pool.Admission)
			}
		} else if pool.Scope == scopeUnknown && m.Coverage == coverageComplete {
			m.Coverage = coveragePartial
		}
	}
	if len(m.Pools) > 0 && m.Coverage == scopeUnknown {
		m.Coverage = coveragePartial
	}
	if bypassesIncluded {
		if r.independentBlock.Blocked != nil && *r.independentBlock.Blocked {
			gates = append(gates, r.independentBlock)
		}
	} else if r.Blocked != nil && *r.Blocked {
		gates = append(gates, r.Admission)
	}
	m.Admission = aggregate(gates...)
	if explicitAllowance && m.Blocked == nil {
		m.Admission = knownAllowed()
	} else if !explicitAllowance && m.Coverage != coverageComplete && m.Blocked != nil && !*m.Blocked {
		m.Admission = Admission{}
	}
	return m
}

func appliesTo(agent string, scope []string, id string) bool {
	return slices.ContainsFunc(scope, func(model string) bool {
		if strings.EqualFold(model, id) {
			return true
		}
		return agent == agentClaude && claudeScopeMatches(model, id)
	})
}

func parseTime(value string) *time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return ptr(t.UTC())
}

func unixSeconds(value int64) *time.Time {
	if value <= 0 || value >= 253402300800 {
		return nil
	}
	return ptr(time.Unix(value, 0).UTC())
}

func millis(value int64) *time.Time {
	if value <= 0 || value >= 253402300800000 {
		return nil
	}
	return ptr(time.UnixMilli(value).UTC())
}

func durationSeconds(value int64) time.Duration {
	if value <= 0 || value > int64(time.Duration(1<<63-1)/time.Second) {
		return 0
	}
	return time.Duration(value) * time.Second
}

func optionalTime(raw json.RawMessage) *time.Time {
	var date string
	if json.Unmarshal(raw, &date) == nil {
		return parseTime(date)
	}
	var epoch int64
	if json.Unmarshal(raw, &epoch) != nil {
		return nil
	}
	if epoch >= 100_000_000_000 {
		return millis(epoch)
	}
	return unixSeconds(epoch)
}

func optionalInt64(raw json.RawMessage) int64 {
	var value int64
	if json.Unmarshal(raw, &value) != nil {
		return 0
	}
	return value
}
