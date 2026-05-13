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
