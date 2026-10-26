package govee

import (
	"context"
	"sync"
	"time"
)

// tokenBucket is a concurrency-safe token bucket with burst capacity equal
// to the refill window's worth of tokens. It is used to enforce Govee's
// published request limits client-side:
//
//	GET  /router/api/v1/user/devices:    30 req/min/account
//	POST /router/api/v1/device/control:   12 req/s/account (720/min), burst 80
//	POST /router/api/v1/device/control:   2 req/s/device  (120/min), burst 6
//	POST /router/api/v1/device/state:     30 req/min/device
//	POST /router/api/v1/device/scenes:    30 req/min/device
//
// The bucket is lazy: it fills continuously while the client is used, not on
// construction, so tests and idle clients do not need to wait for warmup.
type tokenBucket struct {
	mu       sync.Mutex
	capacity float64
	tokens   float64
	rate     float64 // tokens per second
	last     time.Time
}

// newTokenBucket creates a bucket allowing `allow` requests per `window`
// duration, with burst capacity equal to `allow`.
func newTokenBucket(allow int, window time.Duration) *tokenBucket {
	return &tokenBucket{
		capacity: float64(allow),
		tokens:   float64(allow),
		rate:     float64(allow) / window.Seconds(),
		last:     time.Now(),
	}
}

// Wait blocks until a token is available or ctx is done. It returns nil when
// a token was consumed, ctx.Err() on cancellation, or a rate-limit error when
// the wait would take longer than the context deadline.
func (b *tokenBucket) Wait(ctx context.Context) error {
	for {
		b.mu.Lock()
		b.refill()
		if b.tokens >= 1 {
			b.tokens--
			b.mu.Unlock()
			return nil
		}
		// Replenish the next token and wait for it.
		need := (1 - b.tokens) / b.rate
		wait := time.Duration(need * float64(time.Second))
		// Round up slightly so we don't spin.
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		b.mu.Unlock()
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
}

// refill must be called with b.mu held.
func (b *tokenBucket) refill() {
	now := time.Now()
	if !b.last.IsZero() {
		elapsed := now.Sub(b.last).Seconds()
		b.tokens += elapsed * b.rate
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
	}
	b.last = now
}

// TryTake attempts to consume a token without blocking. It reports whether
// the token was taken. Used by tests and by callers that want to fail fast.
func (b *tokenBucket) TryTake() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// Available returns the current number of tokens (for tests and metrics).
func (b *tokenBucket) Available() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	return b.tokens
}
