package llmgateway

import (
	"encoding/json"
	"testing"

	"github.com/picoaide/picoaide/internal/llmgateway/channels"
)

func TestQwenChannelRequestOverrideEmitsRawHTTPShape(t *testing.T) {
	input := []byte(`{"model":"qwen3","thinking":{"type":"enabled"},"thinking_budget":2048}`)
	output, err := (&API{}).applyChannelRequestOverrides(input, channels.Qwen{}, "qwen3", "")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(output, &body); err != nil {
		t.Fatal(err)
	}
	if body["enable_thinking"] != true || body["thinking_budget"] != float64(2048) {
		t.Fatalf("outbound body = %#v", body)
	}
	if _, ok := body["extra_body"]; ok {
		t.Fatalf("SDK-only extra_body wrapper leaked: %#v", body)
	}
	if _, ok := body["thinking"]; ok {
		t.Fatalf("DeepSeek thinking field leaked: %#v", body)
	}
}
