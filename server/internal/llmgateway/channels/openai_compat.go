package channels

import (
	"context"
	"encoding/json"
	"strings"
)

// OpenAICompat covers providers that expose an OpenAI-compatible /models and
// /chat/completions or /responses surface. The configured provider URL is used
// for real requests; these example URLs only make the admin channel selector
// self-documenting until an administrator enters the internal endpoint.
type OpenAICompat struct {
	name string
	base string
	mode string
}

func init() {
	Register(OpenAICompat{name: "glm", base: "https://glm.example.com/v1", mode: "glm"})
	Register(OpenAICompat{name: "minimax", base: "https://minimax.example.com/v1", mode: "minimax"})
	Register(OpenAICompat{name: "hunyuan", base: "https://hunyuan.example.com/v1", mode: "hunyuan"})
}

func (c OpenAICompat) Name() string { return c.name }

func (c OpenAICompat) BaseURL() string { return c.base }

func (c OpenAICompat) FetchModels(ctx context.Context, apiKey string, fetchFn func(url string) ([]byte, error)) ([]ModelInfo, error) {
	if fetchFn == nil {
		fetchFn = func(url string) ([]byte, error) { return HTTPFetch(ctx, url, apiKey) }
	}
	body, err := fetchFn(strings.TrimRight(c.base, "/") + "/models")
	if err != nil {
		return nil, err
	}
	return ParseOAIModels(body)
}

func (OpenAICompat) RequestOverrides(string) (map[string]any, []string) { return nil, nil }

func (OpenAICompat) DefaultModelCaps() (int64, int64) { return 131072, 8192 }

// TransformRequest removes DeepSeek-only fields from ordinary OpenAI-compatible
// requests and maps the normalized thinking switch only when the selected
// provider has a known compatible field.
func (c OpenAICompat) TransformRequest(_, defaultParams string, body map[string]any) error {
	if extra, ok := body["extra_body"].(map[string]any); ok {
		for key, value := range extra {
			if _, exists := body[key]; !exists {
				body[key] = value
			}
		}
		delete(body, "extra_body")
	}
	enabled, configured := compatibleThinking(defaultParams, body)
	budget := compatibleThinkingBudget(defaultParams, body)
	delete(body, "thinking")
	delete(body, "enable_thinking")
	delete(body, "thinking_budget")
	delete(body, "reasoning_effort")
	if !configured {
		return nil
	}
	switch c.mode {
	case "glm":
		thinking := map[string]any{"type": "disabled"}
		if enabled {
			thinking["type"] = "enabled"
		}
		body["thinking"] = thinking
	case "minimax":
		body["reasoning_split"] = enabled
	case "hunyuan":
		body["enable_thinking"] = enabled
		if budget > 0 {
			body["thinking_budget"] = budget
		}
	}
	return nil
}

func compatibleThinking(defaultParams string, body map[string]any) (bool, bool) {
	if value, ok := body["enable_thinking"].(bool); ok {
		return value, true
	}
	if value, ok := body["thinking"].(map[string]any); ok {
		if enabled, ok := value["enabled"].(bool); ok {
			return enabled, true
		}
		switch strings.ToLower(strings.TrimSpace(stringValue(value["type"]))) {
		case "enabled", "enable", "on", "true":
			return true, true
		case "disabled", "disable", "off", "false":
			return false, true
		}
	}
	if defaultParams == "" {
		return false, false
	}
	var params struct {
		Thinking *struct {
			Enabled *bool `json:"enabled"`
		} `json:"thinking"`
	}
	if json.Unmarshal([]byte(defaultParams), &params) == nil && params.Thinking != nil && params.Thinking.Enabled != nil {
		return *params.Thinking.Enabled, true
	}
	return false, false
}

func compatibleThinkingBudget(defaultParams string, body map[string]any) int64 {
	if value, ok := numberValue(body["thinking_budget"]); ok && value > 0 {
		return value
	}
	if defaultParams == "" {
		return 0
	}
	var params struct {
		Thinking *struct {
			Budget int64 `json:"budget"`
		} `json:"thinking"`
	}
	if json.Unmarshal([]byte(defaultParams), &params) == nil && params.Thinking != nil {
		return params.Thinking.Budget
	}
	return 0
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func numberValue(value any) (int64, bool) {
	switch number := value.(type) {
	case float64:
		return int64(number), number > 0
	case int:
		return int64(number), number > 0
	case int64:
		return number, number > 0
	case json.Number:
		parsed, err := number.Int64()
		return parsed, err == nil && parsed > 0
	default:
		return 0, false
	}
}
