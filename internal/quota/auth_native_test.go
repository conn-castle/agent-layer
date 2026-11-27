package quota

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaCodexVaultSelectsNativeSlot(t *testing.T) {
	home := t.TempDir()
	canonical, err := filepath.EvalSymlinks(home)
	require.NoError(t, err)
	link := filepath.Join(t.TempDir(), "codex-home")
	require.NoError(t, os.Symlink(home, link))
	vault := filepath.Join(home, codexSecretsDirectory, codexVaultFilename)
	authJSON := codexCredentialFixture("vault-workspace")
	const key = "fixture-vault-passphrase"
	plain, err := json.Marshal(map[string]any{
		"version": 1,
		"secrets": map[string]string{
			"global/CODEX_AUTH": authJSON,
			"other/CODEX_AUTH":  codexCredentialFixture("other-workspace"),
		},
	})
	require.NoError(t, err)
	ciphertext := encryptVaultFixture(t, plain, key)
	writeFixture(t, vault, string(ciphertext))
	writeFixture(t, filepath.Join(home, authFilename), codexCredentialFixture("older-workspace"))

	settings := codexSettings{home: link, store: "auto", vault: true}
	readKey := func(_ context.Context, service, account string) ([]byte, failure) {
		sum := sha256.Sum256([]byte(canonical))
		assert.Equal(t, "codex", service)
		assert.Equal(t, fmt.Sprintf("secrets|%x", sum[:8]), account)
		return []byte(key), failure{}
	}
	raw, fail := codexStoredCredential(t.Context(), settings, readKey)
	require.Empty(t, fail.status)
	auth, fail := parseCodexCredential(raw)
	require.Empty(t, fail.status)
	assert.Equal(t, "vault-workspace", auth.account)
	retained, err := readBoundedFile(vault)
	require.NoError(t, err)
	assert.Equal(t, ciphertext, retained)

	t.Run("relative detached path", func(t *testing.T) {
		t.Chdir(link)
		raw, fail := readCodexVault(t.Context(), filepath.Join(codexSecretsDirectory, codexVaultFilename), ".", readKey)
		require.Empty(t, fail.status)
		assert.Equal(t, authJSON, string(raw))
	})

	raw, fail = codexStoredCredential(t.Context(), settings, func(context.Context, string, string) ([]byte, failure) {
		return nil, missingCredentials()
	})
	assert.Empty(t, raw)
	assert.Equal(t, codexVaultUnavailable, fail.reason)
}

func TestQuotaCodexVaultBadAndAbsentRecords(t *testing.T) {
	const key = "fixture-vault-passphrase"
	for _, tc := range []struct {
		name, content, passphrase, reason string
	}{
		{"missing auth", `{"version":1,"secrets":{"global/UNRELATED":"synthetic"}}`, key, reasonMissingCredential},
		{"missing schema fields", `{}`, key, "codex_vault_unreadable"},
		{"future version", `{"version":2,"secrets":{}}`, key, "codex_vault_version_unsupported"},
		{"wrong passphrase", `{"version":1,"secrets":{}}`, "fixture-wrong-key", "codex_vault_unreadable"},
		{"invalid plaintext", "corrupt fixture", key, "codex_vault_unreadable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, fail := decryptCodexVault(encryptVaultFixture(t, []byte(tc.content), key), []byte(tc.passphrase))
			assert.Empty(t, raw)
			assert.Equal(t, tc.reason, fail.reason)
		})
	}
}

func TestQuotaCodexVaultWorkFactorLimit(t *testing.T) {
	const key = "fixture-vault-passphrase"
	ciphertext := encryptVaultFixture(t, []byte(`{"version":1,"secrets":{}}`), key)
	// Change only the stanza's work factor and assert rejection before scrypt.
	ciphertext = bytes.Replace(ciphertext, []byte(" 10\n"), []byte(" 21\n"), 1)
	raw, fail := decryptCodexVault(ciphertext, []byte(key))
	assert.Empty(t, raw)
	assert.Equal(t, "codex_vault_work_factor_unsupported", fail.reason)
}

func encryptVaultFixture(t *testing.T, plaintext []byte, key string) []byte {
	t.Helper()
	recipient, err := age.NewScryptRecipient(key)
	require.NoError(t, err)
	recipient.SetWorkFactor(10)
	var ciphertext bytes.Buffer
	writer, err := age.Encrypt(&ciphertext, recipient)
	require.NoError(t, err)
	_, err = writer.Write(plaintext)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return ciphertext.Bytes()
}

func TestQuotaMuseMacOSKeychainSelection(t *testing.T) {
	xdg := t.TempDir()
	path := filepath.Join(xdg, agentMuse, authFilename)
	const metadata = `{"schema_version":1,"providers":{"meta":{"mechanism":"oauth","storage":"keychain","access_token":"stale-file-token","expires_at":"2026-12-10T04:00:00Z"}}}`
	const payload = `{"secret_schema_version":1,"access_token":"keychain-token","refresh_token":"keychain-refresh"}`
	writeFixture(t, path, metadata)

	for _, tc := range []struct {
		name, platform, backend, raw, token, reason string
		detached, lookup                            bool
		keyringFail                                 failure
	}{
		{name: "default Mac backend", platform: platformDarwin, raw: payload, token: "keychain-token", lookup: true},
		{name: "explicit keychain hex output", platform: platformDarwin, backend: "keychain", raw: hex.EncodeToString([]byte(payload)), token: "keychain-token", lookup: true},
		{name: "missing keychain entry", platform: platformDarwin, lookup: true, keyringFail: missingCredentials(), reason: "muse_keychain_entry_unavailable"},
		{name: "locked keychain", platform: platformDarwin, lookup: true, keyringFail: unsupportedAuth(keyringUnavailable), reason: keyringUnavailable},
		{name: "malformed payload", platform: platformDarwin, lookup: true, raw: "not valid JSON", reason: "invalid_credentials"},
		{name: "future keychain schema", platform: platformDarwin, lookup: true, raw: `{"secret_schema_version":2}`, reason: "muse_keychain_version_unsupported"},
		{name: "forced file", platform: platformDarwin, backend: "file", reason: keyringUnavailable},
		{name: "detached marker cannot select ambient account", platform: platformDarwin, detached: true, reason: keyringUnavailable},
		{name: "Linux marker cannot select ambient account", platform: "linux", reason: keyringUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := credentialSource{env: []string{"XDG_CONFIG_HOME=" + xdg, "TBH_CREDENTIAL_BACKEND=" + tc.backend}}
			if tc.detached {
				s.file = path
			}
			called := false
			auth, fail := museCredentialFrom(t.Context(), s, tc.platform, func(_ context.Context, service, account string) ([]byte, failure) {
				called = true
				assert.Equal(t, "ai.meta.dev.credentials", service)
				assert.Equal(t, "meta", account)
				return []byte(tc.raw), tc.keyringFail
			})
			assert.Equal(t, tc.lookup, called)
			assert.Equal(t, tc.reason, fail.reason)
			assert.Equal(t, tc.token, auth.token)
			if tc.token != "" {
				assert.True(t, auth.refreshable)
				require.NotNil(t, auth.expires)
				assert.Equal(t, 2026, auth.expires.Year())
			}
		})
	}
	content, err := readBoundedFile(path)
	require.NoError(t, err)
	assert.Equal(t, metadata, string(content))
}
