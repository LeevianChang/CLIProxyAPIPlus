package executor

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
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

func TestSimulateKiroPromptCacheReadsStablePrefixWithChangingExplicitCacheControl(t *testing.T) {
	resetKiroSimulatedCacheForTest()
	firstPayload := []byte(`{
  "system": "Stable system",
  "tools": [{"name":"read","description":"Read file","input_schema":{"type":"object"}}],
  "messages": [{"role":"user","content":[{"type":"text","text":"first variable request","cache_control":{"type":"ephemeral"}}]}]
}`)
	secondPayload := []byte(`{
  "system": "Stable system",
  "tools": [{"name":"read","description":"Read file","input_schema":{"type":"object"}}],
  "messages": [{"role":"user","content":[{"type":"text","text":"second variable request","cache_control":{"type":"ephemeral"}}]}]
}`)

	first := simulateKiroPromptCache("auth-c", "kiro-claude-sonnet-4-6", sdktranslator.FromString("claude"), firstPayload, 1000)
	second := simulateKiroPromptCache("auth-c", "kiro-claude-sonnet-4-6", sdktranslator.FromString("claude"), secondPayload, 1000)

	if first.CreationTokens <= 0 {
		t.Fatalf("first creation=%d, want positive", first.CreationTokens)
	}
	if second.ReadTokens <= 0 {
		t.Fatalf("second read=%d, want stable prefix cache read", second.ReadTokens)
	}
}

func TestApplyKiroSimulatedCacheUsesUncachedInputTokens(t *testing.T) {
	detail := usage.Detail{
		InputTokens:  100,
		OutputTokens: 3,
	}

	applyKiroSimulatedCache(&detail, kiroSimulatedCacheResult{
		ReadTokens:     70,
		CreationTokens: 30,
		UncachedTokens: 0,
		Simulated:      true,
	})

	if detail.InputTokens != 0 {
		t.Fatalf("input tokens = %d, want explicitly supplied uncached input 0", detail.InputTokens)
	}
	if detail.CachedTokens != 70 {
		t.Fatalf("cached tokens = %d, want 70", detail.CachedTokens)
	}
	if detail.CacheWriteTokens != 30 {
		t.Fatalf("cache write tokens = %d, want 30", detail.CacheWriteTokens)
	}
}

func TestSimulateKiroPromptCacheKeepsMinimumUncachedTokens(t *testing.T) {
	resetKiroSimulatedCacheForTest()
	payload := []byte(`{
  "system": "Stable system prompt that is long enough to be cached",
  "tools": [{"name":"read","description":"Read file","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}],
  "messages": [{"role":"user","content":"hello"}]
}`)

	totalInput := int64(5000)
	result := simulateKiroPromptCache("auth-min", "kiro-claude-sonnet-4-6", sdktranslator.FromString("claude"), payload, totalInput)

	// Dynamic minimum: 5% of 5000 = 250, which is > 100 (absolute minimum)
	expectedMinUncached := int64(float64(totalInput) * kiroSimulatedCacheMinUncachedRate)
	if expectedMinUncached < kiroSimulatedCacheMinUncachedAbs {
		expectedMinUncached = kiroSimulatedCacheMinUncachedAbs
	}
	if result.UncachedTokens < expectedMinUncached {
		t.Fatalf("uncached tokens = %d, want at least %d", result.UncachedTokens, expectedMinUncached)
	}
	if result.ReadTokens+result.CreationTokens+result.UncachedTokens != totalInput {
		t.Fatalf("token sum read+creation+uncached = %d, want %d", result.ReadTokens+result.CreationTokens+result.UncachedTokens, totalInput)
	}
}

func TestKiroCreditCalibratedInputTokens(t *testing.T) {
	got := kiroCreditCalibratedInputTokens(0.03484375907131012, 5)
	want := int64(672)
	if got != want {
		t.Fatalf("calibrated input = %d, want %d", got, want)
	}
}

func TestCalibrateKiroSimulatedCacheToTotalInputKeepsDistribution(t *testing.T) {
	result := calibrateKiroSimulatedCacheToTotalInput(kiroSimulatedCacheResult{
		ReadTokens:     100,
		CreationTokens: 300,
		UncachedTokens: 100,
		Simulated:      true,
	}, 1000)

	if got := result.ReadTokens + result.CreationTokens + result.UncachedTokens; got != 1000 {
		t.Fatalf("token sum = %d, want 1000", got)
	}
	if result.ReadTokens != 200 || result.CreationTokens != 600 || result.UncachedTokens != 200 {
		t.Fatalf("calibrated result read=%d creation=%d uncached=%d, want 200/600/200", result.ReadTokens, result.CreationTokens, result.UncachedTokens)
	}
}
