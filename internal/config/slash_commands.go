package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// LoadSlashCommands reads .agent-layer/slash-commands/*.md in lexicographic order.
func LoadSlashCommands(dir string) ([]SlashCommand, error) {
	entries, err := os.ReadDir(dir)
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
		data, err := osReadFileFunc(path)
		if err != nil {
			return nil, fmt.Errorf(messages.ConfigFailedReadSlashCommandFmt, path, err)
		}
		data = bytes.TrimPrefix(data, utf8BOM)
		description, autoApprove, body, err := parseSlashCommand(string(data))
		if err != nil {
			return nil, fmt.Errorf(messages.ConfigInvalidSlashCommandFmt, path, err)
		}
		commands = append(commands, SlashCommand{
			Name:        strings.TrimSuffix(name, ".md"),
			Description: description,
			AutoApprove: autoApprove,
			Body:        body,
			SourcePath:  path,
		})
	}

	return commands, nil
}

func parseSlashCommand(content string) (string, bool, string, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	if !scanner.Scan() {
		return "", false, "", fmt.Errorf(messages.ConfigSlashCommandMissingContent)
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return "", false, "", fmt.Errorf(messages.ConfigSlashCommandMissingFrontMatter)
	}

	var fmLines []string
	foundEnd := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			foundEnd = true
			break
		}
		fmLines = append(fmLines, line)
	}
	if !foundEnd {
		return "", false, "", fmt.Errorf(messages.ConfigSlashCommandUnterminatedFrontMatter)
	}

	var bodyBuilder strings.Builder
	for scanner.Scan() {
		bodyBuilder.WriteString(scanner.Text())
		bodyBuilder.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return "", false, "", fmt.Errorf(messages.ConfigSlashCommandFailedReadContentFmt, err)
	}

	body := strings.TrimPrefix(bodyBuilder.String(), "\n")
	body = strings.TrimRight(body, "\n")

	description, err := parseDescription(fmLines)
	if err != nil {
		return "", false, "", err
	}

	autoApprove, err := parseAutoApprove(fmLines)
	if err != nil {
		return "", false, "", err
	}

	return description, autoApprove, body, nil
}

func parseAutoApprove(lines []string) (bool, error) {
	for _, line := range lines {
		// Skip continuation lines (indented lines belong to the previous key's value).
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "auto-approve:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "auto-approve:"))
		if value == "" {
			return false, fmt.Errorf(messages.ConfigSlashCommandAutoApproveInvalidFmt, "")
		}
		b, err := strconv.ParseBool(value)
		if err != nil {
			return false, fmt.Errorf(messages.ConfigSlashCommandAutoApproveInvalidFmt, value)
		}
		return b, nil
	}
	return false, nil
}

func parseDescription(lines []string) (string, error) {
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "description:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		if value == "" {
			return "", fmt.Errorf(messages.ConfigSlashCommandDescriptionEmpty)
		}
		if value == ">-" || value == ">" || value == "|" || value == "|+" || value == "|-" {
			var parts []string
			for j := i + 1; j < len(lines); j++ {
				if strings.HasPrefix(lines[j], "  ") {
					parts = append(parts, strings.TrimSpace(strings.TrimPrefix(lines[j], "  ")))
					continue
				}
				break
			}
			description := strings.TrimSpace(strings.Join(parts, " "))
			if description == "" {
				return "", fmt.Errorf(messages.ConfigSlashCommandDescriptionEmpty)
			}
			return description, nil
		}
		return strings.Trim(value, "\""), nil
	}

	return "", fmt.Errorf(messages.ConfigSlashCommandMissingDescription)
}
