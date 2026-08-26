package auth

import (
	"crypto/sha256"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type LoginLimiter struct {
	mu      sync.Mutex
	limit   rate.Limit
	burst   int
	maximum int
	entries map[string]limiterEntry
}

func NewLoginLimiter(attempts int, window time.Duration, maximumEntries int) *LoginLimiter {
	return &LoginLimiter{
		limit:   rate.Every(window / time.Duration(attempts)),
		burst:   attempts,
		maximum: maximumEntries,
		entries: make(map[string]limiterEntry),
	}
}

func (l *LoginLimiter) Allow(key string, now time.Time) bool {
	if l == nil || l.maximum <= 0 || l.burst <= 0 {
		return false
	}
	hash := sha256.Sum256([]byte(key))
	boundedKey := string(hash[:])
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[boundedKey]
	if !ok {
		if len(l.entries) >= l.maximum {
			l.removeOldest()
		}
		entry = limiterEntry{limiter: rate.NewLimiter(l.limit, l.burst)}
	}
	entry.lastSeen = now
	allowed := entry.limiter.AllowN(now, 1)
	l.entries[boundedKey] = entry
	return allowed
}

func (l *LoginLimiter) removeOldest() {
	var oldestKey string
	var oldestTime time.Time
	for key, entry := range l.entries {
		if oldestKey == "" || entry.lastSeen.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.lastSeen
		}
	}
	delete(l.entries, oldestKey)
}
