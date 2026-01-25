package launchers

import "path/filepath"

// VSCodeLauncherPaths describes absolute paths for VS Code launcher artifacts under .agent-layer.
type VSCodeLauncherPaths struct {
	AgentLayerDir string
	Command       string
	Bat           string
	Desktop       string
	AppDir        string
	AppContents   string
	AppMacOS      string
	AppInfoPlist  string
	AppExec       string
}

// VSCodePaths returns absolute paths for VS Code launcher artifacts under the given repo root.
// Args: root is the repo root directory.
// Returns: VSCodeLauncherPaths containing all launcher paths.
func VSCodePaths(root string) VSCodeLauncherPaths {
	agentLayerDir := filepath.Join(root, ".agent-layer")
	appDir := filepath.Join(agentLayerDir, "open-vscode.app")
	appContents := filepath.Join(appDir, "Contents")
	appMacOS := filepath.Join(appContents, "MacOS")

	return VSCodeLauncherPaths{
		AgentLayerDir: agentLayerDir,
		Command:       filepath.Join(agentLayerDir, "open-vscode.command"),
		Bat:           filepath.Join(agentLayerDir, "open-vscode.bat"),
		Desktop:       filepath.Join(agentLayerDir, "open-vscode.desktop"),
		AppDir:        appDir,
		AppContents:   appContents,
		AppMacOS:      appMacOS,
		AppInfoPlist:  filepath.Join(appContents, "Info.plist"),
		AppExec:       filepath.Join(appMacOS, "open-vscode"),
	}
}

// All returns all launcher paths, including directories and files.
func (p VSCodeLauncherPaths) All() []string {
	return []string{
		p.Command,
		p.Bat,
		p.Desktop,
		p.AppDir,
		p.AppContents,
		p.AppMacOS,
		p.AppInfoPlist,
		p.AppExec,
	}
}
