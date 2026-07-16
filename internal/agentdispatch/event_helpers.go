package agentdispatch

import "strings"

func claudeResultIsErrorV013(raw map[string]any) bool {
	if isErr, ok := raw["is_error"].(bool); ok && isErr {
		return true
	}
	subtype, _ := raw["subtype"].(string)
	return strings.HasPrefix(subtype, "error")
}

func claudeTextDeltaV013(raw map[string]any) (string, bool) {
	if delta, ok := mapValueV013(raw, "delta"); ok {
		return textFromDeltaV013(delta)
	}
	if event, ok := mapValueV013(raw, "event"); ok {
		if delta, ok := mapValueV013(event, "delta"); ok {
			return textFromDeltaV013(delta)
		}
	}
	return "", false
}

func textFromDeltaV013(delta map[string]any) (string, bool) {
	if deltaType, _ := delta[jsonTypeKey].(string); deltaType != "text_delta" {
		return "", false
	}
	text, ok := delta[jsonTextKey].(string)
	return text, ok
}

func firstStringV013(values map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value, true
		}
	}
	return "", false
}

func mapValueV013(values map[string]any, key string) (map[string]any, bool) {
	value, ok := values[key].(map[string]any)
	return value, ok
}
