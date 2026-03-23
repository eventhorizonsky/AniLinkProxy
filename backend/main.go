package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	roleUser  = "user"
	roleAdmin = "admin"
)

type AppConfig struct {
	ListenAddr string
	Upstream   string

	UpstreamAppID     string
	UpstreamAppSecret string
	JWTSecret         string

	SQLitePath string

	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	SMTPFrom string

	TurnstileSiteKey   string
	TurnstileSecretKey string
}

type RuntimeConfig struct {
	TimestampCheckEnabled bool                     `json:"timestampCheckEnabled"`
	TimestampToleranceSec int64                    `json:"timestampToleranceSec"`
	CacheTTLMin           map[string]int           `json:"cacheTtlMin"`
	RateLimit             map[string]EndpointLimit `json:"rateLimit"`
	MatchLockTimeoutSec   int                      `json:"matchLockTimeoutSec"`
	BodySizeLimitBytes    int64                    `json:"bodySizeLimitBytes"`
	BatchMaxItems         int                      `json:"batchMaxItems"`
	AutoBanEnabled        bool                     `json:"autoBanEnabled"`
	AutoBanMinutes        int                      `json:"autoBanMinutes"`
}

type EndpointLimit struct {
	RPS   float64 `json:"rps"`
	Burst float64 `json:"burst"`
}

type User struct {
	ID         int64
	Email      string
	Password   string
	AppID      string
	AppSecret  string
	SecretSeen bool
	Role       string
	Status     string
	BanReason  sql.NullString
	BanUntil   sql.NullString
	CreatedAt  string
}

type APIServer struct {
	cfg        AppConfig
	db         *sql.DB
	httpClient *http.Client

	runtimeMu sync.RWMutex
	runtime   RuntimeConfig

	cache *MemoryCache
	rl    *RateLimiter

	matchMu   sync.Mutex
	matchLock map[string]time.Time
}

type bucket struct {
	Tokens     float64
	LastRefill time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

type cacheValue struct {
	Value    []byte
	ExpireAt time.Time
}

type MemoryCache struct {
	mu   sync.RWMutex
	data map[string]cacheValue
}

type jsonResp struct {
	Code    string      `json:"code"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type authClaims struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("配置读取失败: %v", err)
	}

	db, err := sql.Open("sqlite", cfg.SQLitePath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		log.Fatalf("数据库打开失败: %v", err)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	if err = initSchema(db); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}

	runtimeCfg, err := loadRuntimeConfig(db)
	if err != nil {
		log.Fatalf("运行时配置加载失败: %v", err)
	}

	if err = ensureInitAdmin(db); err != nil {
		log.Fatalf("初始超管创建失败: %v", err)
	}

	server := &APIServer{
		cfg: cfg,
		db:  db,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		runtime:   runtimeCfg,
		cache:     newMemoryCache(),
		rl:        newRateLimiter(),
		matchLock: map[string]time.Time{},
	}
	go server.cache.gcLoop()
	go server.cleanupMatchLoop()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(server.cors)

	r.Route("/admin/api", func(admin chi.Router) {
		admin.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, "OK", "running", map[string]string{"time": time.Now().Format(time.RFC3339)})
		})
		admin.Get("/auth/turnstile/site-key", server.handleTurnstileSiteKey)
		admin.Post("/auth/email/send-register", server.handleSendRegisterCode)
		admin.Post("/auth/register", server.handleRegister)
		admin.Post("/auth/login", server.handleLogin)

		admin.Group(func(protected chi.Router) {
			protected.Use(server.authUserMiddleware)
			protected.Get("/me", server.handleMe)
			protected.Get("/stats/me", server.handleMyStats)
			protected.Get("/risk/me", server.handleMyRisk)
			protected.Post("/secret/send-reset-code", server.handleSendResetCode)
			protected.Post("/secret/reveal", server.handleRevealSecret)
			protected.Post("/secret/reset", server.handleResetSecret)
		})

		admin.Group(func(adm chi.Router) {
			adm.Use(server.authAdminMiddleware)
			adm.Get("/admin/users", server.handleAdminUsers)
			adm.Post("/admin/users/{userID}/ban", server.handleAdminBan)
			adm.Post("/admin/users/{userID}/unban", server.handleAdminUnban)
			adm.Get("/admin/stats/global", server.handleAdminGlobalStats)
			adm.Get("/admin/config", server.handleAdminGetConfig)
			adm.Put("/admin/config", server.handleAdminUpdateConfig)
		})
	})

	// Proxy routes
	r.Get("/api/v2/comment/{episodeId}", server.proxyGET)
	r.Get("/api/v2/search/episodes", server.proxyGET)
	r.Get("/api/v2/bangumi/{animeId}", server.proxyGET)
	r.Get("/api/v2/bangumi/shin", server.proxyGET)
	r.Post("/api/v2/match", server.proxyPOST)
	r.Post("/api/v2/match/batch", server.proxyPOST)

	// Frontend static files
	r.Handle("/*", server.frontendHandler())

	log.Printf("proxy service listening on %s", cfg.ListenAddr)
	if err = http.ListenAndServe(cfg.ListenAddr, r); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

func loadConfig() (AppConfig, error) {
	port := getenv("PORT", "8080")
	smtpPort, _ := strconv.Atoi(getenv("SMTP_PORT", "587"))
	cfg := AppConfig{
		ListenAddr:         ":" + port,
		Upstream:           getenv("UPSTREAM_BASE_URL", "https://api.dandanplay.net"),
		UpstreamAppID:      os.Getenv("UPSTREAM_DANDAN_APP_ID"),
		UpstreamAppSecret:  os.Getenv("UPSTREAM_DANDAN_APP_SECRET"),
		JWTSecret:          getenv("JWT_SECRET", "change-this-in-production"),
		SQLitePath:         getenv("SQLITE_PATH", "./data/proxy.db"),
		SMTPHost:           os.Getenv("SMTP_HOST"),
		SMTPPort:           smtpPort,
		SMTPUser:           os.Getenv("SMTP_USERNAME"),
		SMTPPass:           os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:           os.Getenv("SMTP_FROM_ADDRESS"),
		TurnstileSiteKey:   os.Getenv("TURNSTILE_SITE_KEY"),
		TurnstileSecretKey: os.Getenv("TURNSTILE_SECRET_KEY"),
	}
	if cfg.UpstreamAppID == "" || cfg.UpstreamAppSecret == "" {
		return cfg, errors.New("缺少 UPSTREAM_DANDAN_APP_ID / UPSTREAM_DANDAN_APP_SECRET")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SQLitePath), 0o755); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func defaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		TimestampCheckEnabled: true,
		TimestampToleranceSec: 300,
		CacheTTLMin: map[string]int{
			"comment": 30,
			"search":  180,
			"bangumi": 360,
			"shin":    360,
		},
		RateLimit: map[string]EndpointLimit{
			"comment":     {RPS: 6, Burst: 12},
			"search":      {RPS: 2, Burst: 4},
			"bangumi":     {RPS: 2, Burst: 4},
			"shin":        {RPS: 1, Burst: 2},
			"match":       {RPS: 0.3, Burst: 1},
			"match_batch": {RPS: 0.2, Burst: 1},
		},
		MatchLockTimeoutSec: 45,
		BodySizeLimitBytes:  1024 * 1024,
		BatchMaxItems:       30,
		AutoBanEnabled:      true,
		AutoBanMinutes:      30,
	}
}

func initSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			app_id TEXT NOT NULL UNIQUE,
			app_secret TEXT NOT NULL,
			secret_shown INTEGER NOT NULL DEFAULT 0,
			role TEXT NOT NULL DEFAULT 'user',
			status TEXT NOT NULL DEFAULT 'active',
			ban_reason TEXT,
			ban_until TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_login_at TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS email_codes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL,
			purpose TEXT NOT NULL,
			code_hash TEXT NOT NULL,
			expire_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS email_send_rate (
			rate_key TEXT PRIMARY KEY,
			last_sent_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS app_metrics_daily (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			app_id TEXT NOT NULL,
			endpoint TEXT NOT NULL,
			date TEXT NOT NULL,
			total INTEGER NOT NULL DEFAULT 0,
			success INTEGER NOT NULL DEFAULT 0,
			auth_fail INTEGER NOT NULL DEFAULT 0,
			rate_limited INTEGER NOT NULL DEFAULT 0,
			upstream_fail INTEGER NOT NULL DEFAULT 0,
			timeout INTEGER NOT NULL DEFAULT 0,
			total_latency_ms INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			UNIQUE(app_id, endpoint, date)
		);`,
		`CREATE TABLE IF NOT EXISTS risk_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			app_id TEXT NOT NULL,
			user_id INTEGER NOT NULL,
			level TEXT NOT NULL,
			rule_name TEXT NOT NULL,
			metric_value REAL NOT NULL,
			detail TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS system_config (
			k TEXT PRIMARY KEY,
			v TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_email_codes_email_purpose ON email_codes(email, purpose);`,
		`CREATE INDEX IF NOT EXISTS idx_users_appid ON users(app_id);`,
		`CREATE INDEX IF NOT EXISTS idx_risk_events_user ON risk_events(user_id, created_at DESC);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func ensureInitAdmin(db *sql.DB) error {
	email := os.Getenv("INIT_ADMIN_EMAIL")
	pass := os.Getenv("INIT_ADMIN_PASSWORD")
	if email == "" || pass == "" {
		return nil
	}
	var cnt int
	if err := db.QueryRow(`SELECT COUNT(1) FROM users WHERE role='admin'`).Scan(&cnt); err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	pwHash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	appID := "adm_" + randString(24)
	secret := randString(48)
	_, err = db.Exec(`INSERT INTO users(email, password_hash, app_id, app_secret, secret_shown, role, status, created_at, updated_at)
		VALUES(?,?,?,?,1,'admin','active',?,?)`, strings.ToLower(email), string(pwHash), appID, secret, now, now)
	return err
}

func loadRuntimeConfig(db *sql.DB) (RuntimeConfig, error) {
	cfg := defaultRuntimeConfig()
	var raw string
	err := db.QueryRow(`SELECT v FROM system_config WHERE k='runtime_config'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		if err := saveRuntimeConfig(db, cfg); err != nil {
			return cfg, err
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return defaultRuntimeConfig(), nil
	}
	return cfg, nil
}

func saveRuntimeConfig(db *sql.DB, cfg RuntimeConfig) error {
	raw, _ := json.Marshal(cfg)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO system_config(k, v, updated_at) VALUES('runtime_config', ?, ?)
		ON CONFLICT(k) DO UPDATE SET v=excluded.v, updated_at=excluded.updated_at`, string(raw), now)
	return err
}

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
		rl.buckets[key] = &bucket{Tokens: limit.Burst - 1, LastRefill: now}
		return limit.Burst >= 1
	}
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

func (s *APIServer) proxyGET(w http.ResponseWriter, r *http.Request) {
	s.proxyRequest(w, r, true)
}

func (s *APIServer) proxyPOST(w http.ResponseWriter, r *http.Request) {
	s.proxyRequest(w, r, false)
}

func (s *APIServer) proxyRequest(w http.ResponseWriter, r *http.Request, canCache bool) {
	start := time.Now()
	path := r.URL.Path
	endpoint := endpointKey(path)
	if endpoint == "" {
		writeJSON(w, http.StatusNotFound, "NOT_PROXY_ENDPOINT", "unsupported endpoint", nil)
		return
	}

	user, code, msg := s.verifyClientSignature(r)
	if code != "" {
		writeJSON(w, statusForCode(code), code, msg, nil)
		s.recordMetric("", endpoint, start, code)
		return
	}

	limit := s.getRateLimit(endpoint)
	if !s.rl.Allow(user.AppID+":"+endpoint, limit) {
		writeJSON(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests", nil)
		s.recordMetric(user.AppID, endpoint, start, "RATE_LIMITED")
		s.createRiskEvent(user, "medium", "ratelimit", 1, "触发接口限流")
		return
	}

	releaseMatch := func() {}
	if endpoint == "match" || endpoint == "match_batch" {
		var ok bool
		ok, releaseMatch = s.tryAcquireMatchLock(user.AppID)
		if !ok {
			writeJSON(w, http.StatusTooManyRequests, "MATCH_IN_FLIGHT", "match request is already running", nil)
			s.recordMetric(user.AppID, endpoint, start, "RATE_LIMITED")
			return
		}
		defer releaseMatch()
	}

	var body []byte
	var err error
	if r.Method == http.MethodPost {
		body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, s.getRuntime().BodySizeLimitBytes))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, "BODY_TOO_LARGE", "request body exceeds limit", nil)
			s.recordMetric(user.AppID, endpoint, start, "VALIDATION_FAILED")
			return
		}
		if endpoint == "match" || endpoint == "match_batch" {
			if code, msg := s.validateMatchPayload(endpoint, body); code != "" {
				writeJSON(w, http.StatusBadRequest, code, msg, nil)
				s.recordMetric(user.AppID, endpoint, start, "VALIDATION_FAILED")
				s.createRiskEvent(user, "low", "payload_invalid", 1, msg)
				return
			}
		}
	}

	cacheKey := ""
	if canCache && isCacheableEndpoint(endpoint) {
		cacheKey = cacheKeyOf(r.URL.Path, r.URL.Query())
		if hit, ok := s.cache.Get(cacheKey); ok {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(hit)
			s.recordMetric(user.AppID, endpoint, start, "OK")
			return
		}
	}

	upstreamURL := s.cfg.Upstream + path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "request creation failed", nil)
		s.recordMetric(user.AppID, endpoint, start, "INTERNAL_ERROR")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	s.attachUpstreamSignature(req, path)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, "UPSTREAM_FAILED", err.Error(), nil)
		s.recordMetric(user.AppID, endpoint, start, "UPSTREAM_FAILED")
		s.createRiskEvent(user, "medium", "upstream_error", 1, err.Error())
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if cacheKey != "" && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		ttl := time.Duration(s.getTTL(endpoint)) * time.Minute
		s.cache.Set(cacheKey, respBody, ttl)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		s.recordMetric(user.AppID, endpoint, start, "OK")
	} else {
		s.recordMetric(user.AppID, endpoint, start, "UPSTREAM_FAILED")
	}
}

func (s *APIServer) verifyClientSignature(r *http.Request) (User, string, string) {
	appID := r.Header.Get("X-AppId")
	ts := r.Header.Get("X-Timestamp")
	sign := r.Header.Get("X-Signature")
	if appID == "" || ts == "" || sign == "" {
		return User{}, "AUTH_HEADER_MISSING", "missing signature headers"
	}
	user, err := s.findUserByAppID(appID)
	if err != nil {
		return User{}, "AUTH_INVALID_APP", "app id not found"
	}

	if user.Status == "banned" {
		if !user.BanUntil.Valid {
			return User{}, "BANNED", "account banned"
		}
		t, parseErr := time.Parse(time.RFC3339, user.BanUntil.String)
		if parseErr == nil && t.After(time.Now()) {
			return User{}, "BANNED", "account banned"
		}
	}

	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return User{}, "AUTH_TIMESTAMP_INVALID", "timestamp invalid"
	}
	rt := s.getRuntime()
	if rt.TimestampCheckEnabled {
		diff := time.Now().Unix() - tsInt
		if diff < 0 {
			diff = -diff
		}
		if diff > rt.TimestampToleranceSec {
			return User{}, "AUTH_TIMESTAMP_EXPIRED", "timestamp outside tolerance window"
		}
	}
	expected := generateSignature(appID, tsInt, r.URL.Path, user.AppSecret)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(sign)) != 1 {
		s.createRiskEvent(user, "high", "auth_fail", 1, "签名验签失败")
		return User{}, "AUTH_SIGNATURE_INVALID", "signature invalid"
	}
	return user, "", ""
}

func (s *APIServer) attachUpstreamSignature(req *http.Request, path string) {
	ts := time.Now().Unix()
	req.Header.Set("X-AppId", s.cfg.UpstreamAppID)
	req.Header.Set("X-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Signature", generateSignature(s.cfg.UpstreamAppID, ts, path, s.cfg.UpstreamAppSecret))
}

func generateSignature(appID string, ts int64, path string, secret string) string {
	raw := appID + strconv.FormatInt(ts, 10) + path + secret
	sum := sha256.Sum256([]byte(raw))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func endpointKey(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/v2/comment/"):
		return "comment"
	case path == "/api/v2/search/episodes":
		return "search"
	case strings.HasPrefix(path, "/api/v2/bangumi/shin"):
		return "shin"
	case strings.HasPrefix(path, "/api/v2/bangumi/"):
		return "bangumi"
	case path == "/api/v2/match":
		return "match"
	case path == "/api/v2/match/batch":
		return "match_batch"
	default:
		return ""
	}
}

func isCacheableEndpoint(endpoint string) bool {
	return endpoint == "comment" || endpoint == "search" || endpoint == "bangumi" || endpoint == "shin"
}

func cacheKeyOf(path string, q map[string][]string) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(path)
	for _, k := range keys {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			b.WriteString("|")
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(v)
		}
	}
	return b.String()
}

func (s *APIServer) getTTL(endpoint string) int {
	rt := s.getRuntime()
	if v, ok := rt.CacheTTLMin[endpoint]; ok {
		return v
	}
	return 30
}

func (s *APIServer) getRateLimit(endpoint string) EndpointLimit {
	rt := s.getRuntime()
	if v, ok := rt.RateLimit[endpoint]; ok {
		return v
	}
	return EndpointLimit{RPS: 1, Burst: 2}
}

func (s *APIServer) validateMatchPayload(endpoint string, body []byte) (string, string) {
	if len(body) == 0 {
		return "PAYLOAD_INVALID", "body is empty"
	}
	if endpoint == "match" {
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			return "PAYLOAD_INVALID", "invalid json body"
		}
		if _, ok := req["matchMode"]; !ok {
			req["matchMode"] = "hashAndFileName"
		}
		mode, _ := req["matchMode"].(string)
		if mode != "hashAndFileName" && mode != "hashOnly" && mode != "fileNameOnly" {
			return "PAYLOAD_INVALID", "unsupported matchMode"
		}
	} else {
		var req struct {
			Requests []map[string]interface{} `json:"requests"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return "PAYLOAD_INVALID", "invalid json body"
		}
		if len(req.Requests) == 0 {
			return "PAYLOAD_INVALID", "requests must not be empty"
		}
		if len(req.Requests) > s.getRuntime().BatchMaxItems {
			return "PAYLOAD_INVALID", "batch request exceeds max items"
		}
	}
	return "", ""
}

func (s *APIServer) tryAcquireMatchLock(appID string) (bool, func()) {
	s.matchMu.Lock()
	defer s.matchMu.Unlock()
	if _, exists := s.matchLock[appID]; exists {
		return false, func() {}
	}
	s.matchLock[appID] = time.Now().Add(time.Duration(s.getRuntime().MatchLockTimeoutSec) * time.Second)
	return true, func() {
		s.matchMu.Lock()
		delete(s.matchLock, appID)
		s.matchMu.Unlock()
	}
}

func (s *APIServer) cleanupMatchLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.matchMu.Lock()
		for appID, exp := range s.matchLock {
			if now.After(exp) {
				delete(s.matchLock, appID)
			}
		}
		s.matchMu.Unlock()
	}
}

func (s *APIServer) recordMetric(appID, endpoint string, start time.Time, statusCode string) {
	if appID == "" {
		return
	}
	latency := time.Since(start).Milliseconds()
	date := time.Now().Format("2006-01-02")
	now := time.Now().UTC().Format(time.RFC3339)

	success, authFail, limited, upFail, timeout := 0, 0, 0, 0, 0
	switch statusCode {
	case "OK":
		success = 1
	case "AUTH_SIGNATURE_INVALID", "AUTH_HEADER_MISSING", "AUTH_INVALID_APP", "AUTH_TIMESTAMP_INVALID", "AUTH_TIMESTAMP_EXPIRED":
		authFail = 1
	case "RATE_LIMITED", "MATCH_IN_FLIGHT":
		limited = 1
	case "UPSTREAM_TIMEOUT":
		timeout = 1
	default:
		if strings.Contains(statusCode, "UPSTREAM") {
			upFail = 1
		}
	}
	_, err := s.db.Exec(`INSERT INTO app_metrics_daily(app_id, endpoint, date, total, success, auth_fail, rate_limited, upstream_fail, timeout, total_latency_ms, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(app_id, endpoint, date) DO UPDATE SET
			total=total+1,
			success=success+excluded.success,
			auth_fail=auth_fail+excluded.auth_fail,
			rate_limited=rate_limited+excluded.rate_limited,
			upstream_fail=upstream_fail+excluded.upstream_fail,
			timeout=timeout+excluded.timeout,
			total_latency_ms=total_latency_ms+excluded.total_latency_ms,
			updated_at=excluded.updated_at`,
		appID, endpoint, date, 1, success, authFail, limited, upFail, timeout, latency, now)
	if err != nil {
		log.Printf("record metric error: %v", err)
	}
}

func (s *APIServer) createRiskEvent(user User, level, rule string, metric float64, detail string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.Exec(`INSERT INTO risk_events(app_id, user_id, level, rule_name, metric_value, detail, created_at)
		VALUES(?,?,?,?,?,?,?)`, user.AppID, user.ID, level, rule, metric, detail, now)

	if !s.getRuntime().AutoBanEnabled || level != "high" {
		return
	}
	banUntil := time.Now().Add(time.Duration(s.getRuntime().AutoBanMinutes) * time.Minute).UTC().Format(time.RFC3339)
	_, _ = s.db.Exec(`UPDATE users SET status='banned', ban_reason=?, ban_until=?, updated_at=? WHERE id=?`,
		"auto risk control ban", banUntil, now, user.ID)
}

func (s *APIServer) handleTurnstileSiteKey(w http.ResponseWriter, r *http.Request) {
	if s.cfg.TurnstileSiteKey == "" {
		writeJSON(w, http.StatusOK, "OK", "", map[string]string{"siteKey": ""})
		return
	}
	writeJSON(w, http.StatusOK, "OK", "", map[string]string{"siteKey": s.cfg.TurnstileSiteKey})
}

func (s *APIServer) handleSendRegisterCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email          string `json:"email"`
		TurnstileToken string `json:"turnstileToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json", nil)
		return
	}
	if err := s.verifyTurnstile(req.TurnstileToken, clientIP(r)); err != nil {
		writeJSON(w, http.StatusBadRequest, "TURNSTILE_INVALID", err.Error(), nil)
		return
	}
	if !strings.Contains(req.Email, "@") {
		writeJSON(w, http.StatusBadRequest, "EMAIL_INVALID", "invalid email", nil)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	ip := clientIP(r)
	keys := []string{
		"register:email:" + email,
		"register:ip:" + ip,
		"register:ua:" + shortHash(strings.ToLower(strings.TrimSpace(r.UserAgent()))),
	}
	if err := s.ensureEmailSendAllowedMulti(keys, time.Minute); err != nil {
		writeJSON(w, http.StatusTooManyRequests, "EMAIL_RATE_LIMITED", err.Error(), nil)
		return
	}
	code := randCode(6)
	if err := s.storeEmailCode(email, "register", code, 10*time.Minute); err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "store code failed", nil)
		return
	}
	if err := s.sendEmail(email, "AniLink Proxy 注册验证码", "验证码："+code+"，10分钟内有效。"); err != nil {
		writeJSON(w, http.StatusInternalServerError, "SMTP_SEND_FAILED", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, "OK", "sent", nil)
}

func (s *APIServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		EmailCode string `json:"emailCode"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json", nil)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if len(req.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, "PASSWORD_WEAK", "password length must >= 8", nil)
		return
	}
	ok, err := s.verifyEmailCode(email, "register", req.EmailCode)
	if err != nil || !ok {
		writeJSON(w, http.StatusBadRequest, "EMAIL_CODE_INVALID", "email code invalid", nil)
		return
	}
	pwHash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	now := time.Now().UTC().Format(time.RFC3339)
	appID := "app_" + randString(20)
	secret := randString(48)
	_, err = s.db.Exec(`INSERT INTO users(email, password_hash, app_id, app_secret, role, status, secret_shown, created_at, updated_at)
		VALUES(?,?,?,?,?,?,0,?,?)`, email, string(pwHash), appID, secret, roleUser, "active", now, now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, "REGISTER_FAILED", "email may already exists", nil)
		return
	}
	writeJSON(w, http.StatusOK, "OK", "register success", map[string]string{
		"appId":    appID,
		"appSecret": secret,
	})
}

func (s *APIServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email          string `json:"email"`
		Password       string `json:"password"`
		TurnstileToken string `json:"turnstileToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json", nil)
		return
	}
	if err := s.verifyTurnstile(req.TurnstileToken, clientIP(r)); err != nil {
		writeJSON(w, http.StatusBadRequest, "TURNSTILE_INVALID", err.Error(), nil)
		return
	}
	var u User
	var secretShown int
	err := s.db.QueryRow(`SELECT id,email,password_hash,app_id,app_secret,secret_shown,role,status,created_at FROM users WHERE email=?`,
		strings.ToLower(strings.TrimSpace(req.Email))).
		Scan(&u.ID, &u.Email, &u.Password, &u.AppID, &u.AppSecret, &secretShown, &u.Role, &u.Status, &u.CreatedAt)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, "LOGIN_FAILED", "invalid credentials", nil)
		return
	}
	u.SecretSeen = secretShown == 1
	if u.Status == "banned" {
		writeJSON(w, http.StatusForbidden, "BANNED", "account banned", nil)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.Password)) != nil {
		writeJSON(w, http.StatusUnauthorized, "LOGIN_FAILED", "invalid credentials", nil)
		return
	}
	token, err := s.makeJWT(u)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "token failed", nil)
		return
	}
	_, _ = s.db.Exec(`UPDATE users SET last_login_at=?, updated_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), u.ID)
	writeJSON(w, http.StatusOK, "OK", "", map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":        u.ID,
			"email":     u.Email,
			"appId":     u.AppID,
			"role":      u.Role,
			"status":    u.Status,
			"secretShown": u.SecretSeen,
		},
	})
}

func (s *APIServer) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	writeJSON(w, http.StatusOK, "OK", "", map[string]interface{}{
		"id":         u.ID,
		"email":      u.Email,
		"appId":      u.AppID,
		"role":       u.Role,
		"status":     u.Status,
		"secretShown": u.SecretSeen,
	})
}

func (s *APIServer) handleSendResetCode(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	ip := clientIP(r)
	keys := []string{
		"secret_reset:user:" + strconv.FormatInt(u.ID, 10),
		"secret_reset:email:" + strings.ToLower(strings.TrimSpace(u.Email)),
		"secret_reset:ip:" + ip,
	}
	if err := s.ensureEmailSendAllowedMulti(keys, time.Minute); err != nil {
		writeJSON(w, http.StatusTooManyRequests, "EMAIL_RATE_LIMITED", err.Error(), nil)
		return
	}
	code := randCode(6)
	if err := s.storeEmailCode(u.Email, "secret_reset", code, 10*time.Minute); err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "store code failed", nil)
		return
	}
	if err := s.sendEmail(u.Email, "AniLink Proxy Secret 操作验证码", "验证码："+code+"，10分钟内有效。"); err != nil {
		writeJSON(w, http.StatusInternalServerError, "SMTP_SEND_FAILED", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, "OK", "sent", nil)
}

func (s *APIServer) handleRevealSecret(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	var secret string
	if err := s.db.QueryRow(`SELECT app_secret FROM users WHERE id=?`, u.ID).Scan(&secret); err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "query failed", nil)
		return
	}
	_, _ = s.db.Exec(`UPDATE users SET secret_shown=1, updated_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339), u.ID)
	writeJSON(w, http.StatusOK, "OK", "", map[string]string{"appSecret": secret})
}

func (s *APIServer) handleResetSecret(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	var req struct {
		EmailCode string `json:"emailCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json", nil)
		return
	}
	ok, _ := s.verifyEmailCode(u.Email, "secret_reset", req.EmailCode)
	if !ok {
		writeJSON(w, http.StatusBadRequest, "EMAIL_CODE_INVALID", "email code invalid", nil)
		return
	}
	newSecret := randString(48)
	_, err := s.db.Exec(`UPDATE users SET app_secret=?, secret_shown=1, updated_at=? WHERE id=?`,
		newSecret, time.Now().UTC().Format(time.RFC3339), u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "reset failed", nil)
		return
	}
	writeJSON(w, http.StatusOK, "OK", "", map[string]string{"appSecret": newSecret})
}

func (s *APIServer) handleMyStats(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" {
		from = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	}
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}
	rows, err := s.db.Query(`SELECT endpoint, SUM(total), SUM(success), SUM(auth_fail), SUM(rate_limited), SUM(upstream_fail), SUM(timeout), SUM(total_latency_ms)
		FROM app_metrics_daily WHERE app_id=? AND date>=? AND date<=?
		GROUP BY endpoint ORDER BY endpoint`, u.AppID, from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "query failed", nil)
		return
	}
	defer rows.Close()
	type rowData struct {
		Endpoint     string  `json:"endpoint"`
		Total        int64   `json:"total"`
		Success      int64   `json:"success"`
		AuthFail     int64   `json:"authFail"`
		RateLimited  int64   `json:"rateLimited"`
		UpstreamFail int64   `json:"upstreamFail"`
		Timeout      int64   `json:"timeout"`
		AvgLatencyMs float64 `json:"avgLatencyMs"`
	}
	var out []rowData
	for rows.Next() {
		var d rowData
		var totalLatency int64
		if err := rows.Scan(&d.Endpoint, &d.Total, &d.Success, &d.AuthFail, &d.RateLimited, &d.UpstreamFail, &d.Timeout, &totalLatency); err == nil {
			if d.Total > 0 {
				d.AvgLatencyMs = float64(totalLatency) / float64(d.Total)
			}
			out = append(out, d)
		}
	}
	writeJSON(w, http.StatusOK, "OK", "", out)
}

func (s *APIServer) handleMyRisk(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	rows, err := s.db.Query(`SELECT level, rule_name, metric_value, detail, created_at FROM risk_events WHERE user_id=? ORDER BY id DESC LIMIT 100`, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "query failed", nil)
		return
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var level, rule, detail, created string
		var metric float64
		if err := rows.Scan(&level, &rule, &metric, &detail, &created); err == nil {
			out = append(out, map[string]interface{}{
				"level": level, "rule": rule, "metric": metric, "detail": detail, "createdAt": created,
			})
		}
	}
	writeJSON(w, http.StatusOK, "OK", "", out)
}

func (s *APIServer) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,email,app_id,role,status,ban_reason,ban_until,created_at FROM users ORDER BY id DESC`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "query failed", nil)
		return
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id int64
		var email, appID, role, status, created string
		var reason, until sql.NullString
		if err := rows.Scan(&id, &email, &appID, &role, &status, &reason, &until, &created); err == nil {
			out = append(out, map[string]interface{}{
				"id": id, "email": email, "appId": appID, "role": role, "status": status,
				"banReason": reason.String, "banUntil": until.String, "createdAt": created,
			})
		}
	}
	writeJSON(w, http.StatusOK, "OK", "", out)
}

func (s *APIServer) handleAdminBan(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	var req struct {
		Reason   string `json:"reason"`
		Minutes  int    `json:"minutes"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Minutes <= 0 {
		req.Minutes = 60 * 24
	}
	now := time.Now().UTC().Format(time.RFC3339)
	until := time.Now().Add(time.Duration(req.Minutes) * time.Minute).UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE users SET status='banned', ban_reason=?, ban_until=?, updated_at=? WHERE id=?`,
		req.Reason, until, now, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ban failed", nil)
		return
	}
	writeJSON(w, http.StatusOK, "OK", "banned", nil)
}

func (s *APIServer) handleAdminUnban(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	_, err := s.db.Exec(`UPDATE users SET status='active', ban_reason=NULL, ban_until=NULL, updated_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "unban failed", nil)
		return
	}
	writeJSON(w, http.StatusOK, "OK", "unbanned", nil)
}

func (s *APIServer) handleAdminGlobalStats(w http.ResponseWriter, r *http.Request) {
	row := s.db.QueryRow(`SELECT SUM(total), SUM(success), SUM(auth_fail), SUM(rate_limited), SUM(upstream_fail), SUM(timeout) FROM app_metrics_daily`)
	var total, success, authFail, limited, upFail, timeout sql.NullInt64
	if err := row.Scan(&total, &success, &authFail, &limited, &upFail, &timeout); err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "query failed", nil)
		return
	}
	writeJSON(w, http.StatusOK, "OK", "", map[string]interface{}{
		"total": total.Int64, "success": success.Int64, "authFail": authFail.Int64,
		"rateLimited": limited.Int64, "upstreamFail": upFail.Int64, "timeout": timeout.Int64,
	})
}

func (s *APIServer) handleAdminGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, "OK", "", s.getRuntime())
}

func (s *APIServer) handleAdminUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var cfg RuntimeConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json", nil)
		return
	}
	if cfg.TimestampToleranceSec <= 0 {
		cfg.TimestampToleranceSec = 300
	}
	if cfg.BodySizeLimitBytes <= 0 {
		cfg.BodySizeLimitBytes = 1024 * 1024
	}
	if cfg.BatchMaxItems <= 0 {
		cfg.BatchMaxItems = 30
	}
	if cfg.MatchLockTimeoutSec <= 0 {
		cfg.MatchLockTimeoutSec = 45
	}
	if err := saveRuntimeConfig(s.db, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "save failed", nil)
		return
	}
	s.runtimeMu.Lock()
	s.runtime = cfg
	s.runtimeMu.Unlock()
	writeJSON(w, http.StatusOK, "OK", "updated", cfg)
}

func (s *APIServer) verifyTurnstile(token, remoteIP string) error {
	if s.cfg.TurnstileSecretKey == "" {
		return errors.New("turnstile not configured")
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("turnstile token is required")
	}
	form := url.Values{}
	form.Set("secret", s.cfg.TurnstileSecretKey)
	form.Set("response", strings.TrimSpace(token))
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequest(http.MethodPost, "https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("turnstile request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("turnstile verify failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool     `json:"success"`
		Errors  []string `json:"error-codes"`
	}
	if err = json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("turnstile parse failed: %w", err)
	}
	if !result.Success {
		if len(result.Errors) > 0 {
			return fmt.Errorf("turnstile rejected: %s", strings.Join(result.Errors, ","))
		}
		return errors.New("turnstile rejected")
	}
	return nil
}

func (s *APIServer) ensureEmailSendAllowed(rateKey string, interval time.Duration) error {
	now := time.Now().UTC()
	var lastSent string
	err := s.db.QueryRow(`SELECT last_sent_at FROM email_send_rate WHERE rate_key=?`, rateKey).Scan(&lastSent)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("rate limit check failed")
	}
	if err == nil {
		t, parseErr := time.Parse(time.RFC3339, lastSent)
		if parseErr == nil {
			wait := interval - now.Sub(t)
			if wait > 0 {
				return fmt.Errorf("发送过于频繁，请 %d 秒后重试", int(wait.Seconds())+1)
			}
		}
		_, err = s.db.Exec(`UPDATE email_send_rate SET last_sent_at=? WHERE rate_key=?`, now.Format(time.RFC3339), rateKey)
		return err
	}
	_, err = s.db.Exec(`INSERT INTO email_send_rate(rate_key, last_sent_at) VALUES(?,?)`, rateKey, now.Format(time.RFC3339))
	return err
}

func (s *APIServer) ensureEmailSendAllowedMulti(rateKeys []string, interval time.Duration) error {
	if len(rateKeys) == 0 {
		return nil
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return errors.New("rate limit check failed")
	}
	defer tx.Rollback()

	for _, key := range rateKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		var lastSent string
		qErr := tx.QueryRow(`SELECT last_sent_at FROM email_send_rate WHERE rate_key=?`, key).Scan(&lastSent)
		if qErr != nil && !errors.Is(qErr, sql.ErrNoRows) {
			return errors.New("rate limit check failed")
		}
		if qErr == nil {
			t, parseErr := time.Parse(time.RFC3339, lastSent)
			if parseErr == nil {
				wait := interval - now.Sub(t)
				if wait > 0 {
					return fmt.Errorf("发送过于频繁，请 %d 秒后重试", int(wait.Seconds())+1)
				}
			}
		}
	}

	for _, key := range rateKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		_, upErr := tx.Exec(`INSERT INTO email_send_rate(rate_key, last_sent_at) VALUES(?,?)
			ON CONFLICT(rate_key) DO UPDATE SET last_sent_at=excluded.last_sent_at`, key, now.Format(time.RFC3339))
		if upErr != nil {
			return errors.New("rate limit write failed")
		}
	}
	return tx.Commit()
}

func (s *APIServer) storeEmailCode(email, purpose, code string, ttl time.Duration) error {
	now := time.Now().UTC()
	exp := now.Add(ttl)
	_, err := s.db.Exec(`INSERT INTO email_codes(email,purpose,code_hash,expire_at,created_at) VALUES(?,?,?,?,?)`,
		email, purpose, shaHex(strings.TrimSpace(code)), exp.Format(time.RFC3339), now.Format(time.RFC3339))
	return err
}

func (s *APIServer) verifyEmailCode(email, purpose, code string) (bool, error) {
	rows, err := s.db.Query(`SELECT id, code_hash, expire_at FROM email_codes WHERE email=? AND purpose=? ORDER BY id DESC LIMIT 5`, email, purpose)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	target := shaHex(strings.TrimSpace(code))
	var okID int64
	for rows.Next() {
		var id int64
		var hash, exp string
		if err := rows.Scan(&id, &hash, &exp); err != nil {
			continue
		}
		t, _ := time.Parse(time.RFC3339, exp)
		if time.Now().After(t) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(hash), []byte(target)) == 1 {
			okID = id
			break
		}
	}
	if okID == 0 {
		return false, nil
	}
	_, _ = s.db.Exec(`DELETE FROM email_codes WHERE id=?`, okID)
	return true, nil
}

func (s *APIServer) sendEmail(to, subject, body string) error {
	if s.cfg.SMTPHost == "" || s.cfg.SMTPUser == "" || s.cfg.SMTPPass == "" || s.cfg.SMTPFrom == "" {
		return errors.New("smtp not configured")
	}
	addr := net.JoinHostPort(s.cfg.SMTPHost, strconv.Itoa(s.cfg.SMTPPort))
	msg := "From: " + s.cfg.SMTPFrom + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		body + "\r\n"
	auth := smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, s.cfg.SMTPHost)
	tlsCfg := &tls.Config{
		ServerName: s.cfg.SMTPHost,
		MinVersion: tls.VersionTLS12,
	}

	// 465 常见为 SMTPS（隐式 TLS），587 常见为 STARTTLS。
	if s.cfg.SMTPPort == 465 {
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("smtp tls dial failed: %w", err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, s.cfg.SMTPHost)
		if err != nil {
			return fmt.Errorf("smtp client create failed: %w", err)
		}
		defer client.Quit()

		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth failed: %w", err)
		}
		if err = client.Mail(s.cfg.SMTPFrom); err != nil {
			return fmt.Errorf("smtp mail from failed: %w", err)
		}
		if err = client.Rcpt(to); err != nil {
			return fmt.Errorf("smtp rcpt failed: %w", err)
		}
		w, err := client.Data()
		if err != nil {
			return fmt.Errorf("smtp data failed: %w", err)
		}
		if _, err = w.Write([]byte(msg)); err != nil {
			return fmt.Errorf("smtp write failed: %w", err)
		}
		if err = w.Close(); err != nil {
			return fmt.Errorf("smtp close failed: %w", err)
		}
		return nil
	}

	// 其他端口走标准 SMTP + STARTTLS（若服务端支持）。
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("smtp dial failed: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("smtp client create failed: %w", err)
	}
	defer client.Quit()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err = client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp starttls failed: %w", err)
		}
	}
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth failed: %w", err)
	}
	if err = client.Mail(s.cfg.SMTPFrom); err != nil {
		return fmt.Errorf("smtp mail from failed: %w", err)
	}
	if err = client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt failed: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data failed: %w", err)
	}
	if _, err = w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write failed: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("smtp close failed: %w", err)
	}
	return nil
}

func (s *APIServer) findUserByAppID(appID string) (User, error) {
	var u User
	var secretShown int
	err := s.db.QueryRow(`SELECT id,email,password_hash,app_id,app_secret,secret_shown,role,status,ban_reason,ban_until,created_at
		FROM users WHERE app_id=?`, appID).
		Scan(&u.ID, &u.Email, &u.Password, &u.AppID, &u.AppSecret, &secretShown, &u.Role, &u.Status, &u.BanReason, &u.BanUntil, &u.CreatedAt)
	u.SecretSeen = secretShown == 1
	return u, err
}

func (s *APIServer) makeJWT(u User) (string, error) {
	claims := authClaims{
		UserID: u.ID,
		Role:   u.Role,
		Email:  u.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(72 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "anilink-proxy",
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(s.cfg.JWTSecret))
}

func (s *APIServer) parseJWT(tokenStr string) (User, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &authClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		return User{}, errors.New("invalid token")
	}
	claims := token.Claims.(*authClaims)
	var u User
	var secretShown int
	err = s.db.QueryRow(`SELECT id,email,password_hash,app_id,app_secret,secret_shown,role,status,ban_reason,ban_until,created_at
		FROM users WHERE id=?`, claims.UserID).
		Scan(&u.ID, &u.Email, &u.Password, &u.AppID, &u.AppSecret, &secretShown, &u.Role, &u.Status, &u.BanReason, &u.BanUntil, &u.CreatedAt)
	u.SecretSeen = secretShown == 1
	return u, err
}

type ctxKey string

const userCtxKey ctxKey = "user"

func (s *APIServer) authUserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing bearer token", nil)
			return
		}
		u, err := s.parseJWT(strings.TrimPrefix(auth, "Bearer "))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token", nil)
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *APIServer) authAdminMiddleware(next http.Handler) http.Handler {
	return s.authUserMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r.Context())
		if u.Role != roleAdmin {
			writeJSON(w, http.StatusForbidden, "FORBIDDEN", "admin only", nil)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func userFromCtx(ctx context.Context) User {
	v := ctx.Value(userCtxKey)
	if v == nil {
		return User{}
	}
	return v.(User)
}

func (s *APIServer) getRuntime() RuntimeConfig {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.runtime
}

func writeJSON(w http.ResponseWriter, status int, code, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(jsonResp{
		Code:    code,
		Message: message,
		Data:    data,
	})
}

func statusForCode(code string) int {
	switch code {
	case "AUTH_HEADER_MISSING", "AUTH_INVALID_APP", "AUTH_SIGNATURE_INVALID", "AUTH_TIMESTAMP_INVALID", "AUTH_TIMESTAMP_EXPIRED":
		return http.StatusUnauthorized
	case "BANNED":
		return http.StatusForbidden
	default:
		return http.StatusBadRequest
	}
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var b strings.Builder
	for i := 0; i < n; i++ {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		b.WriteByte(letters[num.Int64()])
	}
	return b.String()
}

func randCode(n int) string {
	const digits = "0123456789"
	var b strings.Builder
	for i := 0; i < n; i++ {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		b.WriteByte(digits[num.Int64()])
	}
	return b.String()
}

func shaHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func shortHash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "empty"
	}
	v := shaHex(s)
	if len(v) > 12 {
		return v[:12]
	}
	return v
}

func clientIP(r *http.Request) string {
	xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}
