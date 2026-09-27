package channels

import (
	"context"
	"testing"
)

func TestOpenAICompatChannelsRegistered(t *testing.T) {
	for _, name := range []string{"glm", "minimax", "hunyuan"} {
		channel, ok := Get(name)
		if !ok {
			t.Fatalf("channel %q is not registered", name)
		}
		var gotURL string
		models, err := channel.FetchModels(context.Background(), "key", func(url string) ([]byte, error) {
			gotURL = url
			return []byte(`{"data":[{"id":"model-a"}]}`), nil
		})
		if err != nil || len(models) != 1 || gotURL == "" {
			t.Fatalf("channel %q model discovery = models:%v url:%q err:%v", name, models, gotURL, err)
		}
	}
}

func TestOpenAICompatThinkingTranslation(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{name: "glm", want: "thinking"},
		{name: "minimax", want: "reasoning_split"},
		{name: "hunyuan", want: "enable_thinking"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channel, _ := Get(tc.name)
			compat := channel.(OpenAICompat)
			body := map[string]any{
				"model":            "model-a",
				"thinking":         map[string]any{"type": "enabled"},
				"reasoning_effort": "high",
				"thinking_budget":  float64(2048),
			}
			if err := compat.TransformRequest("model-a", "", body); err != nil {
				t.Fatal(err)
			}
			if _, ok := body[tc.want]; !ok {
				t.Fatalf("translated body missing %q: %#v", tc.want, body)
			}
			for _, key := range []string{"thinking", "enable_thinking", "thinking_budget", "reasoning_effort"} {
				if key == tc.want {
					continue
				}
				if tc.name == "hunyuan" && key == "thinking_budget" {
					continue
				}
				if _, ok := body[key]; ok && tc.name != "glm" {
					t.Fatalf("vendor-specific field %q leaked into %s body: %#v", key, tc.name, body)
				}
			}
		})
	}
}

func TestOpenAICompatDisabledThinkingRemovesDeepSeekFields(t *testing.T) {
	channel := OpenAICompat{name: "hunyuan", base: "http://example.test/v1", mode: "hunyuan"}
	body := map[string]any{
		"thinking":         map[string]any{"type": "disabled"},
		"reasoning_effort": "max",
	}
	if err := channel.TransformRequest("model-a", "", body); err != nil {
		t.Fatal(err)
	}
	if got, ok := body["enable_thinking"].(bool); !ok || got {
		t.Fatalf("disabled thinking = %#v", body["enable_thinking"])
	}
	if _, ok := body["thinking"]; ok {
		t.Fatalf("DeepSeek thinking object leaked: %#v", body)
	}
}
