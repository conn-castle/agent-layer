package quota

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaSummaryPreservesRoutingSignals(t *testing.T) {
	now := fixedTime()
	reset := now.Add(time.Hour)
	data := Response{
		SchemaVersion: SchemaVersion, Status: ResponsePartial,
		Missing: []MissingReading{{Selector: "other", Agent: agentCodex, Status: statusMismatch, Reason: "unexpected_account"}},
		Results: []Result{
			{
				Selector: "work", Agent: agentCodex, Status: statusOK,
				ObservedAt: &now, Reauth: ptr(false), Retries: 1, AccountCheck: "matched",
				Admission: knownAllowed(),
				Pools: []Pool{
					{ID: "reviews", Scope: scopeOther, Windows: []Window{{UsedPercent: ptr(100.0)}}},
					{ID: "shared", Scope: scopeAll, Windows: []Window{
						{ID: "disabled", UsedPercent: ptr(100.0), Disabled: ptr(true)},
						{ID: "unknown"},
						{ID: weeklyID, UsedPercent: ptr(25.0)},
					}},
					{ID: "specific", Scope: scopeModels, Windows: []Window{
						{ID: fiveHourID, UsedPercent: ptr(75.0), ResetsAt: &reset},
					}},
				},
				Models: []Model{
					{ID: "configured", Coverage: coverageComplete, Admission: knownBlocked(&reset)},
					{ID: "discovered", Coverage: coverageComplete},
					{ID: "requested", Coverage: scopeUnknown, AuthStatus: statusNonSubscription},
				},
			},
			{
				Selector: "other", Agent: agentCodex, Status: statusMismatch, Reason: "unexpected_account",
				AccountCheck: "mismatch", Admission: knownAllowed(),
				Pools:  []Pool{{ID: "shared", Scope: scopeAll, Windows: []Window{{UsedPercent: ptr(99.0)}}}},
				Models: []Model{{ID: "requested", Coverage: coverageComplete, Admission: knownAllowed()}},
			},
		},
	}
	report := data.Summary([]Source{
		{Selector: "other", Models: []string{"requested"}},
		{Selector: "work", ConfiguredModel: "configured", Models: []string{"requested"}},
	})
	assert.Equal(t, data.Status, report.Status)
	assert.Equal(t, data.Missing, report.Missing)
	require.Len(t, report.Results, 2)
	row, mismatch := report.Results[0], report.Results[1]
	assert.Equal(t, "work", row.Selector)
	assert.Equal(t, &now, row.ObservedAt)
	assert.Equal(t, 1, row.Retries)
	assert.Equal(t, "matched", row.AccountCheck)
	assert.Equal(t, &summaryWindow{Pool: "specific", Scope: scopeModels, Window: fiveHourID, UsedPercent: 75, ResetsAt: &reset}, row.HighestUsage)
	assert.Equal(t, []summaryModel{
		{ID: "configured", Coverage: coverageComplete, Admission: knownBlocked(&reset)},
		{ID: "requested", Coverage: scopeUnknown, AuthStatus: statusNonSubscription},
	}, row.Models)
	assert.Nil(t, mismatch.HighestUsage)
	assert.Nil(t, mismatch.Blocked)
	assert.Empty(t, mismatch.Models)

	emptyModelReport := Response{
		SchemaVersion: SchemaVersion, Status: ResponseComplete, Missing: []MissingReading{},
		Results: []Result{{
			Selector: "empty", Agent: agentCodex, Status: statusOK,
			Models: []Model{{ID: "", Coverage: coverageComplete, Admission: knownAllowed()}},
		}},
	}.Summary([]Source{{Selector: "empty"}})
	require.Len(t, emptyModelReport.Results, 1)
	assert.Empty(t, emptyModelReport.Results[0].Models)

	evaluated := Result{Agent: agentCodex, Models: []Model{{ID: ""}, {ID: "valid"}}}
	evaluated.evaluateModels(nil)
	require.Len(t, evaluated.Models, 1)
	assert.Equal(t, "valid", evaluated.Models[0].ID)
}
