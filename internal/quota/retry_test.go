package quota

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaRetriesRecoverOnlyFailedReads(t *testing.T) {
	source := subscriptionsFixture(t)[1]
	source.Expected = "claude@example.test"
	routes := allFixtureRoutes(t)
	for _, tc := range []struct {
		name, status, reason string
		recoverIdentity      bool
		permanent            bool
	}{
		{name: "recovered identity", status: statusOK, recoverIdentity: true},
		{name: "transient unverified identity", status: statusMismatch, reason: "account_unverified"},
		{name: "permanent unverified identity", status: statusMismatch, reason: "account_unverified", permanent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := map[string]int{}
				h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
					endpoint := req.URL.String()
					calls[endpoint]++
					if endpoint == claudeOAuth+"profile" && tc.permanent {
						return response(http.StatusOK, "{}")
					}
					if endpoint == claudeOAuth+"profile" && (calls[endpoint] == 1 || !tc.recoverIdentity) {
						return response(http.StatusServiceUnavailable, "{}")
					}
					return response(http.StatusOK, routes[endpoint])
				})
				out := query(t.Context(), Request{Sources: []Source{source}, MaxRetries: 1}, h)
				row := out.Results[0]
				assert.Equal(t, 1, calls[claudeOAuth+"usage"])
				assert.Equal(t, tc.status, row.Status)
				assert.Equal(t, tc.reason, row.Reason)
				assert.Equal(t, !tc.recoverIdentity && !tc.permanent, row.Retryable)
				assert.Equal(t, ptr(fixedTime()), row.ObservedAt)
				if tc.permanent {
					assert.Zero(t, row.Retries)
				} else {
					assert.Equal(t, 1, row.Retries)
					assert.Equal(t, 2, calls[claudeOAuth+"profile"])
				}
			})
		})
	}
}

func TestQuotaHTTPRetryPolicy(t *testing.T) {
	source := subscriptionsFixture(t)[0]
	payload := readFixture(t, agentCodex)
	for _, tc := range []struct {
		name, reason                   string
		code, max, calls               int
		body                           string
		err                            error
		bodyBroken, recover, retryable bool
	}{
		{name: "503 recovery", code: http.StatusServiceUnavailable, max: 2, calls: 2, recover: true},
		{name: "short response body", code: http.StatusOK, max: 1, calls: 2, bodyBroken: true, recover: true},
		{name: "transient connection failure", err: io.EOF, max: 1, calls: 2, recover: true},
		{name: "certificate verification failure", err: &tls.CertificateVerificationError{Err: io.EOF}, max: 2, calls: 1, reason: "request_failed"},
		{name: "limit reached", code: http.StatusBadGateway, max: 2, calls: 3, reason: "provider_unavailable", retryable: true},
		{name: "zero retries", code: http.StatusTooManyRequests, calls: 1, reason: "throttled", retryable: true},
		{name: "unauthorized", code: http.StatusUnauthorized, max: 2, calls: 1, reason: credentialRejected},
		{name: "provider confirmed login expiry", code: http.StatusUnauthorized, body: `{"error":"expired_token"}`, max: 2, calls: 1, reason: reasonExpiredCredentials},
		{name: "forbidden", code: http.StatusForbidden, max: 2, calls: 1, reason: "provider_denied_access"},
		{name: "not found", code: http.StatusNotFound, max: 2, calls: 1, reason: "provider_http_error"},
		{name: "unsupported HTTP method", code: http.StatusNotImplemented, max: 2, calls: 1, reason: "provider_http_error"},
		{name: "malformed JSON", code: http.StatusOK, body: "{sensitive provider failure", max: 2, calls: 1, reason: "invalid_response"},
		{name: "oversized JSON", code: http.StatusOK, body: strings.Repeat(" ", maxPayloadSize+1), max: 2, calls: 1, reason: "invalid_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				h := fixtureHTTP(func(*http.Request) (*http.Response, error) {
					calls++
					if tc.recover && calls > 1 {
						return response(http.StatusOK, payload)
					}
					if tc.err != nil {
						return nil, tc.err
					}
					r, err := response(tc.code, tc.body)
					if tc.bodyBroken {
						r.Body = io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF))
					}
					return r, err
				})
				start := time.Now()
				h.now = func() time.Time { return fixedTime().Add(time.Since(start)) }
				out := query(t.Context(), Request{Sources: []Source{source}, MaxRetries: tc.max}, h)
				row := out.Results[0]
				assert.Equal(t, tc.calls, calls)
				assert.Equal(t, tc.calls-1, row.Retries)
				assert.Equal(t, tc.reason, row.Reason)
				assert.Equal(t, tc.retryable, row.Retryable)
				if tc.recover {
					require.Equal(t, statusOK, row.Status)
					require.NotNil(t, row.ObservedAt)
					assert.True(t, row.ObservedAt.After(row.AttemptedAt))
				} else {
					assert.Nil(t, row.ObservedAt)
				}
				if tc.reason == reasonExpiredCredentials {
					assert.Equal(t, statusLoginExpired, row.Status)
					assert.Equal(t, ptr(true), row.Reauth)
				}
			})
		})
	}
}

func TestQuotaRetryAfterAndPostBody(t *testing.T) {
	source := subscriptionsFixture(t)[4]
	for _, tc := range []struct {
		name, header   string
		budget         time.Duration
		retries, calls int
		hint           *time.Time
	}{
		{name: "seconds", header: "1", budget: 5 * time.Second, retries: 2, calls: 2},
		{name: "HTTP date", header: fixedTime().Add(time.Second).Format(http.TimeFormat), budget: 5 * time.Second, retries: 2, calls: 2},
		{name: "beyond budget", header: "9", budget: time.Second, retries: 2, calls: 1, hint: ptr(fixedTime().Add(9 * time.Second))},
		{name: "zero retries still reports hint", header: "9", budget: time.Second, calls: 1, hint: ptr(fixedTime().Add(9 * time.Second))},
		{name: "long server delay does not trigger a premature retry", header: "999999999999999999999", budget: 5 * time.Second, retries: 2, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), tc.budget)
				defer cancel()
				start := time.Now()
				calls := 0
				h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
					calls++
					assert.Equal(t, http.MethodPost, req.Method)
					raw, err := io.ReadAll(req.Body)
					assert.NoError(t, err)
					assert.Equal(t, `{"onboard":false}`, string(raw))
					if calls > 1 {
						assert.GreaterOrEqual(t, time.Since(start), time.Second)
						return response(http.StatusOK, `{"is_subs_active":true,"user_email":"fixture@example.test"}`)
					}
					r, err := response(http.StatusTooManyRequests, "{}")
					r.Header.Set("Retry-After", tc.header)
					return r, err
				})
				out := query(ctx, Request{Sources: []Source{source}, MaxRetries: tc.retries}, h)
				row := out.Results[0]
				assert.Equal(t, tc.calls, calls)
				assert.Equal(t, tc.calls-1, row.Retries)
				assert.Equal(t, tc.hint, row.RetryAt)
				if tc.calls > 1 {
					assert.Equal(t, reasonNoUsage, row.Reason)
					assert.False(t, row.Retryable)
					assert.Equal(t, ptr(false), row.Reauth)
					assert.NotNil(t, row.ObservedAt)
					assert.True(t, row.Subscription.Verified)
				} else {
					assert.Equal(t, "throttled", row.Reason)
					assert.True(t, row.Retryable)
					assert.Nil(t, row.ObservedAt)
				}
			})
		})
	}
}

func TestQuotaRetryReservesTimeAfterHungAttempt(t *testing.T) {
	source := subscriptionsFixture(t)[0]
	payload := readFixture(t, agentCodex)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
		defer cancel()
		start := time.Now()
		calls := 0
		h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				<-req.Context().Done()
				assert.Equal(t, 2*time.Second, time.Since(start))
				return nil, req.Context().Err()
			}
			return response(http.StatusOK, payload)
		})
		out := query(ctx, Request{Sources: []Source{source}, MaxRetries: 5}, h)
		assert.Equal(t, ResponseComplete, out.Status)
		assert.Equal(t, 1, out.Results[0].Retries)
		assert.Equal(t, 2, calls)
	})
}

func TestQuotaCancelDuringBackoff(t *testing.T) {
	source := subscriptionsFixture(t)[0]
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		calls := 0
		h := fixtureHTTP(func(*http.Request) (*http.Response, error) {
			calls++
			return response(http.StatusServiceUnavailable, "{}")
		})
		finished := make(chan Response, 1)
		go func() { finished <- query(ctx, Request{Sources: []Source{source}, MaxRetries: 2}, h) }()
		synctest.Wait()
		cancel()
		result := <-finished
		row := result.Results[0]
		assert.Equal(t, 1, calls)
		assert.Equal(t, "canceled", row.Reason)
		assert.Zero(t, row.Retries)
		assert.True(t, row.Retryable)
	})
}

func TestQuotaExpiredLoginAndAccessToken(t *testing.T) {
	now := fixedTime()
	for _, tc := range []struct {
		name           string
		credential     credential
		status, reason string
		reauth         *bool
	}{
		{"no renewal", credential{}, statusLoginExpired, reasonExpiredCredentials, ptr(true)},
		{"renewal expired", credential{refreshable: true, refreshExpires: ptr(now)}, statusLoginExpired, reasonExpiredCredentials, ptr(true)},
		{"renewal lifetime known", credential{refreshable: true, refreshExpires: ptr(now.Add(time.Hour))}, statusCredentials, "access_token_expired", ptr(false)},
		{"renewal lifetime unknown", credential{refreshable: true}, statusCredentials, "access_token_expired", nil},
		{"external token source", credential{renewalUnknown: true}, statusCredentials, "access_token_expired", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fail := tc.credential.authenticationFailure(reasonExpiredCredentials, now)
			assert.Equal(t, tc.status, fail.status)
			assert.Equal(t, tc.reason, fail.reason)
			assert.Equal(t, tc.reauth, fail.reauth)
			assert.False(t, fail.retryable)
		})
	}
}

func TestQuotaExpiredLoginRotationIsNotANetworkRetry(t *testing.T) {
	source := subscriptionsFixture(t)[0]
	payload := readFixture(t, agentCodex)
	calls := 0
	h := fixtureHTTP(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Header.Get("Authorization") == "Bearer "+fakeToken {
			temp := source.File + ".new"
			writeFixture(t, temp, strings.Replace(codexCredentialFixture("workspace-fixture"), fakeToken, "rotated-fixture", 1))
			assert.NoError(t, os.Rename(temp, source.File))
			return response(http.StatusUnauthorized, `{"error":"expired_token"}`)
		}
		assert.Equal(t, "Bearer rotated-fixture", req.Header.Get("Authorization"))
		return response(http.StatusOK, payload)
	})
	out := query(t.Context(), Request{Sources: []Source{source}, MaxRetries: 2}, h)
	assert.Equal(t, ResponseComplete, out.Status)
	assert.Equal(t, 2, calls)
	assert.Zero(t, out.Results[0].Retries)
}

func TestQuotaOfflineSkipsRequestsAndRetries(t *testing.T) {
	source := subscriptionsFixture(t)[0]
	h := fixtureHTTP(func(*http.Request) (*http.Response, error) {
		t.Error("offline query sent a request")
		return nil, io.ErrUnexpectedEOF
	})
	out := query(t.Context(), Request{Sources: []Source{source}, MaxRetries: 2, Offline: true}, h)
	assert.Equal(t, ResponseNoReadings, out.Status)
	assert.Equal(t, "network_disabled", out.Results[0].Reason)
	assert.Zero(t, out.Results[0].Retries)
	assert.False(t, out.Results[0].Retryable)
}
