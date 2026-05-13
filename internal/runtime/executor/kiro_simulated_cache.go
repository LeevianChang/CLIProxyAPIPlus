package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
	"github.com/tiktoken-go/tokenizer"
)

const (
	kiroSimulatedCacheDefaultTTL      = 5 * time.Minute
	kiroSimulatedCacheExtendedTTL     = time.Hour
	kiroSimulatedCacheMinUncachedAbs  = int64(100)
	kiroSimulatedCacheMinUncachedRate = 0.05 // 5% of total input tokens
)

type kiroSimulatedCacheResult struct {
	ReadTokens     int64
	CreationTokens int64
	UncachedTokens int64
	Simulated      bool
}

func addKiroCachePart(parts *[]string, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		*parts = append(*parts, value)
	}
}

type kiroCacheBreakpoint struct {
	Hash   string
	Tokens int64
	TTL    time.Duration
}

type kiroSimulatedCacheEntry struct {
	Tokens    int64
	ExpiresAt time.Time
}

var kiroSimulatedCache = struct {
	sync.Mutex
	entries map[string]kiroSimulatedCacheEntry
}{entries: make(map[string]kiroSimulatedCacheEntry)}

func simulateKiroPromptCache(authKey, model string, source sdktranslator.Format, payload []byte, totalInputTokens int64) kiroSimulatedCacheResult {
	breakpoints := computeKiroCacheBreakpoints(model, source, payload)
	if len(breakpoints) == 0 || totalInputTokens <= 0 {
		return kiroSimulatedCacheResult{UncachedTokens: totalInputTokens}
	}
	for i := range breakpoints {
		if breakpoints[i].Tokens > totalInputTokens {
			breakpoints[i].Tokens = totalInputTokens
		}
	}

	now := time.Now()
	result := kiroSimulatedCacheResult{Simulated: true}
	cachePrefix := "kiro:" + strings.TrimSpace(authKey) + ":" + strings.TrimSpace(model) + ":"

	kiroSimulatedCache.Lock()
	defer kiroSimulatedCache.Unlock()

	for key, entry := range kiroSimulatedCache.entries {
		if now.After(entry.ExpiresAt) {
			delete(kiroSimulatedCache.entries, key)
		}
	}

	hitIndex := -1
	for i := len(breakpoints) - 1; i >= 0; i-- {
		bp := breakpoints[i]
		key := cachePrefix + bp.Hash
		entry, ok := kiroSimulatedCache.entries[key]
		if !ok || now.After(entry.ExpiresAt) {
			delete(kiroSimulatedCache.entries, key)
			continue
		}
		result.ReadTokens = entry.Tokens
		kiroSimulatedCache.entries[key] = kiroSimulatedCacheEntry{Tokens: entry.Tokens, ExpiresAt: now.Add(bp.TTL)}
		hitIndex = i
		break
	}

	prevTokens := result.ReadTokens
	start := 0
	if hitIndex >= 0 {
		start = hitIndex + 1
	}
	for _, bp := range breakpoints[start:] {
		if bp.Tokens <= prevTokens {
			continue
		}
		key := cachePrefix + bp.Hash
		kiroSimulatedCache.entries[key] = kiroSimulatedCacheEntry{Tokens: bp.Tokens, ExpiresAt: now.Add(bp.TTL)}
		result.CreationTokens += bp.Tokens - prevTokens
		prevTokens = bp.Tokens
	}

	// Dynamic minimum uncached: 5% of total input, at least 100 tokens
	// This better reflects real caching behavior where the last user turn
	// and new tool results are never cached
	minUncached := int64(float64(totalInputTokens) * kiroSimulatedCacheMinUncachedRate)
	if minUncached < kiroSimulatedCacheMinUncachedAbs {
		minUncached = kiroSimulatedCacheMinUncachedAbs
	}
	if totalInputTokens > minUncached {
		maxCachedTokens := totalInputTokens - minUncached
		cachedTokens := result.ReadTokens + result.CreationTokens
		if cachedTokens > maxCachedTokens {
			excess := cachedTokens - maxCachedTokens
			if result.CreationTokens >= excess {
				result.CreationTokens -= excess
			} else {
				excess -= result.CreationTokens
				result.CreationTokens = 0
				if result.ReadTokens >= excess {
					result.ReadTokens -= excess
				} else {
					result.ReadTokens = 0
				}
			}
		}
	}

	cachedTokens := result.ReadTokens + result.CreationTokens
	result.UncachedTokens = totalInputTokens - cachedTokens
	if result.UncachedTokens < 0 {
		result.UncachedTokens = 0
	}
	return result
}

func computeKiroCacheBreakpoints(model string, source sdktranslator.Format, payload []byte) []kiroCacheBreakpoint {
	enc, err := getTokenizer(model)
	if err != nil {
		return nil
	}
	if source.String() == "openai" {
		return computeOpenAIKiroCacheBreakpoints(enc, payload)
	}
	return computeClaudeKiroCacheBreakpoints(enc, payload)
}

func computeClaudeKiroCacheBreakpoints(enc tokenizer.Codec, payload []byte) []kiroCacheBreakpoint {
	root := gjson.ParseBytes(payload)
	var parts []string
	var breakpoints []kiroCacheBreakpoint

	tools := root.Get("tools")
	if tools.IsArray() {
		tools.ForEach(func(_, tool gjson.Result) bool {
			appendCanonicalJSONWithoutCacheControl(&parts, tool)
			if hasCacheControl(tool) {
				breakpoints = appendKiroCacheBreakpoint(enc, parts, breakpoints, cacheControlTTL(tool.Get("cache_control")))
			}
			return true
		})
	}

	system := root.Get("system")
	if system.Type == gjson.String {
		addKiroCachePart(&parts, system.String())
	} else if system.IsArray() {
		system.ForEach(func(_, block gjson.Result) bool {
			appendClaudeCachePart(&parts, block)
			if hasCacheControl(block) {
				breakpoints = appendKiroCacheBreakpoint(enc, parts, breakpoints, cacheControlTTL(block.Get("cache_control")))
			}
			return true
		})
	}
	breakpoints = appendKiroCacheBreakpoint(enc, parts, breakpoints, kiroSimulatedCacheDefaultTTL)

	messages := root.Get("messages")
	if messages.IsArray() {
		messages.ForEach(func(_, message gjson.Result) bool {
			content := message.Get("content")
			if content.Type == gjson.String {
				addKiroCachePart(&parts, content.String())
				return true
			}
			if content.IsArray() {
				content.ForEach(func(_, block gjson.Result) bool {
					appendClaudeCachePart(&parts, block)
					if hasCacheControl(block) {
						breakpoints = appendKiroCacheBreakpoint(enc, parts, breakpoints, cacheControlTTL(block.Get("cache_control")))
					}
					return true
				})
			}
			return true
		})
	}

	if len(breakpoints) == 0 {
		return defaultKiroCacheBreakpoints(enc, partsFromClaudeSystemAndTools(root))
	}
	return breakpoints
}

func computeOpenAIKiroCacheBreakpoints(enc tokenizer.Codec, payload []byte) []kiroCacheBreakpoint {
	root := gjson.ParseBytes(payload)
	var parts []string
	var breakpoints []kiroCacheBreakpoint

	tools := root.Get("tools")
	if tools.IsArray() {
		tools.ForEach(func(_, tool gjson.Result) bool {
			appendCanonicalJSONWithoutCacheControl(&parts, tool)
			if hasCacheControl(tool) || hasCacheControl(tool.Get("function")) {
				breakpoints = appendKiroCacheBreakpoint(enc, parts, breakpoints, cacheControlTTL(firstCacheControl(tool, tool.Get("function"))))
			}
			return true
		})
	}

	messages := root.Get("messages")
	if messages.IsArray() {
		stableBreakpointAdded := false
		messages.ForEach(func(_, message gjson.Result) bool {
			if message.Get("role").String() != "system" && !stableBreakpointAdded {
				breakpoints = appendKiroCacheBreakpoint(enc, parts, breakpoints, kiroSimulatedCacheDefaultTTL)
				stableBreakpointAdded = true
			}
			content := message.Get("content")
			if content.Type == gjson.String {
				addKiroCachePart(&parts, content.String())
				return true
			}
			if content.IsArray() {
				content.ForEach(func(_, block gjson.Result) bool {
					appendClaudeCachePart(&parts, block)
					if hasCacheControl(block) {
						breakpoints = appendKiroCacheBreakpoint(enc, parts, breakpoints, cacheControlTTL(block.Get("cache_control")))
					}
					return true
				})
			}
			return true
		})
		if !stableBreakpointAdded {
			breakpoints = appendKiroCacheBreakpoint(enc, parts, breakpoints, kiroSimulatedCacheDefaultTTL)
		}
	}

	if len(breakpoints) == 0 {
		return defaultKiroCacheBreakpoints(enc, partsFromOpenAISystemAndTools(root))
	}
	return breakpoints
}

func appendClaudeCachePart(parts *[]string, value gjson.Result) {
	if value.Get("text").Exists() {
		addKiroCachePart(parts, value.Get("text").String())
		return
	}
	appendCanonicalJSONWithoutCacheControl(parts, value)
}

func defaultKiroCacheBreakpoints(enc tokenizer.Codec, parts []string) []kiroCacheBreakpoint {
	if len(parts) == 0 {
		return nil
	}
	return appendKiroCacheBreakpoint(enc, parts, nil, kiroSimulatedCacheDefaultTTL)
}

func appendKiroCacheBreakpoint(enc tokenizer.Codec, parts []string, breakpoints []kiroCacheBreakpoint, ttl time.Duration) []kiroCacheBreakpoint {
	joined := strings.TrimSpace(strings.Join(parts, "\n"))
	if joined == "" {
		return breakpoints
	}
	tokens, err := enc.Count(joined)
	if err != nil || tokens <= 0 {
		return breakpoints
	}
	hash := sha256.Sum256([]byte(joined))
	return append(breakpoints, kiroCacheBreakpoint{Hash: hex.EncodeToString(hash[:]), Tokens: int64(tokens), TTL: ttl})
}

func partsFromClaudeSystemAndTools(root gjson.Result) []string {
	var parts []string
	tools := root.Get("tools")
	if tools.IsArray() {
		tools.ForEach(func(_, tool gjson.Result) bool {
			appendCanonicalJSONWithoutCacheControl(&parts, tool)
			return true
		})
	}
	system := root.Get("system")
	if system.Type == gjson.String {
		addKiroCachePart(&parts, system.String())
	} else if system.IsArray() {
		system.ForEach(func(_, block gjson.Result) bool {
			appendClaudeCachePart(&parts, block)
			return true
		})
	}
	return parts
}

func partsFromOpenAISystemAndTools(root gjson.Result) []string {
	var parts []string
	tools := root.Get("tools")
	if tools.IsArray() {
		tools.ForEach(func(_, tool gjson.Result) bool {
			appendCanonicalJSONWithoutCacheControl(&parts, tool)
			return true
		})
	}
	messages := root.Get("messages")
	if messages.IsArray() {
		messages.ForEach(func(_, message gjson.Result) bool {
			if message.Get("role").String() == "system" {
				content := message.Get("content")
				if content.Type == gjson.String {
					addKiroCachePart(&parts, content.String())
				} else {
					appendClaudeCachePart(&parts, content)
				}
			}
			return true
		})
	}
	return parts
}

func appendCanonicalJSONWithoutCacheControl(parts *[]string, value gjson.Result) {
	if !value.Exists() {
		return
	}
	var decoded any
	if err := json.Unmarshal([]byte(value.Raw), &decoded); err != nil {
		addKiroCachePart(parts, value.String())
		return
	}
	cleaned := removeCacheControl(decoded)
	raw, err := json.Marshal(cleaned)
	if err != nil {
		return
	}
	addKiroCachePart(parts, string(raw))
}

func removeCacheControl(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			if key == "cache_control" {
				continue
			}
			out[key] = removeCacheControl(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = removeCacheControl(child)
		}
		return out
	default:
		return value
	}
}

func hasCacheControl(value gjson.Result) bool {
	return value.Get("cache_control").Exists()
}

func firstCacheControl(values ...gjson.Result) gjson.Result {
	for _, value := range values {
		if cc := value.Get("cache_control"); cc.Exists() {
			return cc
		}
	}
	return gjson.Result{}
}

func cacheControlTTL(value gjson.Result) time.Duration {
	if value.Get("ttl").String() == "1h" {
		return kiroSimulatedCacheExtendedTTL
	}
	return kiroSimulatedCacheDefaultTTL
}
