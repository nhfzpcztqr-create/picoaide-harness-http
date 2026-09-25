package channels

import (
	"context"
	"encoding/json"
	"testing"
)

func TestQwenRegistry(t *testing.T) {
	ch, ok := Get("qwen")
	if !ok {
		t.Fatal("qwen channel not registered")
	}
	if ch.Name() != "qwen" {
		t.Fatalf("name = %s", ch.Name())
	}
}

func TestQwenFetchModels(t *testing.T) {
	fetchFn := func(url string) ([]byte, error) {
		if url != "http://localhost:8000/v1/models" {
			t.Fatalf("url = %s", url)
		}
		return []byte(`{"object":"list","data":[{"id":"qwen3.8-27b-instruct"},{"id":"qwen3.8-max"}]}`), nil
	}
	ch, ok := Get("qwen")
	if !ok {
		t.Fatal("qwen channel not found")
	}
	ms, err := ch.FetchModels(context.Background(), "k", fetchFn)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].ID != "qwen3.8-27b-instruct" || ms[1].ID != "qwen3.8-max" {
		t.Fatalf("models = %+v", ms)
	}
}

func TestQwenRequestOverrides(t *testing.T) {
	ch, ok := Get("qwen")
	if !ok {
		t.Fatal("qwen channel not found")
	}
	ov, rm := ch.RequestOverrides("qwen3.8-27b-instruct")
	if ov != nil {
		t.Fatalf("overrides should be nil, got %v", ov)
	}
	if rm != nil {
		t.Fatalf("removeKeys should be nil, got %v", rm)
	}
}

func TestQwenCaps(t *testing.T) {
	ch, ok := Get("qwen")
	if !ok {
		t.Fatal("qwen channel not found")
	}
	cl, mo := ch.DefaultModelCaps()
	if cl != 131072 || mo != 8192 {
		t.Fatalf("caps = %d/%d", cl, mo)
	}
}

// TestQwenTransformRequestBody 验证思考参数映射。
func TestQwenTransformRequestBody(t *testing.T) {
	ch, ok := Get("qwen")
	if !ok {
		t.Fatal("qwen channel not found")
	}

	cases := []struct {
		name     string
		input    map[string]any
		wantEffort any  // nil 表示字段不应存在
		wantThinking bool // thinking 字段是否应保留(true=保留,false=删除)
		wantBudget bool  // thinking_budget 是否应保留
		changed  bool
	}{
		{
			name: "off → none,删除 thinking",
			input: map[string]any{
				"model": "qwen3.8-27b",
				"thinking": map[string]any{"type": "disabled"},
				"reasoning_effort": "off",
			},
			wantEffort:   "none",
			wantThinking: false,
			wantBudget:   false,
			changed:      true,
		},
		{
			name: "low → low,删除 thinking",
			input: map[string]any{
				"model": "qwen3.8-27b",
				"thinking": map[string]any{"type": "enabled"},
				"reasoning_effort": "low",
			},
			wantEffort:   "low",
			wantThinking: false,
			wantBudget:   false,
			changed:      true,
		},
		{
			name: "high → medium,删除 thinking",
			input: map[string]any{
				"model": "qwen3.8-27b",
				"thinking": map[string]any{"type": "enabled"},
				"reasoning_effort": "high",
			},
			wantEffort:   "medium",
			wantThinking: false,
			wantBudget:   false,
			changed:      true,
		},
		{
			name: "max → xhigh,删除 thinking",
			input: map[string]any{
				"model": "qwen3.8-27b",
				"thinking": map[string]any{"type": "enabled"},
				"reasoning_effort": "max",
			},
			wantEffort:   "xhigh",
			wantThinking: false,
			wantBudget:   false,
			changed:      true,
		},
		{
			name: "只开 thinking 开关不设档位 → 删除 thinking,不设 effort",
			input: map[string]any{
				"model": "qwen3.8-27b",
				"thinking": map[string]any{"type": "enabled"},
			},
			wantEffort:   nil,
			wantThinking: false,
			wantBudget:   false,
			changed:      true,
		},
		{
			name: "thinking.type=disabled → none,删除 thinking",
			input: map[string]any{
				"model": "qwen3.8-27b",
				"thinking": map[string]any{"type": "disabled"},
			},
			wantEffort:   "none",
			wantThinking: false,
			wantBudget:   false,
			changed:      true,
		},
		{
			name: "无思考参数 → 不变",
			input: map[string]any{
				"model": "qwen3.8-27b",
				"messages": []any{},
			},
			wantEffort:   nil,
			wantThinking: false,
			wantBudget:   false,
			changed:      false,
		},
		{
			name: "删除 thinking_budget(与 effort 互斥)",
			input: map[string]any{
				"model": "qwen3.8-27b",
				"thinking_budget": 8192,
				"reasoning_effort": "high",
			},
			wantEffort:   "medium",
			wantThinking: false,
			wantBudget:   false,
			changed:      true,
		},
		{
			name: "仅 thinking_budget 也删除(避免与默认 effort 冲突)",
			input: map[string]any{
				"model": "qwen3.8-27b",
				"thinking_budget": 4096,
			},
			wantEffort:   nil,
			wantThinking: false,
			wantBudget:   false,
			changed:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 深拷贝 input,避免用例间互相影响
			raw, _ := json.Marshal(tc.input)
			var body map[string]any
			json.Unmarshal(raw, &body)

			got := ch.TransformRequestBody(body)
			if got != tc.changed {
				t.Fatalf("changed = %v, want %v", got, tc.changed)
			}

			// 验证 reasoning_effort
			if tc.wantEffort == nil {
				if _, ok := body["reasoning_effort"]; ok {
					t.Fatalf("reasoning_effort should not exist, got %v", body["reasoning_effort"])
				}
			} else {
				if body["reasoning_effort"] != tc.wantEffort {
					t.Fatalf("reasoning_effort = %v, want %v", body["reasoning_effort"], tc.wantEffort)
				}
			}

			// 验证 thinking 字段
			_, hasThinking := body["thinking"]
			if hasThinking != tc.wantThinking {
				t.Fatalf("thinking exists = %v, want %v", hasThinking, tc.wantThinking)
			}

			// 验证 thinking_budget 字段
			_, hasBudget := body["thinking_budget"]
			if hasBudget != tc.wantBudget {
				t.Fatalf("thinking_budget exists = %v, want %v", hasBudget, tc.wantBudget)
			}

			// 验证其他字段保留
			if body["model"] != "qwen3.8-27b" {
				t.Fatalf("model field was modified: %v", body["model"])
			}
		})
	}
}
