package quota

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	mathrand "math/rand/v2"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	maxPayloadSize     = 2 << 20
	credentialRejected = "credentials_rejected"
	credentialRevoked  = "revoked_credentials" // #nosec G101 -- public result reason.
	maxRetryHint       = 24 * time.Hour
	reasonTimeout      = "timeout"
)

type httpReader struct {
	client     *http.Client
	now        func() time.Time
	maxRetries int
	retries    *atomic.Int32
}

func newHTTPReader() *httpReader {
	// Do not delegate credentials to ambient HTTP proxy settings.
	return &httpReader{
		client: &http.Client{
			Transport: &http.Transport{ForceAttemptHTTP2: true},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (h *httpReader) fetch(ctx context.Context, auth credential, method, endpoint, body string, headers http.Header, output any) (*time.Time, failure) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(body))
	if err != nil || req.URL.Scheme != "https" {
		return nil, unavailable("request_failed")
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Authorization", "Bearer "+auth.token)
	req.Header.Set("Accept", "application/json")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	// All callers use audited read-only requests, including the provider RPCs
	// with POST bodies. Rebuild the body for every explicit retry.
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return nil, contextFailure(ctx)
		}
		attemptCtx, cancel := quotaAttemptContext(ctx, h.maxRetries-attempt+1)
		request := req.Clone(attemptCtx)
		request.Body, _ = req.GetBody()
		if attempt > 0 && h.retries != nil {
			h.retries.Add(1)
		}
		when, fail := h.fetchAttempt(request, auth, output)
		cancel()
		if fail.status == "" {
			return when, fail
		}
		if ctx.Err() != nil {
			return nil, contextFailure(ctx)
		}
		if !fail.retryable || attempt >= h.maxRetries {
			return nil, fail
		}
		delay := quotaRetryDelay(attempt, fail.retryAt, h.now())
		if !canWaitForQuotaRetry(ctx, delay) {
			return nil, fail
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, contextFailure(ctx)
		}
	}
}

func quotaAttemptContext(ctx context.Context, attemptsLeft int) (context.Context, context.CancelFunc) {
	deadline, bounded := ctx.Deadline()
	if !bounded || attemptsLeft <= 1 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Until(deadline)/time.Duration(min(attemptsLeft, 3)))
}

func (h *httpReader) fetchAttempt(req *http.Request, auth credential, output any) (*time.Time, failure) {
	ctx := req.Context()
	response, err := h.client.Do(req)
	if err != nil {
		return nil, quotaNetworkFailure(ctx, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, quotaHTTPFailure(response, req.URL.Hostname(), auth, h.now())
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxPayloadSize+1))
	if err != nil {
		return nil, quotaNetworkFailure(ctx, err)
	}
	if ctx.Err() != nil {
		return nil, contextFailure(ctx)
	}
	if len(raw) > maxPayloadSize || unmarshalClean(raw, output) != nil {
		return nil, unavailable("invalid_response")
	}
	// Timestamp receipt of the complete reading, which can lag its headers.
	return ptr(h.now()), failure{}
}

func unmarshalClean(raw []byte, output any) error {
	rv := reflect.ValueOf(output)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return json.Unmarshal(raw, output)
	}
	tmp := reflect.New(rv.Type().Elem())
	if err := json.Unmarshal(raw, tmp.Interface()); err != nil {
		return err
	}
	rv.Elem().Set(tmp.Elem())
	return nil
}

func quotaNetworkFailure(ctx context.Context, err error) failure {
	if ctx.Err() != nil {
		return contextFailure(ctx)
	}
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	var protocol tls.RecordHeaderError
	if errors.As(err, &dns) && dns.IsNotFound || errors.As(err, &certificate) || errors.As(err, &protocol) {
		return unavailable("request_failed")
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return transientFailure(reasonTimeout)
	}
	return transientFailure("network_error")
}

func quotaHTTPFailure(response *http.Response, host string, auth credential, now time.Time) failure {
	var fail failure
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		fail = httpAuthFailure(response, host, auth, now)
		fail.remote = true
		return fail
	case http.StatusTooManyRequests:
		fail = transientFailure("throttled")
	case http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		fail = transientFailure("provider_unavailable")
	default:
		return unavailable("provider_http_error")
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		fail.retryAt = serverRetryAt(response.Header.Get("Retry-After"), now)
	}
	// Bounded draining may let the transport reuse the connection.
	_, _ = io.CopyN(io.Discard, response.Body, 4<<10)
	return fail
}

func quotaRetryDelay(attempt int, retryAt *time.Time, now time.Time) time.Duration {
	backoff := (500 * time.Millisecond) << min(attempt, 3)
	wait := backoff/2 + mathrand.N(backoff/2+1) // #nosec G404 -- jitter only spreads read retries.
	if retryAt != nil {
		wait = max(wait, retryAt.Sub(now))
	}
	return wait
}

func canWaitForQuotaRetry(ctx context.Context, wait time.Duration) bool {
	deadline, bounded := ctx.Deadline()
	if !bounded {
		// The public command always has a deadline. Bound callers that omit it.
		return wait < time.Minute
	}
	remaining := time.Until(deadline)
	return remaining > 0 && wait < remaining-min(250*time.Millisecond, remaining/4)
}

func serverRetryAt(header string, now time.Time) *time.Time {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	if strings.Trim(header, "0123456789") == "" {
		seconds, err := strconv.ParseUint(header, 10, 64)
		if err != nil || seconds > uint64(maxRetryHint/time.Second) {
			// A long delay cannot fit the command budget. Keep it out of the
			// public hint without treating it as permission to retry sooner.
			return ptr(now.Add(2 * maxRetryHint))
		}
		if seconds > 0 {
			return ptr(now.Add(time.Duration(seconds) * time.Second).UTC())
		}
		return nil
	}
	if at, err := http.ParseTime(header); err == nil && at.After(now) {
		return ptr(at.UTC())
	}
	return nil
}

func publishRetryAt(at *time.Time, now time.Time) *time.Time {
	if at == nil || !at.After(now) || at.Sub(now) > maxRetryHint {
		return nil
	}
	return at
}

func contextFailure(ctx context.Context) failure {
	if ctx.Err() == context.DeadlineExceeded {
		return transientFailure(reasonTimeout)
	}
	return transientFailure("canceled")
}

func retainRead(r *Result, observed *time.Time, failed failure, component string) {
	if failed.status != "" {
		r.Missing = append(r.Missing, component)
		return
	}
	if observed != nil && (r.ObservedAt == nil || observed.Before(*r.ObservedAt)) {
		r.ObservedAt = observed
	}
}

// Match known provider errors without returning provider response bodies.
func httpAuthFailure(response *http.Response, host string, auth credential, now time.Time) failure {
	var body struct {
		Code    string          `json:"code"`
		Type    string          `json:"type"`
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	const maxAuthErrorSize = 4 << 10
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxAuthErrorSize))
	if err == nil && json.Unmarshal(raw, &body) == nil {
		var nested struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		var code string
		if json.Unmarshal(body.Error, &code) != nil {
			_ = json.Unmarshal(body.Error, &nested)
		}
		if response.StatusCode == http.StatusForbidden && host == "api.anthropic.com" &&
			(strings.Contains(nested.Message, "OAuth token has been revoked") || strings.Contains(body.Message, "OAuth token has been revoked")) {
			return failure{status: statusLoginExpired, reason: credentialRevoked, reauth: ptr(true)}
		}
		switch textFrom(nested.Code, nested.Type, code, body.Code, body.Type) {
		case "session_revoked", "refresh_token_revoked":
			return failure{status: statusLoginExpired, reason: credentialRevoked, reauth: ptr(true)}
		case "token_revoked", "revoked_token":
			return auth.authenticationFailure(credentialRevoked, now)
		case "token_expired", "expired_token":
			return auth.authenticationFailure(reasonExpiredCredentials, now)
		case "insufficient_scope":
			return unsupportedAuth("oauth_scope_unavailable")
		}
	}
	if response.StatusCode == http.StatusUnauthorized {
		return auth.authenticationFailure(credentialRejected, now)
	}
	return unavailable("provider_denied_access")
}
