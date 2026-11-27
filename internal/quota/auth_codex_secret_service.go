package quota

import "context"

const reasonAmbiguousSlot = "ambiguous_credential_slot"

type codexSecretService interface {
	searchItems(ctx context.Context, attrs map[string]string) (unlocked, locked []string, err error)
	readDefaultCollection(ctx context.Context) (string, error)
	collectionLocked(ctx context.Context, collection string) (bool, error)
	collectionSearchItems(ctx context.Context, collection string, attrs map[string]string) ([]string, error)
	itemAttributes(ctx context.Context, item string) (map[string]string, error)
	itemLocked(ctx context.Context, item string) (bool, error)
	itemSecret(ctx context.Context, item string) ([]byte, error)
}

// readCodexSecretService mirrors keyring 3.6.3's SsCredential lookup on Linux:
// first select a unique service+username+target=default match across Secret
// Service, then fall back to a unique targetless legacy item in the default
// collection. Ambiguous or locked stores fail closed; only an unlocked default
// collection (or absent default alias) with no matching item proves absence.
func readCodexSecretService(ctx, callCtx context.Context, store codexSecretService, service, account string) ([]byte, failure) {
	unlocked, locked, err := store.searchItems(callCtx, map[string]string{
		keyringServiceAttr:  service,
		keyringUsernameAttr: account,
		"target":            "default",
	})
	if err != nil {
		return nil, secretServiceFailure(ctx, callCtx)
	}
	if len(unlocked)+len(locked) > 1 {
		return nil, unsupportedAuth(reasonAmbiguousSlot)
	}
	if len(locked) == 1 {
		return nil, unsupportedAuth(keyringUnavailable)
	}
	if len(unlocked) == 1 {
		return readCodexSecretItem(ctx, callCtx, store, unlocked[0])
	}

	collection, err := store.readDefaultCollection(callCtx)
	if err != nil {
		return nil, secretServiceFailure(ctx, callCtx)
	}
	if collection == "" || collection == "/" {
		return nil, missingCredentials()
	}
	items, err := store.collectionSearchItems(callCtx, collection, map[string]string{
		keyringServiceAttr:  service,
		keyringUsernameAttr: account,
	})
	if err != nil {
		return nil, secretServiceFailure(ctx, callCtx)
	}
	var matches []string
	for _, item := range items {
		attrs, err := store.itemAttributes(callCtx, item)
		if err != nil {
			return nil, secretServiceFailure(ctx, callCtx)
		}
		if _, hasTarget := attrs["target"]; !hasTarget {
			matches = append(matches, item)
		}
	}
	if len(matches) > 1 {
		return nil, unsupportedAuth(reasonAmbiguousSlot)
	}
	if len(matches) == 1 {
		isLocked, err := store.itemLocked(callCtx, matches[0])
		if err != nil {
			return nil, secretServiceFailure(ctx, callCtx)
		}
		if isLocked {
			return nil, unsupportedAuth(keyringUnavailable)
		}
		return readCodexSecretItem(ctx, callCtx, store, matches[0])
	}
	colLocked, err := store.collectionLocked(callCtx, collection)
	if err != nil {
		return nil, secretServiceFailure(ctx, callCtx)
	}
	if colLocked {
		return nil, unsupportedAuth(keyringUnavailable)
	}
	return nil, missingCredentials()
}

func readCodexSecretItem(ctx, callCtx context.Context, store codexSecretService, item string) ([]byte, failure) {
	secret, err := store.itemSecret(callCtx, item)
	if err != nil {
		return nil, secretServiceFailure(ctx, callCtx)
	}
	if len(secret) > maxPayloadSize {
		return nil, unsupportedAuth(keyringUnavailable)
	}
	if len(secret) == 0 {
		return nil, invalidCredentials()
	}
	return secret, failure{}
}

func secretServiceFailure(ctx, callCtx context.Context) failure {
	if ctx.Err() != nil {
		return contextFailure(ctx)
	}
	if callCtx.Err() != nil {
		return unsupportedAuth("credential_store_timeout")
	}
	return unsupportedAuth(keyringUnavailable)
}
