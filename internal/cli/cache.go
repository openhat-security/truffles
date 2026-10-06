package cli

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

type Cache struct {
	Path string
}

type CacheEntry struct {
	Key     string    `json:"key"`
	Value   string    `json:"value"`
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires"`
}

func NewCache(name string) (*Cache, error) {
	dir := filepath.Join(os.TempDir(), "truffles-cache")
	if err := os.MkdirAll(dir, 0755); err != nil {
		dir = os.TempDir()
	}
	p := filepath.Join(dir, name+".jsonl")
	return &Cache{Path: p}, nil
}

func (c *Cache) Set(key, value string, ttl time.Duration) error {
	entry := CacheEntry{Key: key, Value: value, Created: time.Now()}
	if ttl > 0 {
		entry.Expires = time.Now().Add(ttl)
	}
	f, err := os.OpenFile(c.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(entry)
	f.Write(b)
	f.Write([]byte("\n"))
	return nil
}

func (c *Cache) Get(key string) (string, bool) {
	b, err := ioutil.ReadFile(c.Path)
	if err != nil {
		return "", false
	}
	lines := splitLinesStr(string(b))
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if line == "" {
			continue
		}
		var e CacheEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.Key == key {
			if !e.Expires.IsZero() && time.Now().After(e.Expires) {
				continue
			}
			return e.Value, true
		}
	}
	return "", false
}

func splitLinesStr(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
