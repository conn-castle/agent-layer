package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/version"
)

// Repo identifies the GitHub repository used for release checks.
const Repo = "conn-castle/agent-layer"

// ReleasesBaseURL is the base URL for release downloads.
const ReleasesBaseURL = "https://github.com/" + Repo + "/releases"

var latestReleaseURL = "https://api.github.com/repos/" + Repo + "/releases/latest"
var httpClient = &http.Client{Timeout: 10 * time.Second}

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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", fmt.Errorf("create latest release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "agent-layer")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch latest release: unexpected status %s", resp.Status)
	}

	var payload latestReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode latest release: %w", err)
	}
	if strings.TrimSpace(payload.TagName) == "" {
		return "", fmt.Errorf("latest release missing tag_name")
	}
	normalized, err := version.Normalize(payload.TagName)
	if err != nil {
		return "", fmt.Errorf("invalid latest release tag %q: %w", payload.TagName, err)
	}
	return normalized, nil
}

// normalizeCurrentVersion validates the current version and reports dev builds.
func normalizeCurrentVersion(raw string) (string, bool, error) {
	if version.IsDev(raw) {
		return "dev", true, nil
	}
	normalized, err := version.Normalize(raw)
	if err != nil {
		return "", false, fmt.Errorf("invalid current version %q: %w", raw, err)
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
		return [3]int{}, fmt.Errorf("invalid version %q", raw)
	}
	var out [3]int
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}, fmt.Errorf("invalid version segment %q: %w", part, err)
		}
		out[i] = value
	}
	return out, nil
}
