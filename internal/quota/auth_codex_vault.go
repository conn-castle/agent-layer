package quota

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"filippo.io/age"
)

const (
	codexSecretsDirectory = "secrets"
	codexVaultFilename    = "codex_auth.age"
	codexVaultEntry       = "global/CODEX_AUTH"
	codexVaultUnavailable = "codex_vault_key_unavailable"
	// Bound scrypt memory even if a ciphertext supplies a hostile work factor.
	codexVaultMaxWork = 20
)

type keyringReader func(context.Context, string, string) ([]byte, failure)

// age's scrypt operation cannot be canceled. Limit it to one active
// derivation and let the caller stop waiting when its context expires.
var codexVaultDecryptSlot = make(chan struct{}, 1)

var errCodexVaultWorkFactor = errors.New("vault scrypt work factor exceeds limit")

type codexVaultIdentity struct{ *age.ScryptIdentity }

func (i codexVaultIdentity) Unwrap(stanzas []*age.Stanza) ([]byte, error) {
	if len(stanzas) == 1 && stanzas[0].Type == "scrypt" && len(stanzas[0].Args) == 2 {
		if work, err := strconv.Atoi(stanzas[0].Args[1]); err == nil && work > codexVaultMaxWork {
			return nil, errCodexVaultWorkFactor
		}
	}
	return i.ScryptIdentity.Unwrap(stanzas)
}

func readCodexVault(ctx context.Context, path, home string, read keyringReader) ([]byte, failure) {
	ciphertext, fail := credentialFile(path)
	if fail.status != "" {
		return nil, fail
	}
	key, fail := read(ctx, "codex", codexKeyringAccount(home, "secrets"))
	if ctx.Err() != nil {
		return nil, contextFailure(ctx)
	}
	if fail.status != "" || len(key) == 0 {
		// A vault with an inaccessible key may still be the active login.
		return nil, unsupportedAuth(codexVaultUnavailable)
	}
	select {
	case codexVaultDecryptSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, contextFailure(ctx)
	}

	type result struct {
		auth []byte
		fail failure
	}
	finished := make(chan result, 1)
	go func() {
		defer func() { <-codexVaultDecryptSlot }()
		auth, fail := decryptCodexVault(ciphertext, key)
		finished <- result{auth: auth, fail: fail}
	}()
	select {
	case decrypted := <-finished:
		return decrypted.auth, decrypted.fail
	case <-ctx.Done():
		return nil, contextFailure(ctx)
	}
}

func decryptCodexVault(ciphertext, key []byte) ([]byte, failure) {
	unreadable := failure{status: statusCredentials, reason: "codex_vault_unreadable"}
	identity, err := age.NewScryptIdentity(string(key))
	if err != nil {
		return nil, unreadable
	}
	identity.SetMaxWorkFactor(codexVaultMaxWork)
	reader, err := age.Decrypt(bytes.NewReader(ciphertext), codexVaultIdentity{identity})
	if errors.Is(err, errCodexVaultWorkFactor) {
		return nil, unsupportedAuth("codex_vault_work_factor_unsupported")
	}
	if err != nil {
		return nil, unreadable
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxPayloadSize+1))
	if err != nil || len(raw) > maxPayloadSize {
		return nil, unreadable
	}
	var data struct {
		Version *uint8            `json:"version"`
		Secrets map[string]string `json:"secrets"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Version == nil || data.Secrets == nil {
		return nil, unreadable
	}
	if *data.Version > 1 {
		return nil, unsupportedAuth("codex_vault_version_unsupported")
	}
	auth, exists := data.Secrets[codexVaultEntry]
	if !exists {
		return nil, missingCredentials()
	}
	return []byte(auth), failure{}
}
