package web

import (
	"sync"
	"time"
)

type SoftLock struct {
	ArticleID int64     `json:"article_id"`
	Editor    string    `json:"editor"`
	ExpiresAt time.Time `json:"expires_at"`
}

// LockManager verwaltet rein informative Bearbeitungs-Hinweise im Speicher (TTL-basiert).
type LockManager struct {
	mu    sync.Mutex
	locks map[int64]SoftLock
	ttl   time.Duration
}

func NewLockManager(ttl time.Duration) *LockManager {
	return &LockManager{
		locks: make(map[int64]SoftLock),
		ttl:   ttl,
	}
}

func (m *LockManager) Heartbeat(articleID int64, editor string) SoftLock {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	if existing, ok := m.locks[articleID]; ok && existing.ExpiresAt.After(now) && existing.Editor != editor {
		return existing
	}
	lock := SoftLock{
		ArticleID: articleID,
		Editor:    editor,
		ExpiresAt: now.Add(m.ttl),
	}
	m.locks[articleID] = lock
	return lock
}

func (m *LockManager) Release(articleID int64, editor string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.locks[articleID]; ok && existing.Editor == editor {
		delete(m.locks, articleID)
	}
}

func (m *LockManager) Get(articleID int64) (SoftLock, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock, ok := m.locks[articleID]
	if !ok || time.Now().UTC().After(lock.ExpiresAt) {
		delete(m.locks, articleID)
		return SoftLock{}, false
	}
	return lock, true
}
