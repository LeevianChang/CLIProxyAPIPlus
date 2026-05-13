package claude

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	"github.com/tidwall/gjson"
)

func TestBuildClaudeResponseIncludesCacheReadInputTokens(t *testing.T) {
	out := BuildClaudeResponse("ok", nil, "kiro-test", usage.Detail{
		InputTokens:  12,
		OutputTokens: 3,
		CachedTokens: 7,
	}, "end_turn")

	if got := gjson.GetBytes(out, "usage.cache_read_input_tokens").Int(); got != 7 {
		t.Fatalf("cache_read_input_tokens = %d, want 7; output=%s", got, string(out))
	}
}

func TestBuildClaudeMessageDeltaEventIncludesCacheReadInputTokens(t *testing.T) {
	out := string(BuildClaudeMessageDeltaEvent("end_turn", usage.Detail{
		InputTokens:  12,
		OutputTokens: 3,
		CachedTokens: 7,
	}))
	data := strings.TrimPrefix(strings.TrimSpace(out), "event: message_delta\ndata: ")

	if got := gjson.Get(data, "usage.cache_read_input_tokens").Int(); got != 7 {
		t.Fatalf("cache_read_input_tokens = %d, want 7; output=%s", got, out)
	}
}
