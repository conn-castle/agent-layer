// Package envref owns the vocabulary Agent Layer uses to tell a *reference* to
// a secret from a *literal* one in configuration.
//
// That vocabulary has two halves. The `${NAME}` placeholder syntax names a
// value in `.agent-layer/.env`, and a set of query-parameter key shapes marks a
// URL value as credential-bearing. Both are shared by MCP server configuration
// and Git-backed skill imports, and by both the substitution path that resolves
// a placeholder and the validation paths that must recognize one without
// resolving it.
//
// They live in one leaf package so no caller can drift into a second, weaker
// definition of what counts as a secret.
package envref

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Pattern matches one `${NAME}` placeholder and captures NAME.
var Pattern = regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)

// AgentLayerPrefix is the namespace `.agent-layer/.env` is filtered to, so it
// is also the only namespace a placeholder can resolve from.
const AgentLayerPrefix = "AL_"

// Names returns the variable names referenced by input, in scan order.
// Repeated references are returned once per occurrence.
func Names(input string) []string {
	matches := Pattern.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return nil
	}
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 && match[1] != "" {
			names = append(names, match[1])
		}
	}
	return names
}

// IsAgentLayerName reports whether a placeholder name can resolve from
// `.agent-layer/.env`, which is filtered to the AL_ namespace.
func IsAgentLayerName(name string) bool {
	return strings.HasPrefix(name, AgentLayerPrefix)
}

// IsEntirelyPlaceholders reports whether input is built only from placeholders,
// with no literal text between or around them. An empty input qualifies,
// because it carries no literal value either.
//
// Callers use it to tell a referenced secret from a literal one without
// resolving anything.
func IsEntirelyPlaceholders(input string) bool {
	return Pattern.ReplaceAllString(input, "") == ""
}

// secretQueryTokenSegment is the single segment that appears in several of the
// key shapes below, so the shapes stay in step if it is ever reworded.
const secretQueryTokenSegment = "token"

// secretLikeQueryKeySegments are the query-parameter key shapes that carry a
// credential. Each entry is a run of consecutive identifier segments, so
// `access_token`, `accessToken`, and `ACCESS-TOKEN` all match one shape.
var secretLikeQueryKeySegments = [][]string{
	{secretQueryTokenSegment},
	{"secret"},
	{"password"},
	{"passwd"},
	{"apikey"},
	{"api", "key"},
	{"access", secretQueryTokenSegment},
	{"access", "key"},
	{"auth"},
}

// IsSecretQueryKey reports whether a URL query parameter name marks its value
// as a credential.
func IsSecretQueryKey(key string) bool {
	segments := identifierSegments(key)
	for _, candidate := range secretLikeQueryKeySegments {
		for start := 0; start+len(candidate) <= len(segments); start++ {
			if slices.Equal(segments[start:start+len(candidate)], candidate) {
				return true
			}
		}
	}
	return false
}

// LiteralSecretQueryKey reports the first query parameter in rawURL whose key
// marks a credential and whose value is literal text rather than a placeholder.
//
// It scans the raw string rather than parsing a URL, so it applies equally to a
// reference whose scheme or host is itself a placeholder. Keys are percent-
// decoded so an encoded key cannot slip past the vocabulary above.
func LiteralSecretQueryKey(rawURL string) (string, bool) {
	_, query, hasQuery := strings.Cut(rawURL, "?")
	if !hasQuery {
		return "", false
	}
	// A fragment is not part of the query and never carries a git credential.
	query, _, _ = strings.Cut(query, "#")

	for _, pair := range strings.Split(query, "&") {
		key, value, hasValue := strings.Cut(pair, "=")
		if !hasValue {
			continue
		}
		if decoded, err := url.QueryUnescape(key); err == nil {
			key = decoded
		}
		key = strings.TrimSpace(key)
		if !IsSecretQueryKey(key) {
			continue
		}
		if strings.TrimSpace(value) == "" || IsEntirelyPlaceholders(value) {
			continue
		}
		return key, true
	}
	return "", false
}

// identifierSegments splits an identifier at separators, camel-case boundaries,
// and acronym boundaries, then normalizes the resulting segments for comparison.
func identifierSegments(identifier string) []string {
	runes := []rune(identifier)
	segments := make([]string, 0, len(runes))
	segmentStart := -1

	for i, current := range runes {
		if !unicode.IsLetter(current) && !unicode.IsDigit(current) {
			if segmentStart >= 0 {
				segments = append(segments, strings.ToLower(string(runes[segmentStart:i])))
				segmentStart = -1
			}
			continue
		}

		if segmentStart < 0 {
			segmentStart = i
			continue
		}

		previous := runes[i-1]
		nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
		if unicode.IsUpper(current) &&
			(unicode.IsLower(previous) || unicode.IsDigit(previous) || unicode.IsUpper(previous) && nextIsLower) {
			segments = append(segments, strings.ToLower(string(runes[segmentStart:i])))
			segmentStart = i
		}
	}

	if segmentStart >= 0 {
		segments = append(segments, strings.ToLower(string(runes[segmentStart:])))
	}
	return segments
}
