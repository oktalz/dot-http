package oauth2

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

type cacheEntry struct {
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"` // unix seconds, 0 = no expiry
}

// Cache is a thread-safe token store backed by a JSON file.
type Cache struct {
	mu      sync.Mutex
	file    string
	entries map[string]cacheEntry
}

// NewCache loads an existing cache file or returns an empty cache.
// If file is empty, operates in-memory only.
func NewCache(file string) *Cache {
	c := &Cache{file: file, entries: make(map[string]cacheEntry)}
	if file == "" {
		return c
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, &c.entries)
	return c
}

// Get returns the access token for key if it exists and is not expired.
func (c *Cache) Get(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return ""
	}
	if e.ExpiresAt != 0 && time.Now().Unix() >= e.ExpiresAt {
		delete(c.entries, key)
		return ""
	}
	return e.AccessToken
}

// Set stores a token with an optional TTL (seconds). 0 = no expiry.
func (c *Cache) Set(key, token string, ttlSeconds int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var expiresAt int64
	if ttlSeconds > 0 {
		// Subtract 30s buffer so we refresh before actual expiry
		expiresAt = time.Now().Unix() + int64(ttlSeconds) - 30
	}
	c.entries[key] = cacheEntry{AccessToken: token, ExpiresAt: expiresAt}
	c.save()
}

// save writes the cache to disk. Must be called with mu held.
func (c *Cache) save() {
	if c.file == "" {
		return
	}
	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(c.file, data, 0o600)
}
