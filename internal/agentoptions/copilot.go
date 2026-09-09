package agentoptions

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const copilotJSONRPCVersion = "2.0"

// Copilot's headless SDK transport uses Content-Length frames, unlike the
// newline-delimited Claude and Codex protocols. No conversation is created.
func readCopilotModels(input io.Reader, output io.Writer) ([]string, error) {
	reader := bufio.NewReader(input)
	var connected struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	if err := copilotRequest(reader, output, 1, "connect", &connected); err != nil {
		return nil, err
	}
	if connected.ProtocolVersion < 3 {
		return nil, errors.New("unsupported Copilot protocol; update the Copilot CLI")
	}
	var result struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := copilotRequest(reader, output, 2, "models.list", &result); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(result.Models))
	for _, model := range result.Models {
		models = append(models, model.ID)
	}
	return models, nil
}

func copilotRequest(reader *bufio.Reader, output io.Writer, id int, method string, result any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": copilotJSONRPCVersion, "id": id, "method": method, paramsKey: map[string]any{}})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		return err
	}
	for {
		body, err := readCopilotFrame(reader)
		if err != nil {
			return fmt.Errorf("read %s response: %w", method, err)
		}
		var response struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
		if response.ID == nil || *response.ID != id {
			continue
		}
		if response.Error != nil {
			return fmt.Errorf("copilot rejected %s (%d: %s); check CLI version, authentication, and connectivity",
				method, response.Error.Code, response.Error.Message)
		}
		return json.Unmarshal(response.Result, result)
	}
}

func readCopilotFrame(reader *bufio.Reader) ([]byte, error) {
	length := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, errors.New("invalid Copilot response header")
		}
		if strings.EqualFold(key, "Content-Length") {
			if length != 0 {
				return nil, errors.New("duplicate Copilot Content-Length")
			}
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil || length <= 0 || length > maxDiscoveryBytes {
				return nil, errors.New("invalid Copilot Content-Length")
			}
		}
	}
	if length == 0 {
		return nil, errors.New("missing Copilot Content-Length")
	}
	body := make([]byte, length)
	_, err := io.ReadFull(reader, body)
	return body, err
}
