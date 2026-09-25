package llmgateway

import (
	"encoding/json"
	"testing"
)

func TestThinkingAdapterFromDefaultParams(t *testing.T) {
	cases := []struct {
		name     string
		params   string
		wantVal  string
		wantOK   bool
	}{
		{"空串", "", "", false},
		{"空对象", "{}", "", false},
		{"qwen 模式", `{"_thinking_adapter":"qwen"}`, "qwen", true},
		{"strip_open 模式", `{"_thinking_adapter":"strip_open"}`, "strip_open", true},
		{"strip_all 模式", `{"_thinking_adapter":"strip_all"}`, "strip_all", true},
		{"deepseek 模式", `{"_thinking_adapter":"deepseek"}`, "deepseek", true},
		{"与其他参数共存", `{"max_output":8192,"_thinking_adapter":"qwen"}`, "qwen", true},
		{"非法 JSON", `{bad json`, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := thinkingAdapterFromDefaultParams(tc.params)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.wantVal {
				t.Fatalf("val = %q, want %q", got, tc.wantVal)
			}
		})
	}
}

// TestApplyThinkingAdapter 验证各适配器模式的转换效果。
func TestApplyThinkingAdapter(t *testing.T) {
	api := &API{}

	type bodyCheck struct {
		effort   any  // string 值或 nil(表示字段不存在)
		thinking bool // thinking 字段是否存在
		budget   bool // thinking_budget 字段是否存在
	}

	cases := []struct {
		name    string
		adapter string
		input   map[string]any
		want    bodyCheck
	}{
		// --- deepseek(默认/空):原样透传 ---
		{
			name:    "deepseek 模式原样透传",
			adapter: "deepseek",
			input: map[string]any{
				"model": "test-model",
				"thinking": map[string]any{"type": "enabled"},
				"reasoning_effort": "max",
			},
			want: bodyCheck{effort: "max", thinking: true, budget: false},
		},
		{
			name:    "空 adapter 不转换",
			adapter: "",
			input: map[string]any{
				"model": "test-model",
				"thinking": map[string]any{"type": "enabled"},
				"reasoning_effort": "max",
			},
			want: bodyCheck{effort: "max", thinking: true, budget: false},
		},

		// --- qwen 模式 ---
		{
			name:    "qwen: off → none,删 thinking",
			adapter: "qwen",
			input: map[string]any{
				"thinking": map[string]any{"type": "disabled"},
				"reasoning_effort": "off",
			},
			want: bodyCheck{effort: "none", thinking: false, budget: false},
		},
		{
			name:    "qwen: low → low",
			adapter: "qwen",
			input: map[string]any{
				"thinking": map[string]any{"type": "enabled"},
				"reasoning_effort": "low",
			},
			want: bodyCheck{effort: "low", thinking: false, budget: false},
		},
		{
			name:    "qwen: high → medium",
			adapter: "qwen",
			input: map[string]any{
				"thinking": map[string]any{"type": "enabled"},
				"reasoning_effort": "high",
			},
			want: bodyCheck{effort: "medium", thinking: false, budget: false},
		},
		{
			name:    "qwen: max → xhigh",
			adapter: "qwen",
			input: map[string]any{
				"thinking": map[string]any{"type": "enabled"},
				"reasoning_effort": "max",
			},
			want: bodyCheck{effort: "xhigh", thinking: false, budget: false},
		},
		{
			name:    "qwen: 只开 thinking 不设档位 → 删 thinking,不设 effort",
			adapter: "qwen",
			input: map[string]any{
				"thinking": map[string]any{"type": "enabled"},
			},
			want: bodyCheck{effort: nil, thinking: false, budget: false},
		},
		{
			name:    "qwen: thinking.type=disabled → none",
			adapter: "qwen",
			input: map[string]any{
				"thinking": map[string]any{"type": "disabled"},
			},
			want: bodyCheck{effort: "none", thinking: false, budget: false},
		},
		{
			name:    "qwen: 删除 thinking_budget",
			adapter: "qwen",
			input: map[string]any{
				"reasoning_effort": "high",
				"thinking_budget":  8192,
			},
			want: bodyCheck{effort: "medium", thinking: false, budget: false},
		},

		// --- strip_open 模式 ---
		{
			name:    "strip_open: off → 保留 none,删 thinking",
			adapter: "strip_open",
			input: map[string]any{
				"thinking": map[string]any{"type": "disabled"},
				"reasoning_effort": "off",
			},
			want: bodyCheck{effort: "none", thinking: false, budget: false},
		},
		{
			name:    "strip_open: thinking.type=disabled → none",
			adapter: "strip_open",
			input: map[string]any{
				"thinking": map[string]any{"type": "disabled"},
			},
			want: bodyCheck{effort: "none", thinking: false, budget: false},
		},
		{
			name:    "strip_open: low → 删除全部思考参数(走模型默认)",
			adapter: "strip_open",
			input: map[string]any{
				"thinking": map[string]any{"type": "enabled"},
				"reasoning_effort": "low",
			},
			want: bodyCheck{effort: nil, thinking: false, budget: false},
		},
		{
			name:    "strip_open: high → 删除全部思考参数(走模型默认)",
			adapter: "strip_open",
			input: map[string]any{
				"thinking": map[string]any{"type": "enabled"},
				"reasoning_effort": "high",
			},
			want: bodyCheck{effort: nil, thinking: false, budget: false},
		},
		{
			name:    "strip_open: max → 删除全部思考参数(走模型默认)",
			adapter: "strip_open",
			input: map[string]any{
				"thinking": map[string]any{"type": "enabled"},
				"reasoning_effort": "max",
			},
			want: bodyCheck{effort: nil, thinking: false, budget: false},
		},
		{
			name:    "strip_open: 无思考参数 → 不变",
			adapter: "strip_open",
			input: map[string]any{
				"model": "test-model",
			},
			want: bodyCheck{effort: nil, thinking: false, budget: false},
		},

		// --- strip_all 模式 ---
		{
			name:    "strip_all: 全删(off 也删)",
			adapter: "strip_all",
			input: map[string]any{
				"thinking": map[string]any{"type": "disabled"},
				"reasoning_effort": "off",
			},
			want: bodyCheck{effort: nil, thinking: false, budget: false},
		},
		{
			name:    "strip_all: 全删(max 也删)",
			adapter: "strip_all",
			input: map[string]any{
				"thinking": map[string]any{"type": "enabled"},
				"reasoning_effort": "max",
				"thinking_budget":  8192,
			},
			want: bodyCheck{effort: nil, thinking: false, budget: false},
		},
		{
			name:    "strip_all: 无参数不变",
			adapter: "strip_all",
			input: map[string]any{
				"model": "test-model",
			},
			want: bodyCheck{effort: nil, thinking: false, budget: false},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.input)
			out, err := api.applyThinkingAdapter(raw, tc.adapter)
			if err != nil {
				t.Fatalf("err = %v", err)
			}

			var body map[string]any
			if err := json.Unmarshal(out, &body); err != nil {
				t.Fatalf("parse err: %v", err)
			}

			// 验证 reasoning_effort
			effort, hasEffort := body["reasoning_effort"]
			if tc.want.effort == nil {
				if hasEffort {
					t.Fatalf("reasoning_effort should not exist, got %v", effort)
				}
			} else {
				if !hasEffort {
					t.Fatalf("reasoning_effort missing, want %v", tc.want.effort)
				}
				if effort != tc.want.effort {
					t.Fatalf("reasoning_effort = %v, want %v", effort, tc.want.effort)
				}
			}

			// 验证 thinking
			_, hasThinking := body["thinking"]
			if hasThinking != tc.want.thinking {
				t.Fatalf("thinking exists = %v, want %v", hasThinking, tc.want.thinking)
			}

			// 验证 thinking_budget
			_, hasBudget := body["thinking_budget"]
			if hasBudget != tc.want.budget {
				t.Fatalf("thinking_budget exists = %v, want %v", hasBudget, tc.want.budget)
			}
		})
	}
}
