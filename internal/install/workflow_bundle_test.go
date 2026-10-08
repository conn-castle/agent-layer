package install

import (
	"path/filepath"
	"testing"
)

func seedWorkflowBundleForTest(t *testing.T, root string) {
	t.Helper()
	inst := &installer{root: root, sys: RealSystem{}}
	templates := inst.templates()
	for _, dir := range templates.managedTemplateDirs() {
		if err := templates.writeTemplateDirCached(dir); err != nil {
			t.Fatalf("seed managed workflow dir %s: %v", dir.destRoot, err)
		}
	}
	for _, dir := range templates.memoryTemplateDirs() {
		if err := templates.writeTemplateDirCached(dir); err != nil {
			t.Fatalf("seed memory workflow dir %s: %v", dir.destRoot, err)
		}
	}
	if err := templates.writeTemplateDirCached(templateDir{templateRoot: "skills-catalog/dispatch-agent", destRoot: filepath.Join(root, ".agent-layer", "skills", "dispatch-agent")}); err != nil {
		t.Fatal(err)
	}
	if err := inst.writeManagedBaselineIfConsistent(BaselineStateSourceWrittenByInit); err != nil {
		t.Fatalf("write workflow baseline: %v", err)
	}
}
