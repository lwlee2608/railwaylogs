package api

import "time"

const backoffMultiplier = 1.5

// RetryConfig mirrors Railway CLI's LOGS_RETRY_CONFIG.
type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

// Backoff tracks retry state for a reconnect loop.
type Backoff struct {
	cfg     RetryConfig
	attempt int
	delay   time.Duration
}

func NewBackoff(cfg RetryConfig) *Backoff {
	return &Backoff{cfg: cfg, delay: cfg.InitialDelay}
}

// Next returns the next delay and whether retries are exhausted.
func (b *Backoff) Next() (time.Duration, bool) {
	b.attempt++
	if b.attempt > b.cfg.MaxAttempts {
		return 0, false
	}
	d := b.delay
	next := time.Duration(float64(b.delay) * backoffMultiplier)
	if next > b.cfg.MaxDelay {
		next = b.cfg.MaxDelay
	}
	b.delay = next
	return d, true
}

// Reset clears retry state (call after a successful read).
func (b *Backoff) Reset() {
	b.attempt = 0
	b.delay = b.cfg.InitialDelay
}
