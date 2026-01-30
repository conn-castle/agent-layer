package config

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/envfile"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// LoadProjectConfigFS reads and validates the full Agent Layer config from an fs.FS rooted at repo root.
// fsys is the filesystem to read from; root is used for error messages and built-in env values.
func LoadProjectConfigFS(fsys fs.FS, root string) (*ProjectConfig, error) {
	if fsys == nil {
		return nil, fmt.Errorf(messages.ConfigFSRequired)
	}
	if root == "" {
		return nil, fmt.Errorf(messages.ConfigRootRequired)
	}
	paths := DefaultPaths(root)

	cfg, err := LoadConfigFS(fsys, root, paths.ConfigPath)
	if err != nil {
		return nil, err
	}

	env, err := LoadEnvFS(fsys, root, paths.EnvPath)
	if err != nil {
		return nil, err
	}
	env = WithBuiltInEnv(env, root)

	instructions, err := LoadInstructionsFS(fsys, root, paths.InstructionsDir)
	if err != nil {
		return nil, err
	}

	slashCommands, err := LoadSlashCommandsFS(fsys, root, paths.SlashCommandsDir)
	if err != nil {
		return nil, err
	}

	commandsAllow, err := LoadCommandsAllowFS(fsys, root, paths.CommandsAllow)
	if err != nil {
		return nil, err
	}

	return &ProjectConfig{
		Config:        *cfg,
		Env:           env,
		Instructions:  instructions,
		SlashCommands: slashCommands,
		CommandsAllow: commandsAllow,
		Root:          root,
	}, nil
}

// LoadConfigFS reads .agent-layer/config.toml from fsys and validates it.
// root is used for path resolution when path is absolute; path is used for error messages.
func LoadConfigFS(fsys fs.FS, root string, path string) (*Config, error) {
	data, err := readFileFS(fsys, root, path)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingFileFmt, path, err)
	}
	return ParseConfig(data, path)
}

// LoadEnvFS reads .agent-layer/.env from fsys into a key-value map.
// root is used for path resolution when path is absolute; path is used for error messages.
func LoadEnvFS(fsys fs.FS, root string, path string) (map[string]string, error) {
	data, err := readFileFS(fsys, root, path)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingEnvFileFmt, path, err)
	}

	env, err := envfile.Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigInvalidEnvFileFmt, path, err)
	}
	return filterAgentLayerEnv(env), nil
}

// LoadInstructionsFS reads .agent-layer/instructions/*.md from fsys in lexicographic order.
// root is used for path resolution when dir is absolute; dir is used for error messages.
func LoadInstructionsFS(fsys fs.FS, root string, dir string) ([]InstructionFile, error) {
	entries, err := readDirFS(fsys, root, dir)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingInstructionsDirFmt, dir, err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".md") {
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return nil, fmt.Errorf(messages.ConfigNoInstructionFilesFmt, dir)
	}

	sort.Strings(names)

	files := make([]InstructionFile, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := readFileFS(fsys, root, path)
		if err != nil {
			return nil, fmt.Errorf(messages.ConfigFailedReadInstructionFmt, path, err)
		}
		data = bytes.TrimPrefix(data, utf8BOM)
		files = append(files, InstructionFile{
			Name:    name,
			Content: string(data),
		})
	}

	return files, nil
}

// LoadSlashCommandsFS reads .agent-layer/slash-commands/*.md from fsys in lexicographic order.
// root is used for path resolution when dir is absolute; dir is used for error messages.
func LoadSlashCommandsFS(fsys fs.FS, root string, dir string) ([]SlashCommand, error) {
	entries, err := readDirFS(fsys, root, dir)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingSlashCommandsDirFmt, dir, err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".md") {
			names = append(names, name)
		}
	}

	sort.Strings(names)

	commands := make([]SlashCommand, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := readFileFS(fsys, root, path)
		if err != nil {
			return nil, fmt.Errorf(messages.ConfigFailedReadSlashCommandFmt, path, err)
		}
		data = bytes.TrimPrefix(data, utf8BOM)
		description, body, err := parseSlashCommand(string(data))
		if err != nil {
			return nil, fmt.Errorf(messages.ConfigInvalidSlashCommandFmt, path, err)
		}
		commands = append(commands, SlashCommand{
			Name:        strings.TrimSuffix(name, ".md"),
			Description: description,
			Body:        body,
			SourcePath:  path,
		})
	}

	return commands, nil
}

// LoadCommandsAllowFS reads .agent-layer/commands.allow from fsys into a slice of prefixes.
// root is used for path resolution when path is absolute; path is used for error messages.
func LoadCommandsAllowFS(fsys fs.FS, root string, path string) ([]string, error) {
	data, err := readFileFS(fsys, root, path)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingCommandsAllowlistFmt, path, err)
	}

	var commands []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		commands = append(commands, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf(messages.ConfigFailedReadCommandsAllowlistFmt, path, err)
	}

	return commands, nil
}

// readFileFS reads a file from fsys using a path relative to root.
// root is used for path resolution when path is absolute.
func readFileFS(fsys fs.FS, root string, path string) ([]byte, error) {
	fsPath, err := fsPathFromRoot(root, path)
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(fsys, fsPath)
}

// readDirFS reads a directory from fsys using a path relative to root.
// root is used for path resolution when dir is absolute.
func readDirFS(fsys fs.FS, root string, dir string) ([]fs.DirEntry, error) {
	fsPath, err := fsPathFromRoot(root, dir)
	if err != nil {
		return nil, err
	}
	return fs.ReadDir(fsys, fsPath)
}

// fsPathFromRoot returns an fs.FS-compatible path for a full or relative path under root.
// When targetPath is absolute, it is made relative to root; when relative, it passes through.
func fsPathFromRoot(root string, targetPath string) (string, error) {
	if filepath.IsAbs(targetPath) {
		if root == "" {
			return "", fmt.Errorf(messages.ConfigPathOutsideRootFmt, targetPath, root)
		}
		rel, err := filepath.Rel(root, targetPath)
		if err != nil {
			return "", fmt.Errorf(messages.ConfigPathOutsideRootFmt, targetPath, root)
		}
		rel = filepath.Clean(rel)
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf(messages.ConfigPathOutsideRootFmt, targetPath, root)
		}
		fsPath := filepath.ToSlash(rel)
		return pathpkg.Clean(fsPath), nil
	}
	fsPath := filepath.ToSlash(targetPath)
	return pathpkg.Clean(fsPath), nil
}
