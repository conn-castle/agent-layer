package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/musepolicy"
	"github.com/conn-castle/agent-layer/internal/projection"
)

const (
	museApprovalHookMarker    = " # agent-layer-mcp-approvals"
	musePermissionRequestHook = "PermissionRequest"
)

// writeMuseApprovals patches only our marked command hook, preserving user hooks. Native
// command grants live in Muse's shared policy store but carry workspace scope.
func writeMuseApprovals(sys System, root string, project *config.ProjectConfig) error {
	enabled := config.IsAgentEnabled(project.Config.Agents.Muse.Enabled)
	dir := filepath.Join(root, ".muse")
	if info, err := sys.Lstat(dir); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			if !enabled {
				return nil // No local ownership proof; never follow user-owned paths.
			}
			return fmt.Errorf("muse hook directory must be a real directory: %s", dir)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	path := filepath.Join(dir, "hooks.json")
	receiptPath := filepath.Join(dir, "agent-layer-policy.json")
	_, receiptErr := sys.Lstat(receiptPath)
	if receiptErr != nil && !os.IsNotExist(receiptErr) {
		return fmt.Errorf("stat Muse policy ownership receipt: %w", receiptErr)
	}
	if !enabled && os.IsNotExist(receiptErr) {
		owned, err := museLocalOwnershipEvidence(sys, path, "al hook muse-mcp ", museApprovalHookMarker)
		if err != nil || !owned {
			return err
		}
	}
	receipt, err := readMusePolicyReceipt(sys, receiptPath)
	if err != nil {
		return err
	}
	document, err := readMuseSettings(sys, path)
	if err != nil {
		return err
	}
	hooks := map[string]any{}
	if raw, exists := document[hooksKey]; exists {
		var ok bool
		hooks, ok = raw.(map[string]any)
		if !ok {
			return fmt.Errorf("muse hooks must be an object: %s", path)
		}
	}
	entries := []any{}
	owned := false
	if raw, exists := hooks[musePermissionRequestHook]; exists {
		rows, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("muse PermissionRequest hooks must be an array: %s", path)
		}
		for _, row := range rows {
			entry, ok := row.(map[string]any)
			if !ok {
				return fmt.Errorf("muse PermissionRequest hook must be an object: %s", path)
			}
			handlers, ok := entry[hooksKey].([]any)
			if !ok {
				return fmt.Errorf("muse PermissionRequest entry must have a hooks array: %s", path)
			}
			kept := []any{}
			for _, handler := range handlers {
				object, ok := handler.(map[string]any)
				command, _ := object[chimeHandlerCommandKey].(string)
				if ok && strings.HasPrefix(command, "al hook muse-mcp ") && strings.HasSuffix(command, museApprovalHookMarker) {
					owned = true
					continue
				}
				kept = append(kept, handler)
			}
			if len(kept) > 0 {
				entry[hooksKey] = kept
				entries = append(entries, entry)
			}
		}
	}
	if !enabled && !owned && receipt == nil {
		return nil
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return err
	}
	if enabled {
		command := "al hook muse-mcp " + shellSingleQuote(canonical)
		command += museApprovalHookMarker
		entries = append(entries, map[string]any{matcherKey: "mcp__.*", hooksKey: []any{map[string]any{chimeHandlerTypeKey: chimeHandlerCommandType, chimeHandlerCommandKey: command}}})
	}
	// Retire a previous native directory only for this trusted workspace root.
	var nativeDir string
	if enabled {
		nativeDir, err = musepolicy.ConfigDir()
		if err != nil {
			return fmt.Errorf("resolve Muse approval policy directory: %w", err)
		}
	}
	target, targetErr := musePolicyRetirementDirectory(receipt, canonical)
	if targetErr != nil {
		if !enabled {
			return fmt.Errorf("invalid Agent Layer Muse policy ownership receipt: %w: %s", targetErr, receiptPath)
		}
		// Enabled sync rewrites current grants; do not retire another workspace.
	} else if target != "" && (!enabled || target != nativeDir) {
		if err := musepolicy.SyncCommands(target, canonical, nil); err != nil {
			return fmt.Errorf("retire Muse command approvals: %w", err)
		}
	}
	switch {
	case enabled:
		// Persist ownership before adding rules: a later failure remains recoverable.
		if err := writeMusePolicyReceipt(sys, receiptPath, nativeDir, canonical); err != nil {
			return err
		}
		var commands []string
		if projection.BuildApprovals(project.Config, project.CommandsAllow).AllowCommands {
			commands = project.CommandsAllow
		}
		if err := musepolicy.SyncCommands(nativeDir, canonical, commands); err != nil {
			return fmt.Errorf("sync Muse command approvals: %w", err)
		}
	case receipt != nil:
		if err := sys.Remove(receiptPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	case owned:
		// Compatibility with a previous generated hook that predates receipts.
		nativeDir, err = musepolicy.ConfigDir()
		if err != nil {
			return err
		}
		if err := musepolicy.SyncCommands(nativeDir, canonical, nil); err != nil {
			return err
		}
	}
	if len(entries) > 0 {
		hooks[musePermissionRequestHook] = entries
	} else {
		delete(hooks, musePermissionRequestHook)
	}
	if len(hooks) > 0 {
		document[hooksKey] = hooks
	} else {
		delete(document, hooksKey)
	}
	if len(document) == 0 {
		if err = sys.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := sys.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err = sys.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return sys.WriteFileAtomic(path, append(data, '\n'), 0o600)
}
