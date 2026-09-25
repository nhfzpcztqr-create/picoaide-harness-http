package channels

import (
	"context"
)

// Qwen 渠道:Qwen 系列模型 OpenAI 兼容 API(vLLM 部署)。
//
// 思考参数差异:
//   - Qwen3.8 系列用 reasoning_effort 控制思考级别,值为 none/low/medium/xhigh
//   - 客户端(DeepSeek 风格)用 thinking.type + reasoning_effort(off/low/high/max)
//   - Qwen 不认识 thinking 字段,需剔除
//   - reasoning_effort 与 thinking_budget 互斥,不同时设置
type Qwen struct{}

func init() { Register(Qwen{}) }

func (Qwen) Name() string    { return "qwen" }
func (Qwen) BaseURL() string { return "http://localhost:8000/v1" }

// FetchModels:从 vLLM /models 拉取目录,OpenAI 兼容格式。
func (q Qwen) FetchModels(ctx context.Context, apiKey string, fetchFn func(url string) ([]byte, error)) ([]ModelInfo, error) {
	f := fetchFn
	if f == nil {
		f = func(url string) ([]byte, error) { return HTTPFetch(ctx, url, apiKey) }
	}
	body, err := f(q.BaseURL() + "/models")
	if err != nil {
		return nil, err
	}
	return ParseOAIModels(body)
}

// reasoningEffortMap:DeepSeek 风格档位 → Qwen 风格档位。
//
// 映射关系:
//   - off  → none   关闭思考
//   - low  → low    效率优先
//   - high → medium 平衡模式
//   - max  → xhigh  最强推理(也是 Qwen 默认值)
var reasoningEffortMap = map[string]string{
	"off":  "none",
	"low":  "low",
	"high": "medium",
	"max":  "xhigh",
}

// RequestOverrides Qwen 渠道的静态覆盖为空;动态参数转换由 TransformRequestBody 处理。
func (Qwen) RequestOverrides(modelID string) (map[string]any, []string) {
	return nil, nil
}

// TransformRequestBody 把 DeepSeek 风格的思考参数转换成 Qwen 风格。
//
// 转换规则:
//  1. 读取 thinking.type 和 reasoning_effort,计算最终档位
//  2. 删除 thinking 字段(Qwen 不认识该结构)
//  3. 把 reasoning_effort 映射成 Qwen 的值(off→none, high→medium, max→xhigh)
//  4. 显式关闭时,reasoning_effort 设为 "none"
//  5. 删除 thinking_budget(与 reasoning_effort 互斥,同时设置会报错)
func (q Qwen) TransformRequestBody(body map[string]any) bool {
	// 1. 解析当前思考状态
	thinkingObj, _ := body["thinking"].(map[string]any)
	thinkingType, _ := thinkingObj["type"].(string)
	effort, _ := body["reasoning_effort"].(string)

	// 2. 计算最终档位
	var finalEffort string
	switch {
	case thinkingType == "disabled" || effort == "off":
		// 显式关闭 → none
		finalEffort = "none"
	case effort != "":
		// 传了 effort 档位 → 映射
		if mapped, ok := reasoningEffortMap[effort]; ok {
			finalEffort = mapped
		} else {
			// 未知档位,不做转换,原样保留让上游报错
			finalEffort = effort
		}
	default:
		// 只开了 thinking 开关但没传档位,或都没传 → 不主动设置,走模型默认(xhigh)
		finalEffort = ""
	}

	changed := false

	// 3. 删除 DeepSeek 的 thinking 字段
	if _, hasThinking := body["thinking"]; hasThinking {
		delete(body, "thinking")
		changed = true
	}

	// 4. 更新 reasoning_effort
	if finalEffort != "" {
		if body["reasoning_effort"] != finalEffort {
			body["reasoning_effort"] = finalEffort
			changed = true
		}
	}

	// 5. 安全起见:删除 thinking_budget(与 reasoning_effort 互斥)
	if _, hasBudget := body["thinking_budget"]; hasBudget {
		delete(body, "thinking_budget")
		changed = true
	}

	return changed
}

// DefaultModelCaps:Qwen3.8 系列常见规格(256K 上下文,128K 输出)。
func (q Qwen) DefaultModelCaps() (int64, int64) {
	return 262144, 131072
}
