package server

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/alex/codegateway/internal/provider"
)

// Failover tuning constants (see docs/multi-channel-failover-routing.md §7).
const (
	backoffBase       = 1 * time.Second   // exponential backoff base
	backoffCap        = 2 * time.Minute   // exponential backoff ceiling
	configErrCooldown = 30 * time.Minute  // long cooldown for auth/quota errors
	maxWaitBudget     = 30 * time.Second  // §7.2③ single all-cooled wait budget
)

// backoff returns base*2^fails, capped, with overflow protection.
func backoff(fails int) time.Duration {
	if fails < 0 {
		fails = 0
	}
	d := backoffBase << fails
	if d <= 0 || d > backoffCap {
		return backoffCap
	}
	return d
}

// errorClass describes how the failover loop should react to a provider error.
type errorClass struct {
	retryable bool          // whether to try the next candidate
	cooldown  time.Duration // how long to circuit-break the failing channel
}

// classifyError decides failover behavior from a provider error (§7.2 / §3.4).
// It honors an upstream Retry-After hint when present (§7.2①).
func classifyError(err error, fails int) errorClass {
	if err == nil {
		return errorClass{retryable: false, cooldown: 0}
	}
	// Client cancellation / deadline: do not switch, do not cool down.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return errorClass{retryable: false, cooldown: 0}
	}

	var pe *provider.ProviderError
	if errors.As(err, &pe) {
		switch {
		case pe.StatusCode == 429 || pe.StatusCode >= 500:
			// Rate limit / upstream failure: switch + short cooldown.
			// Upstream Retry-After hint takes priority over self-computed backoff.
			if d, ok := pe.RetryAfter(); ok {
				return errorClass{retryable: true, cooldown: d}
			}
			return errorClass{retryable: true, cooldown: backoff(fails)}
		case pe.StatusCode == 401 || pe.StatusCode == 403 || pe.StatusCode == 402:
			// Auth failure / insufficient balance: switch + long cooldown (config issue).
			return errorClass{retryable: true, cooldown: configErrCooldown}
		case pe.StatusCode == 400:
			// Bad request: the request itself is invalid; switching won't help.
			return errorClass{retryable: false, cooldown: 0}
		default:
			// Other 4xx: switch but do not cool down aggressively.
			return errorClass{retryable: true, cooldown: backoff(fails)}
		}
	}

	// Non-provider errors (network dial, provider construction, timeout wrappers):
	// treat as transient — switch with short backoff.
	return errorClass{retryable: true, cooldown: backoff(fails)}
}

// channelBreaker holds circuit-breaker state for one channel (in-memory, §7.1).
type channelBreaker struct {
	consecutiveFails int
	cooldownUntil    time.Time
}

// breakerRegistry is a process-wide, concurrency-safe store of per-channel
// breaker state, keyed by channel ID. It is a singleton so state survives
// across the stateless handler closures (but not across restarts, by design).
type breakerRegistry struct {
	mu       sync.Mutex
	breakers map[int64]*channelBreaker
}

var (
	globalBreakers     *breakerRegistry
	globalBreakersOnce sync.Once
)

// breakers returns the process-wide breaker registry singleton.
func breakers() *breakerRegistry {
	globalBreakersOnce.Do(func() {
		globalBreakers = &breakerRegistry{breakers: make(map[int64]*channelBreaker)}
	})
	return globalBreakers
}

// cooledDownUntil reports the channel's active cooldown deadline, or zero time
// if the channel is currently eligible.
func (r *breakerRegistry) cooledDownUntil(channelID int64) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.breakers[channelID]
	if !ok {
		return time.Time{}
	}
	return b.cooldownUntil
}

// isCoolingDown reports whether the channel is currently circuit-broken.
func (r *breakerRegistry) isCoolingDown(channelID int64, now time.Time) bool {
	until := r.cooledDownUntil(channelID)
	return !until.IsZero() && now.Before(until)
}

// fails returns the current consecutive-failure count for a channel.
func (r *breakerRegistry) fails(channelID int64) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.breakers[channelID]; ok {
		return b.consecutiveFails
	}
	return 0
}

// reportSuccess clears a channel's failure/cooldown state (§3.5).
func (r *breakerRegistry) reportSuccess(channelID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.breakers, channelID)
}

// reportFailure increments the failure count and sets the cooldown deadline.
func (r *breakerRegistry) reportFailure(channelID int64, cooldown time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.breakers[channelID]
	if !ok {
		b = &channelBreaker{}
		r.breakers[channelID] = b
	}
	b.consecutiveFails++
	if cooldown > 0 {
		b.cooldownUntil = time.Now().Add(cooldown)
	}
}
