package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/version"
)

// Repo identifies the GitHub repository used for release checks.
const Repo = "conn-castle/agent-layer"

// ReleasesBaseURL is the base URL for release downloads.
const ReleasesBaseURL = "https://github.com/" + Repo + "/releases"

var latestReleaseURL = "https://api.github.com/repos/" + Repo + "/releases/latest"
var httpClient = &http.Client{Timeout: 10 * time.Second}
var retryDelay = 250 * time.Millisecond

const fetchLatestRetryCount = 1

// RateLimitError indicates GitHub's API rate limit was hit while checking for updates.
//
// Callers should generally treat this as a best-effort failure and suppress/minimize output.
type RateLimitError struct {
	StatusCode int
	Status     string
	Remaining  *int
}

func (e *RateLimitError) Error() string {
	remainingText := "unknown"
	if e.Remaining != nil {
		remainingText = fmt.Sprintf("%d", *e.Remaining)
	}
	return fmt.Sprintf("github api rate limit exceeded (%s, remaining=%s)", e.Status, remainingText)
}

// IsRateLimitError reports whether err represents a GitHub API rate-limit condition.
func IsRateLimitError(err error) bool {
	var rl *RateLimitError
	return errors.As(err, &rl)
}

// CheckResult captures the latest release check outcome.
type CheckResult struct {
	Current      string
	Latest       string
	Outdated     bool
	CurrentIsDev bool
}

// Check fetches the latest release and compares it to the currentVersion.
// It returns the normalized versions along with an outdated flag.
func Check(ctx context.Context, currentVersion string) (CheckResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	current, isDev, err := normalizeCurrentVersion(currentVersion)
	if err != nil {
		return CheckResult{}, err
	}

	latest, err := fetchLatestReleaseVersion(ctx)
	if err != nil {
		return CheckResult{}, err
	}

	result := CheckResult{
		Current:      current,
		Latest:       latest,
		CurrentIsDev: isDev,
	}
	if !isDev {
		cmp, err := compareSemver(current, latest)
		if err != nil {
			return CheckResult{}, err
		}
		result.Outdated = cmp < 0
	}
	return result, nil
}

type latestReleaseResponse struct {
	TagName string `json:"tag_name"`
}

// fetchLatestReleaseVersion returns the normalized latest release tag.
func fetchLatestReleaseVersion(ctx context.Context) (string, error) {
	for attempt := 0; attempt <= fetchLatestRetryCount; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
		if err != nil {
			return "", fmt.Errorf(messages.UpdateCreateRequestErrFmt, err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "agent-layer")

		resp, err := httpClient.Do(req)
		if err != nil {
			if shouldRetryLatestCheck(err, 0, attempt) {
				time.Sleep(retryDelay)
				continue
			}
			return "", fmt.Errorf(messages.UpdateFetchLatestReleaseErrFmt, err)
		}

		if resp.StatusCode != http.StatusOK {
			if rateLimitErr := rateLimitErrorFromResponse(resp); rateLimitErr != nil {
				_ = resp.Body.Close()
				return "", rateLimitErr
			}
			status := resp.StatusCode
			statusText := resp.Status
			_ = resp.Body.Close()
			if shouldRetryLatestCheck(nil, status, attempt) {
				time.Sleep(retryDelay)
				continue
			}
			return "", fmt.Errorf(messages.UpdateFetchLatestReleaseStatusFmt, statusText)
		}

		var payload latestReleaseResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			_ = resp.Body.Close()
			return "", fmt.Errorf(messages.UpdateDecodeLatestReleaseErrFmt, err)
		}
		_ = resp.Body.Close()
		if strings.TrimSpace(payload.TagName) == "" {
			return "", fmt.Errorf(messages.UpdateLatestReleaseMissingTag)
		}
		normalized, err := version.Normalize(payload.TagName)
		if err != nil {
			return "", fmt.Errorf(messages.UpdateInvalidLatestReleaseTagFmt, payload.TagName, err)
		}
		return normalized, nil
	}

	return "", fmt.Errorf(messages.UpdateFetchLatestReleaseErrFmt, errors.New("retry budget exhausted"))
}

func rateLimitErrorFromResponse(resp *http.Response) *RateLimitError {
	if resp == nil {
		return nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return &RateLimitError{StatusCode: resp.StatusCode, Status: resp.Status}
	}
	// GitHub returns 403 Forbidden for unauthenticated exhaustion; confirm with rate-limit headers.
	if resp.StatusCode == http.StatusForbidden {
		remainingStr := strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining"))
		if remainingStr == "" {
			return nil
		}
		remaining, err := strconv.Atoi(remainingStr)
		if err != nil {
			return nil //nolint:nilerr // Malformed header means we cannot confirm rate limiting; fall through to generic error.
		}
		if remaining == 0 {
			return &RateLimitError{StatusCode: resp.StatusCode, Status: resp.Status, Remaining: &remaining}
		}
	}
	return nil
}

func shouldRetryLatestCheck(err error, statusCode int, attempt int) bool {
	if attempt >= fetchLatestRetryCount {
		return false
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		var netErr net.Error
		return errors.As(err, &netErr)
	}
	return statusCode >= 500 && statusCode <= 599
}

// normalizeCurrentVersion validates the current version and reports dev builds.
func normalizeCurrentVersion(raw string) (string, bool, error) {
	if version.IsDev(raw) {
		return "dev", true, nil
	}
	normalized, err := version.Normalize(raw)
	if err != nil {
		return "", false, fmt.Errorf(messages.UpdateInvalidCurrentVersionFmt, raw, err)
	}
	return normalized, false, nil
}

// compareSemver compares two semantic versions in X.Y.Z form.
// It returns -1 if a < b, 0 if a == b, and 1 if a > b.
func compareSemver(a string, b string) (int, error) {
	aParts, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	bParts, err := parseSemver(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < len(aParts); i++ {
		if aParts[i] < bParts[i] {
			return -1, nil
		}
		if aParts[i] > bParts[i] {
			return 1, nil
		}
	}
	return 0, nil
}

// parseSemver converts a semantic version into numeric components.
func parseSemver(raw string) ([3]int, error) {
	normalized, err := version.Normalize(raw)
	if err != nil {
		return [3]int{}, err
	}
	parts := strings.Split(normalized, ".")
	if len(parts) != 3 {
		return [3]int{}, fmt.Errorf(messages.UpdateInvalidVersionFmt, raw)
	}
	var out [3]int
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}, fmt.Errorf(messages.UpdateInvalidVersionSegmentFmt, part, err)
		}
		out[i] = value
	}
	return out, nil
}
