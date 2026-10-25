package skillimport

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/gitrepo"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestServiceCancellationStopsProjectLockWait(t *testing.T) {
	for _, operation := range []string{"add", "remove", "remove catalog", "install catalog", "reset", "resolve", "pull", "push", "diff", "catalog state"} {
		t.Run(operation, func(t *testing.T) {
			proj := newProject(t)
			service := New(proj.root)
			entry := developmentEntry(t)
			var fetched atomic.Bool
			service.newRunner = func(map[string]string) (*gitrepo.Runner, error) {
				fetched.Store(true)
				return nil, errors.New("unexpected fetch after canceled lock wait")
			}
			testutil.AssertCanceledLockWaitPreservesProject(t, proj.root, func(ctx context.Context) error {
				var err error
				switch operation {
				case "add":
					_, err = service.Add(ctx, AddOptions{Repository: templates.GeneralSkillsRepository, Selectors: []string{"skills/tools/playwright"}})
				case "remove":
					_, err = service.Remove(ctx, templates.GeneralSkillsRepository, "skills/tools/playwright")
				case "remove catalog":
					_, err = service.RemoveCatalogSelectors(ctx, []string{"skills/tools/playwright"})
				case "install catalog":
					_, err = service.InstallCatalog(ctx, []string{"skills/tools/playwright"}, nil)
				case "reset":
					_, err = service.Reset(ctx, "playwright")
				case "resolve":
					_, err = service.Resolve(ctx, "playwright")
				case "pull":
					_, err = service.Pull(ctx)
				case "push":
					_, err = service.Push(ctx)
				case "diff":
					_, err = service.Diff(ctx, "playwright", "", "")
				case "catalog state":
					_, err = CatalogState(ctx, proj.root, entry)
				}
				return err
			})
			require.False(t, fetched.Load())
		})
	}
}
