package ffmpeg

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sync"
	"time"
)

type cacheEntry struct {
	localPath string
	refCount  int
	expiresAt time.Time
}

// FileCache implementa ports.IFileCache.
// Garante que o mesmo vídeo fonte é baixado uma única vez por TTL,
// mesmo com centenas de jobs processando clips do mesmo arquivo simultaneamente.
type FileCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	ttl     time.Duration
}

func NewFileCache(ttl time.Duration) *FileCache {
	c := &FileCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
	}
	go c.sweep(sweepInterval(ttl))
	return c
}

// sweepInterval returns how often the sweeper runs: half the TTL, capped between 1s and 30min.
func sweepInterval(ttl time.Duration) time.Duration {
	d := ttl / 2
	if d < time.Second {
		d = time.Second
	}
	if d > 30*time.Minute {
		d = 30 * time.Minute
	}
	return d
}

func (c *FileCache) sweep(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		c.sweepOnce()
	}
}

// sweepOnce removes entries where refCount=0 and TTL has expired.
// Exported for direct use in tests without relying on the ticker.
func (c *FileCache) sweepOnce() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for key, e := range c.entries {
		if e.refCount <= 0 && now.After(e.expiresAt) {
			os.Remove(e.localPath)
			delete(c.entries, key)
		}
	}
}

func urlKey(url string) string {
	h := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x", h)
}

func (c *FileCache) Get(url string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[urlKey(url)]
	if !ok || time.Now().After(e.expiresAt) {
		return "", false
	}
	return e.localPath, true
}

func (c *FileCache) Put(url, localPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[urlKey(url)] = &cacheEntry{
		localPath: localPath,
		refCount:  0,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *FileCache) Acquire(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[urlKey(url)]; ok {
		e.refCount++
	}
}

func (c *FileCache) Release(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := urlKey(url)
	e, ok := c.entries[key]
	if !ok {
		return
	}
	e.refCount--
	if e.refCount <= 0 && time.Now().After(e.expiresAt) {
		os.Remove(e.localPath)
		delete(c.entries, key)
	}
}

// Invalidate remove a entrada do cache sem tocar o arquivo no disco.
// Chamado após upload para que a mesma URL reusada como input force novo download.
func (c *FileCache) Invalidate(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, urlKey(url))
}
