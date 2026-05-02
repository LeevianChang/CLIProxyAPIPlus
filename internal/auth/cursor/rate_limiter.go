package cursor

import (
	"math"
	"math/rand"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	CursorDefaultCooldownBase       = 30 * time.Second
	CursorDefaultCooldownMax        = 5 * time.Minute
	CursorDefaultCooldownMultiplier = 2.0
	CursorDefaultJitterPercent      = 0.3
	CursorDefaultMaxConcurrent      = 2
)

// CursorTokenState tracks per-auth-file state for rate limiting.
type CursorTokenState struct {
	CooldownEnd    time.Time
	FailCount      int
	LastSuccess    time.Time
	ActiveRequests int
}

// CursorRateLimiter provides per-auth-file cooldown and concurrency control
// for Cursor requests. When resource_exhausted is received, the auth file
// enters an exponential backoff cooldown period.
type CursorRateLimiter struct {
	mu                  sync.Mutex
	states              map[string]*CursorTokenState
	cooldownBase        time.Duration
	cooldownMax         time.Duration
	cooldownMultiplier  float64
	jitterPercent       float64
	maxConcurrentPerKey int
	rng                 *rand.Rand
}

// CursorRateLimiterConfig allows customizing the rate limiter behavior.
type CursorRateLimiterConfig struct {
	CooldownBase        time.Duration
	CooldownMax         time.Duration
	CooldownMultiplier  float64
	JitterPercent       float64
	MaxConcurrentPerKey int
}

// NewCursorRateLimiter creates a rate limiter with default settings.
func NewCursorRateLimiter() *CursorRateLimiter {
	return &CursorRateLimiter{
		states:              make(map[string]*CursorTokenState),
		cooldownBase:        CursorDefaultCooldownBase,
		cooldownMax:         CursorDefaultCooldownMax,
		cooldownMultiplier:  CursorDefaultCooldownMultiplier,
		jitterPercent:       CursorDefaultJitterPercent,
		maxConcurrentPerKey: CursorDefaultMaxConcurrent,
		rng:                 rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// NewCursorRateLimiterWithConfig creates a rate limiter with custom settings.
func NewCursorRateLimiterWithConfig(cfg CursorRateLimiterConfig) *CursorRateLimiter {
	rl := NewCursorRateLimiter()
	if cfg.CooldownBase > 0 {
		rl.cooldownBase = cfg.CooldownBase
	}
	if cfg.CooldownMax > 0 {
		rl.cooldownMax = cfg.CooldownMax
	}
	if cfg.CooldownMultiplier > 0 {
		rl.cooldownMultiplier = cfg.CooldownMultiplier
	}
	if cfg.JitterPercent > 0 {
		rl.jitterPercent = cfg.JitterPercent
	}
	if cfg.MaxConcurrentPerKey > 0 {
		rl.maxConcurrentPerKey = cfg.MaxConcurrentPerKey
	}
	return rl
}

func (rl *CursorRateLimiter) getOrCreate(key string) *CursorTokenState {
	state, exists := rl.states[key]
	if !exists {
		state = &CursorTokenState{}
		rl.states[key] = state
	}
	return state
}

// IsAvailable returns true if the auth key is not in cooldown and has
// concurrency capacity. Does not block.
func (rl *CursorRateLimiter) IsAvailable(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	state, exists := rl.states[key]
	if !exists {
		return true
	}

	now := time.Now()
	if now.Before(state.CooldownEnd) {
		return false
	}
	if rl.maxConcurrentPerKey > 0 && state.ActiveRequests >= rl.maxConcurrentPerKey {
		return false
	}
	return true
}

// Acquire marks the start of a request for the given auth key.
// Returns true if the request is allowed, false if the key is in cooldown
// or at max concurrency.
func (rl *CursorRateLimiter) Acquire(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	state := rl.getOrCreate(key)
	now := time.Now()

	if now.Before(state.CooldownEnd) {
		log.Debugf("cursor rate limiter: key=%s in cooldown until %s (remaining %s)",
			key, state.CooldownEnd.Format("15:04:05"), state.CooldownEnd.Sub(now).Round(time.Second))
		return false
	}
	if rl.maxConcurrentPerKey > 0 && state.ActiveRequests >= rl.maxConcurrentPerKey {
		log.Debugf("cursor rate limiter: key=%s at max concurrency (%d/%d)",
			key, state.ActiveRequests, rl.maxConcurrentPerKey)
		return false
	}

	state.ActiveRequests++
	return true
}

// Release marks the end of a request for the given auth key.
func (rl *CursorRateLimiter) Release(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	state, exists := rl.states[key]
	if !exists {
		return
	}
	if state.ActiveRequests > 0 {
		state.ActiveRequests--
	}
}

// MarkFailed records a resource_exhausted failure and puts the key into
// exponential backoff cooldown.
func (rl *CursorRateLimiter) MarkFailed(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	state := rl.getOrCreate(key)
	state.FailCount++
	cooldown := rl.calculateCooldown(state.FailCount)
	state.CooldownEnd = time.Now().Add(cooldown)
	log.Infof("cursor rate limiter: key=%s marked failed (count=%d), cooldown=%s until %s",
		key, state.FailCount, cooldown.Round(time.Second), state.CooldownEnd.Format("15:04:05"))
}

// MarkSuccess records a successful request and resets the failure counter.
func (rl *CursorRateLimiter) MarkSuccess(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	state, exists := rl.states[key]
	if !exists {
		return
	}
	state.FailCount = 0
	state.CooldownEnd = time.Time{}
	state.LastSuccess = time.Now()
}

// GetCooldownRemaining returns how long the key is still in cooldown.
// Returns 0 if not in cooldown.
func (rl *CursorRateLimiter) GetCooldownRemaining(key string) time.Duration {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	state, exists := rl.states[key]
	if !exists {
		return 0
	}
	remaining := time.Until(state.CooldownEnd)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Reset clears the state for a specific key.
func (rl *CursorRateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.states, key)
}

func (rl *CursorRateLimiter) calculateCooldown(failCount int) time.Duration {
	if failCount <= 0 {
		return 0
	}
	backoff := float64(rl.cooldownBase) * math.Pow(rl.cooldownMultiplier, float64(failCount-1))
	jitter := backoff * rl.jitterPercent * (rl.rng.Float64()*2 - 1)
	backoff += jitter

	if time.Duration(backoff) > rl.cooldownMax {
		return rl.cooldownMax
	}
	return time.Duration(backoff)
}

// Singleton

var (
	globalCursorRateLimiter     *CursorRateLimiter
	globalCursorRateLimiterOnce sync.Once
)

// GetGlobalCursorRateLimiter returns the singleton CursorRateLimiter instance.
func GetGlobalCursorRateLimiter() *CursorRateLimiter {
	globalCursorRateLimiterOnce.Do(func() {
		globalCursorRateLimiter = NewCursorRateLimiter()
		log.Info("cursor: global rate limiter initialized")
	})
	return globalCursorRateLimiter
}
