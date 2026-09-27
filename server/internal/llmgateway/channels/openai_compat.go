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

// TransformRequestBody removes DeepSeek-only fields from ordinary OpenAI-compatible
// requests and maps the normalized thinking switch only when the selected
// provider has a known compatible field.
func (c OpenAICompat) TransformRequestBody(body map[string]any) bool {
	changed := false

	// 1. 展开 extra_body
	if extra, ok := body["extra_body"].(map[string]any); ok {
		for key, value := range extra {
			if _, exists := body[key]; !exists {
				body[key] = value
			}
		}
		delete(body, "extra_body")
		changed = true
	}

	// 2. 解析思考开关与档位
	enabled, configured := parseThinkingFromBody(body)
	budget := parseThinkingBudgetFromBody(body)

	// 3. 删除通用字段
	if _, hasThinking := body["thinking"]; hasThinking {
		delete(body, "thinking")
		changed = true
	}
	if _, hasEnable := body["enable_thinking"]; hasEnable {
		delete(body, "enable_thinking")
		changed = true
	}
	if _, hasBudget := body["thinking_budget"]; hasBudget {
		delete(body, "thinking_budget")
		changed = true
	}
	if _, hasEffort := body["reasoning_effort"]; hasEffort {
		delete(body, "reasoning_effort")
		changed = true
	}

	if !configured {
		return changed
	}

	// 4. 按 mode 写入厂商特定字段
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
	return true
}

// parseThinkingFromBody 从请求体中解析思考开关状态，仅基于 body 中已有的字段。
func parseThinkingFromBody(body map[string]any) (enabled bool, configured bool) {
	if value, ok := body["enable_thinking"].(bool); ok {
		return value, true
	}
	if value, ok := body["thinking"].(map[string]any); ok {
		if en, ok := value["enabled"].(bool); ok {
			return en, true
		}
		switch strings.ToLower(strings.TrimSpace(stringValue(value["type"]))) {
		case "enabled", "enable", "on", "true":
			return true, true
		case "disabled", "disable", "off", "false":
			return false, true
		}
	}
	if _, ok := body["reasoning_effort"].(string); ok {
		// 传了 reasoning_effort 说明配置了思考
		return true, true
	}
	return false, false
}

// parseThinkingBudgetFromBody 从请求体中解析思考预算。
func parseThinkingBudgetFromBody(body map[string]any) int64 {
	if value, ok := numberValue(body["thinking_budget"]); ok && value > 0 {
		return value
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
