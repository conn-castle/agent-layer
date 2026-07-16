//go:build tools

package tools

import (
	_ "golang.org/x/tools/cmd/deadcode"
	_ "golang.org/x/tools/cmd/goimports"
	_ "golang.org/x/vuln/cmd/govulncheck"
	_ "gotest.tools/gotestsum"
)
