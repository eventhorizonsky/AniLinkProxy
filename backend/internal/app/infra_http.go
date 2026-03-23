package app

import (
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func newMemoryCache() *MemoryCache {
	return &MemoryCache{data: map[string]cacheValue{}}
}

func (m *MemoryCache) Get(key string) ([]byte, bool) {
	m.mu.RLock()
	v, ok := m.data[key]
	m.mu.RUnlock()
	if !ok || time.Now().After(v.ExpireAt) {
		return nil, false
	}
	return v.Value, true
}

func (m *MemoryCache) Set(key string, val []byte, ttl time.Duration) {
	m.mu.Lock()
	m.data[key] = cacheValue{Value: val, ExpireAt: time.Now().Add(ttl)}
	m.mu.Unlock()
}

func (m *MemoryCache) gcLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		m.mu.Lock()
		for k, v := range m.data {
			if now.After(v.ExpireAt) {
				delete(m.data, k)
			}
		}
		m.mu.Unlock()
	}
}

func newRateLimiter() *RateLimiter {
	return &RateLimiter{buckets: map[string]*bucket{}}
}

func (rl *RateLimiter) Allow(key string, limit EndpointLimit) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		// 首次请求按 burst 初始化，并立即消费 1 个令牌。
		rl.buckets[key] = &bucket{Tokens: limit.Burst - 1, LastRefill: now}
		return limit.Burst >= 1
	}
	// 令牌按经过时间 * RPS 回填，上限不超过 burst。
	elapsed := now.Sub(b.LastRefill).Seconds()
	b.Tokens = math.Min(limit.Burst, b.Tokens+elapsed*limit.RPS)
	b.LastRefill = now
	if b.Tokens >= 1 {
		b.Tokens -= 1
		return true
	}
	return false
}

func (s *APIServer) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-AppId, X-Timestamp, X-Signature")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *APIServer) frontendHandler() http.Handler {
	dist := getenv("FRONTEND_DIST", filepath.Clean(filepath.Join(".", "..", "frontend", "dist")))
	index := filepath.Join(dist, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/admin/api/") {
			writeJSON(w, http.StatusNotFound, "NOT_FOUND", "route not found", nil)
			return
		}
		target := filepath.Join(dist, filepath.Clean(r.URL.Path))
		if _, err := os.Stat(target); err == nil && !strings.HasSuffix(r.URL.Path, "/") {
			http.ServeFile(w, r, target)
			return
		}
		if _, err := os.Stat(index); err == nil {
			http.ServeFile(w, r, index)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("frontend not built yet"))
	})
}
