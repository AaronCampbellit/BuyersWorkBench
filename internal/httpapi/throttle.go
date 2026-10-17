package httpapi

import (
	"net"
	"sync"
	"time"
)

type attemptWindow struct {
	count int
	until time.Time
}
type loginThrottle struct {
	mu      sync.Mutex
	entries map[string]attemptWindow
}

// A bounded in-process throttle is suitable for the single local API instance.
// It must become shared storage before deploying multiple API replicas.
func (t *loginThrottle) allow(ip, subject string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.entries == nil {
		t.entries = make(map[string]attemptWindow)
	}
	for key, entry := range t.entries {
		if !now.Before(entry.until) {
			delete(t.entries, key)
		}
	}
	keys := []string{"ip:" + ip, "subject:" + string(tokenHash(subject))}
	for _, key := range keys {
		if t.entries[key].count >= 10 {
			return false
		}
	}
	for _, key := range keys {
		entry, ok := t.entries[key]
		if !ok {
			if len(t.entries) >= 4096 {
				return false
			}
			entry.until = now.Add(time.Minute)
		}
		entry.count++
		t.entries[key] = entry
	}
	return true
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
