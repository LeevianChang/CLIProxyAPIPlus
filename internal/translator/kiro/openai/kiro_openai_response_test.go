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
	out := ConvertKiroStreamToOpenAI(nil, "kiro-test", []byte(`{"stream_options":{"include_usage":true}}`), nil, []byte(`event: message_delta
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

func TestConvertKiroStreamToOpenAISuppressesUsageByDefault(t *testing.T) {
	var param any
	out := ConvertKiroStreamToOpenAI(nil, "kiro-test", []byte(`{}`), nil, []byte(`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":12,"output_tokens":3,"cache_read_input_tokens":7}}

`), &param)
	if len(out) == 0 {
		t.Fatal("expected finish chunk")
	}

	for _, chunk := range out {
		if gjson.GetBytes(chunk, "usage").Exists() {
			t.Fatalf("unexpected usage chunk without stream_options.include_usage: %s", string(chunk))
		}
	}
}

func TestConvertKiroNonStreamToOpenAISuppressesReasoningByDefault(t *testing.T) {
	var param any
	out := ConvertKiroNonStreamToOpenAI(nil, "kiro-test", []byte(`{}`), nil, []byte(`{
		"content":[{"type":"thinking","thinking":"private reasoning"},{"type":"text","text":"answer"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":12,"output_tokens":3}
	}`), &param)

	if gjson.GetBytes(out, "choices.0.message.reasoning_content").Exists() {
		t.Fatalf("unexpected reasoning_content by default: %s", string(out))
	}
	if got := gjson.GetBytes(out, "choices.0.message.content").String(); got != "answer" {
		t.Fatalf("content = %q, want answer; output=%s", got, string(out))
	}
}

func TestConvertKiroNonStreamToOpenAIIncludesReasoningWhenRequested(t *testing.T) {
	var param any
	out := ConvertKiroNonStreamToOpenAI(nil, "kiro-test", []byte(`{"include_reasoning":true}`), nil, []byte(`{
		"content":[{"type":"thinking","thinking":"private reasoning"},{"type":"text","text":"answer"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":12,"output_tokens":3}
	}`), &param)

	if got := gjson.GetBytes(out, "choices.0.message.reasoning_content").String(); got != "private reasoning" {
		t.Fatalf("reasoning_content = %q, want private reasoning; output=%s", got, string(out))
	}
}
