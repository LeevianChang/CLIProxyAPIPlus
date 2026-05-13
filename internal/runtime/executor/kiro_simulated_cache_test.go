package executor

import (
	"testing"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

func resetKiroSimulatedCacheForTest() {
	kiroSimulatedCache.Lock()
	defer kiroSimulatedCache.Unlock()
	kiroSimulatedCache.entries = make(map[string]kiroSimulatedCacheEntry)
}

func TestSimulateKiroPromptCacheDefaultSystemTools(t *testing.T) {
	resetKiroSimulatedCacheForTest()
	payload := []byte(`{
  "system": "You are concise.",
  "tools": [{"name":"read","description":"Read file","input_schema":{"type":"object"}}],
  "messages": [{"role":"user","content":"hello"}]
}`)

	first := simulateKiroPromptCache("auth-a", "kiro-claude-sonnet-4-6", sdktranslator.FromString("claude"), payload, 1000)
	if !first.Simulated {
		t.Fatal("expected simulated cache result")
	}
	if first.CreationTokens <= 0 || first.ReadTokens != 0 {
		t.Fatalf("first request read=%d creation=%d, want creation only", first.ReadTokens, first.CreationTokens)
	}

	second := simulateKiroPromptCache("auth-a", "kiro-claude-sonnet-4-6", sdktranslator.FromString("claude"), payload, 1000)
	if second.ReadTokens != first.CreationTokens || second.CreationTokens != 0 {
		t.Fatalf("second request read=%d creation=%d, want read=%d creation=0", second.ReadTokens, second.CreationTokens, first.CreationTokens)
	}
	if second.UncachedTokens != 1000-second.ReadTokens {
		t.Fatalf("uncached=%d, want %d", second.UncachedTokens, 1000-second.ReadTokens)
	}
}

func TestSimulateKiroPromptCacheExplicitCacheControlIgnoresMetadataInHash(t *testing.T) {
	resetKiroSimulatedCacheForTest()
	firstPayload := []byte(`{
  "system": [{"type":"text","text":"Stable system","cache_control":{"type":"ephemeral"}}],
  "messages": [{"role":"user","content":"hello"}]
}`)
	secondPayload := []byte(`{
  "system": [{"cache_control":{"type":"ephemeral","ttl":"1h"},"text":"Stable system","type":"text"}],
  "messages": [{"role":"user","content":"changed"}]
}`)

	first := simulateKiroPromptCache("auth-b", "kiro-claude-sonnet-4-6", sdktranslator.FromString("claude"), firstPayload, 1000)
	second := simulateKiroPromptCache("auth-b", "kiro-claude-sonnet-4-6", sdktranslator.FromString("claude"), secondPayload, 1000)

	if first.CreationTokens <= 0 {
		t.Fatalf("first creation=%d, want positive", first.CreationTokens)
	}
	if second.ReadTokens != first.CreationTokens {
		t.Fatalf("second read=%d, want previous creation=%d", second.ReadTokens, first.CreationTokens)
	}
}
