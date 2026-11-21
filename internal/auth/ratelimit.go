package auth

import (
	"sync"
	"time"
)

type attemptRecord struct {
	failures  int
	windowEnd time.Time
	lockedTil time.Time
}

// LoginRateLimiter schützt den Login-Endpunkt vor Brute-Force-Angriffen pro Schlüssel (IP / Benutzername).
type LoginRateLimiter struct {
	mu          sync.Mutex
	records     map[string]*attemptRecord
	maxFailures int
	window      time.Duration
	lockout     time.Duration
}

func NewLoginRateLimiter(maxFailures int, window, lockout time.Duration) *LoginRateLimiter {
	return &LoginRateLimiter{
		records:     make(map[string]*attemptRecord),
		maxFailures: maxFailures,
		window:      window,
		lockout:     lockout,
	}
}

// Allow prüft, ob ein Login-Versuch zulässig ist, und liefert ggf. die verbleibende Sperrdauer.
func (l *LoginRateLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	rec, ok := l.records[key]
	if !ok {
		return true, 0
	}
	if !rec.lockedTil.IsZero() && now.Before(rec.lockedTil) {
		return false, time.Until(rec.lockedTil)
	}
	if now.After(rec.windowEnd) {
		delete(l.records, key)
		return true, 0
	}
	return true, 0
}

func (l *LoginRateLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	rec, ok := l.records[key]
	if !ok || now.After(rec.windowEnd) {
		rec = &attemptRecord{
			failures:  0,
			windowEnd: now.Add(l.window),
		}
		l.records[key] = rec
	}
	rec.failures++
	if rec.failures >= l.maxFailures {
		rec.lockedTil = now.Add(l.lockout)
	}
}

func (l *LoginRateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.records, key)
}
