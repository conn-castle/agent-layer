package envref

import "testing"

// TestNamesReportsEveryReference proves the scan feeds both the substitution
// path and the validation paths the same list of variables.
func TestNamesReportsEveryReference(t *testing.T) {
	t.Parallel()
	got := Names("https://${AL_USER}:${AL_TOKEN}@${AL_HOST}/repo.git")
	want := []string{"AL_USER", "AL_TOKEN", "AL_HOST"}
	if len(got) != len(want) {
		t.Fatalf("Names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names = %v, want %v", got, want)
		}
	}
	if Names("https://example.test/repo.git") != nil {
		t.Fatal("a reference with no placeholder reported names")
	}
	// Lowercase and malformed forms are not placeholders, so they stay literal
	// text rather than becoming references that could never resolve.
	for _, input := range []string{"${al_token}", "$AL_TOKEN", "${AL-TOKEN}", "{AL_TOKEN}"} {
		if names := Names(input); names != nil {
			t.Fatalf("Names(%q) = %v, want none", input, names)
		}
	}
}

// TestIsEntirelyPlaceholdersSeparatesReferencesFromLiterals proves the check
// callers use to tell a referenced secret from a literal one, without resolving
// anything.
func TestIsEntirelyPlaceholdersSeparatesReferencesFromLiterals(t *testing.T) {
	t.Parallel()
	entirely := []string{"", "${AL_TOKEN}", "${AL_USER}${AL_TOKEN}"}
	for _, input := range entirely {
		if !IsEntirelyPlaceholders(input) {
			t.Fatalf("IsEntirelyPlaceholders(%q) = false", input)
		}
	}
	// Any literal character around or between placeholders means real secret
	// text is present, so the value is not merely a reference.
	partial := []string{"token", "${AL_TOKEN}x", "x${AL_TOKEN}", "${AL_USER}:${AL_TOKEN}"}
	for _, input := range partial {
		if IsEntirelyPlaceholders(input) {
			t.Fatalf("IsEntirelyPlaceholders(%q) = true", input)
		}
	}
}

// TestIsAgentLayerNameMatchesTheEnvFilter proves the namespace check agrees
// with the AL_ filter .agent-layer/.env is loaded through, so a name that
// passes here can actually resolve.
func TestIsAgentLayerNameMatchesTheEnvFilter(t *testing.T) {
	t.Parallel()
	if !IsAgentLayerName("AL_SKILLS_TOKEN") || !IsAgentLayerName("AL_REPO_ROOT") {
		t.Fatal("an AL_ name was rejected")
	}
	for _, name := range []string{"SKILLS_TOKEN", "GITHUB_TOKEN", "PATH"} {
		if IsAgentLayerName(name) {
			t.Fatalf("%q was treated as resolvable", name)
		}
	}
}

// TestIsSecretQueryKeyRecognizesCredentialParameters pins the shared vocabulary
// that marks a URL query value as a credential. Both the MCP policy warning and
// skill import repository validation read it, so a change here changes what
// both of them treat as a secret.
func TestIsSecretQueryKeyRecognizesCredentialParameters(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key  string
		want bool
	}{
		{key: "token", want: true},
		{key: "secret", want: true},
		{key: "password", want: true},
		{key: "passwd", want: true},
		{key: "apikey", want: true},
		{key: "api_key", want: true},
		{key: "access_token", want: true},
		{key: "access-key", want: true},
		{key: "auth", want: true},
		{key: "accessToken", want: true},
		{key: "authToken", want: true},
		{key: "apiToken", want: true},
		{key: "clientSecret", want: true},
		{key: "APIKey", want: true},
		{key: "my-access-token-value", want: true},
		// Segmentation is what keeps these out: they contain a secret word but
		// never as a standalone segment run.
		{key: "author", want: false},
		{key: "authority", want: false},
		{key: "tokenizer", want: false},
		{key: "passwordless", want: false},
		{key: "authtoken", want: false},
		{key: "accesstoken", want: false},
		{key: "clientsecret", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			if got := IsSecretQueryKey(tc.key); got != tc.want {
				t.Fatalf("IsSecretQueryKey(%q) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}

// TestLiteralSecretQueryKeySeparatesLiteralsFromReferences proves the scan
// finds a credential-bearing query parameter without parsing a URL, so it works
// on a reference whose scheme or host is itself a placeholder.
func TestLiteralSecretQueryKeySeparatesLiteralsFromReferences(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		rawURL  string
		wantKey string
	}{
		{name: "no query", rawURL: "https://example.test/skills.git"},
		{name: "ordinary parameter", rawURL: "https://example.test/skills.git?depth=1"},
		{name: "literal token", rawURL: "https://example.test/skills.git?access_token=literal", wantKey: "access_token"},
		{name: "placeholder value", rawURL: "https://example.test/skills.git?access_token=${AL_TOKEN}"},
		{name: "empty value", rawURL: "https://example.test/skills.git?token="},
		{name: "second parameter", rawURL: "https://example.test/s.git?depth=1&api_key=literal", wantKey: "api_key"},
		// The scheme and host being placeholders must not hide the query.
		{name: "placeholder scheme", rawURL: "${AL_SCHEME}://${AL_HOST}/s.git?token=literal", wantKey: "token"},
		// A percent-encoded key must not slip past the vocabulary.
		{name: "encoded key", rawURL: "https://example.test/s.git?access%5Ftoken=literal", wantKey: "access_token"},
		// A fragment is not part of the query.
		{name: "fragment only", rawURL: "https://example.test/s.git#token=literal"},
		// Partly-literal values still carry real secret text.
		{name: "partly literal", rawURL: "https://example.test/s.git?token=${AL_TOKEN}x", wantKey: "token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			key, found := LiteralSecretQueryKey(tc.rawURL)
			if tc.wantKey == "" {
				if found {
					t.Fatalf("LiteralSecretQueryKey(%q) = %q, want none", tc.rawURL, key)
				}
				return
			}
			if !found || key != tc.wantKey {
				t.Fatalf("LiteralSecretQueryKey(%q) = (%q, %v), want %q", tc.rawURL, key, found, tc.wantKey)
			}
		})
	}
}
