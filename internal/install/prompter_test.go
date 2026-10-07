package install

import (
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestPromptFuncs_NilDefaults(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    *PromptFuncs
	}{
		{"nil", nil},
		{"zero", &PromptFuncs{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.p
			if p.hasStatuslineSource() {
				t.Fatal("missing callback must not build a statusline preview")
			}
			if ok, err := p.statuslineSource(DiffPreview{}); ok || err != nil {
				t.Fatalf("statusline default = (%v, %v), want (false, nil)", ok, err)
			}
			if ok, err := p.deleteUnknownTmpAll(nil); ok || err != nil {
				t.Fatalf("tmp default = (%v, %v), want (false, nil)", ok, err)
			}
			if paths, err := p.selectUnknownsToKeep(nil); paths != nil || err != nil {
				t.Fatalf("keep default = (%v, %v), want (nil, nil)", paths, err)
			}
			if value, err := p.configSetDefault("key", "manifest", "reason", nil); value != "manifest" || err != nil {
				t.Fatalf("config default = (%v, %v), want (manifest, nil)", value, err)
			}
			if ok, err := p.confirmSkillsMigration(nil, nil); !ok || err != nil {
				t.Fatalf("skills default = (%v, %v), want (true, nil)", ok, err)
			}
			for _, required := range []struct {
				name string
				call func() (bool, error)
				want string
			}{
				{"overwrite", func() (bool, error) { return p.overwrite(DiffPreview{}) }, messages.InstallOverwritePromptRequired},
				{"delete all", func() (bool, error) { return p.deleteUnknownAll(nil) }, messages.InstallDeleteUnknownPromptRequired},
				{"delete", func() (bool, error) { return p.deleteUnknown("path") }, messages.InstallDeleteUnknownPromptRequired},
			} {
				if ok, err := required.call(); ok || err == nil || err.Error() != required.want {
					t.Fatalf("%s fallback = (%v, %v), want (false, %q)", required.name, ok, err, required.want)
				}
			}
		})
	}
}

func TestPromptFuncs_Validate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*PromptFuncs) *PromptFuncs
		want   string
	}{
		{"nil", func(*PromptFuncs) *PromptFuncs { return nil }, messages.InstallOverwritePromptRequired},
		{"all missing", func(*PromptFuncs) *PromptFuncs { return &PromptFuncs{} }, messages.InstallOverwritePromptRequired},
		{"unified", func(p *PromptFuncs) *PromptFuncs { p.OverwriteAllUnifiedPreviewFunc = nil; return p }, messages.InstallOverwritePromptRequired},
		{"per file", func(p *PromptFuncs) *PromptFuncs { p.OverwritePreviewFunc = nil; return p }, messages.InstallOverwritePromptRequired},
		{"delete all", func(p *PromptFuncs) *PromptFuncs { p.DeleteUnknownAllFunc = nil; return p }, messages.InstallDeleteUnknownPromptRequired},
		{"delete", func(p *PromptFuncs) *PromptFuncs { p.DeleteUnknownFunc = nil; return p }, messages.InstallDeleteUnknownPromptRequired},
		{"overwrite before delete", func(p *PromptFuncs) *PromptFuncs {
			p.OverwritePreviewFunc = nil
			p.DeleteUnknownAllFunc = nil
			p.DeleteUnknownFunc = nil
			return p
		}, messages.InstallOverwritePromptRequired},
		{"wired", func(p *PromptFuncs) *PromptFuncs { return p }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.modify(autoApprovePrompter())
			err := p.validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("validate: %v", err)
				}
			} else if err == nil || err.Error() != tc.want {
				t.Fatalf("validate = %v, want %q", err, tc.want)
			}
			if err := validatePrompter(p, false); err != nil {
				t.Fatalf("non-overwrite validation: %v", err)
			}
		})
	}
}

func TestPromptFuncs_ConfigSetDefaultPassThrough(t *testing.T) {
	field := &config.FieldDef{Key: "key"}
	p := &PromptFuncs{
		ConfigSetDefaultFunc: func(key string, manifestValue any, rationale string, gotField *config.FieldDef) (any, error) {
			if key != "key" || manifestValue != "manifest" || rationale != "reason" || gotField != field {
				t.Fatal("callback arguments changed")
			}
			return "chosen", nil
		},
	}
	if value, err := p.configSetDefault("key", "manifest", "reason", field); value != "chosen" || err != nil {
		t.Fatalf("config prompt = (%v, %v), want (chosen, nil)", value, err)
	}
}
