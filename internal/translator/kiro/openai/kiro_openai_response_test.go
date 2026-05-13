package openai

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	"github.com/tidwall/gjson"
)

func TestBuildOpenAIResponseIncludesCachedTokens(t *testing.T) {
	out := BuildOpenAIResponse("ok", nil, "kiro-test", usage.Detail{
		InputTokens:  12,
		OutputTokens: 3,
		CachedTokens: 7,
	}, "end_turn")

	if got := gjson.GetBytes(out, "usage.prompt_tokens_details.cached_tokens").Int(); got != 7 {
		t.Fatalf("cached_tokens = %d, want 7; output=%s", got, string(out))
	}
}

func TestBuildOpenAISSEUsageIncludesCachedTokens(t *testing.T) {
	out := BuildOpenAISSEUsage(NewOpenAIStreamState("kiro-test"), usage.Detail{
		InputTokens:  12,
		OutputTokens: 3,
		CachedTokens: 7,
	})
	data := strings.TrimPrefix(strings.TrimSpace(out), "data: ")

	if got := gjson.Get(data, "usage.prompt_tokens_details.cached_tokens").Int(); got != 7 {
		t.Fatalf("cached_tokens = %d, want 7; output=%s", got, out)
	}
}

func TestBuildOpenAIStreamUsageChunkIncludesCachedTokens(t *testing.T) {
	out := BuildOpenAIStreamUsageChunk("kiro-test", usage.Detail{
		InputTokens:  12,
		OutputTokens: 3,
		CachedTokens: 7,
	})

	if got := gjson.GetBytes(out, "usage.prompt_tokens_details.cached_tokens").Int(); got != 7 {
		t.Fatalf("cached_tokens = %d, want 7; output=%s", got, string(out))
	}
}

func TestConvertKiroStreamToOpenAIPropagatesCachedTokens(t *testing.T) {
	var param any
	out := ConvertKiroStreamToOpenAI(nil, "kiro-test", nil, nil, []byte(`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":12,"output_tokens":3,"cache_read_input_tokens":7}}

`), &param)
	if len(out) == 0 {
		t.Fatal("expected output chunks")
	}

	var usageChunk string
	for _, chunk := range out {
		if gjson.GetBytes(chunk, "usage.prompt_tokens_details.cached_tokens").Exists() {
			usageChunk = string(chunk)
			break
		}
	}
	if usageChunk == "" {
		t.Fatalf("expected cached token usage chunk, got %q", out)
	}
	if got := gjson.Get(usageChunk, "usage.prompt_tokens_details.cached_tokens").Int(); got != 7 {
		t.Fatalf("cached_tokens = %d, want 7; output=%s", got, usageChunk)
	}
}
