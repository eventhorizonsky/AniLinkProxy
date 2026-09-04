package app

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func loadConfig() (AppConfig, error) {
	port := getenv("PORT", "8080")
	smtpPort, _ := strconv.Atoi(getenv("SMTP_PORT", "587"))
	cfg := AppConfig{
		ListenAddr:             ":" + port,
		Upstream:               getenv("UPSTREAM_BASE_URL", "https://api.dandanplay.net"),
		UpstreamAppID:          os.Getenv("UPSTREAM_DANDAN_APP_ID"),
		UpstreamAppSecret:      os.Getenv("UPSTREAM_DANDAN_APP_SECRET"),
		JWTSecret:              os.Getenv("JWT_SECRET"),
		SQLitePath:             getenv("SQLITE_PATH", "./data/proxy.db"),
		SMTPHost:               os.Getenv("SMTP_HOST"),
		SMTPPort:               smtpPort,
		SMTPUser:               os.Getenv("SMTP_USERNAME"),
		SMTPPass:               os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:               os.Getenv("SMTP_FROM_ADDRESS"),
		TurnstileSiteKey:       os.Getenv("TURNSTILE_SITE_KEY"),
		TurnstileSecretKey:     os.Getenv("TURNSTILE_SECRET_KEY"),
		CaptchaLaSiteKey:       os.Getenv("CAPTCHALA_SITE_KEY"),
		CaptchaLaSecretKey:     os.Getenv("CAPTCHALA_SECRET_KEY"),
		CaptchaLaVerifyURL:     getenv("CAPTCHALA_VERIFY_URL", "https://apiv1.captcha.la/v1/validate"),
		CaptchaLaVerifyErrKeys: getenv("CAPTCHALA_VERIFY_ERROR_CODES", "token_expired,challenge_expired,challenge_not_found,invalid_answer,token_already_used,token_not_found"),
		CaptchaLaVerifyTimeout: captchaVerifyTimeout(),
		CaptchaLaVerifyRetries: captchaVerifyRetries(),
		AdminAllowedOrigin:     strings.TrimSpace(os.Getenv("ADMIN_ALLOWED_ORIGIN")),
		TrustedProxyCIDRs:      strings.TrimSpace(os.Getenv("TRUSTED_PROXY_CIDRS")),
		AuthCookieSecure:       isTruthyEnv("AUTH_COOKIE_SECURE"),
	}
	if raw := strings.TrimSpace(os.Getenv("SECRET_WRAP_KEY")); raw != "" {
		k, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return cfg, fmt.Errorf("SECRET_WRAP_KEY 须为 32 字节的 base64: %w", err)
		}
		if len(k) != 32 {
			return cfg, fmt.Errorf("SECRET_WRAP_KEY 解码后须恰好 32 字节，当前 %d 字节", len(k))
		}
		cfg.SecretWrapKey = k
	}
	if cfg.UpstreamAppID == "" || cfg.UpstreamAppSecret == "" {
		return cfg, errors.New("缺少 UPSTREAM_DANDAN_APP_ID / UPSTREAM_DANDAN_APP_SECRET")
	}
	if len(strings.TrimSpace(cfg.JWTSecret)) < 32 {
		return cfg, fmt.Errorf("JWT_SECRET 过短，生产环境请使用至少 32 位随机字符串")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SQLitePath), 0o755); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func isTruthyEnv(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes"
}

// captchaVerifyTimeout 读取 CAPTCHALA_VERIFY_TIMEOUT_SEC（秒），默认 10s，非法值回退默认。
func captchaVerifyTimeout() time.Duration {
	sec, err := strconv.Atoi(strings.TrimSpace(os.Getenv("CAPTCHALA_VERIFY_TIMEOUT_SEC")))
	if err != nil || sec <= 0 {
		sec = 10
	}
	return time.Duration(sec) * time.Second
}

// captchaVerifyRetries 读取 CAPTCHALA_VERIFY_RETRIES，默认 1，负数按 0 处理。
func captchaVerifyRetries() int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("CAPTCHALA_VERIFY_RETRIES")))
	if err != nil {
		return 1
	}
	if n < 0 {
		return 0
	}
	return n
}

func defaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		TimestampCheckEnabled: true,
		TimestampToleranceSec: 300,
		CacheTTLMin: map[string]int{
			"comment":            30,
			"search":             180,
			"search_anime":       180,
			"bangumi":            360,
			"bgmtv":              360,
			"shin":               360,
			"season_anime":       360,
			"trending_hot":       360,
			"trending_rising":    360,
			"trending_new_anime": 360,
		},
		RateLimit: map[string]EndpointLimit{
			"comment":            {RPS: 6, Burst: 12},
			"comment_push":       {RPS: 2, Burst: 5},
			"search":             {RPS: 2, Burst: 4},
			"search_anime":       {RPS: 2, Burst: 4},
			"bangumi":            {RPS: 2, Burst: 4},
			"bgmtv":              {RPS: 2, Burst: 4},
			"shin":               {RPS: 1, Burst: 2},
			"season_anime":       {RPS: 2, Burst: 4},
			"trending_hot":       {RPS: 2, Burst: 4},
			"trending_rising":    {RPS: 2, Burst: 4},
			"trending_new_anime": {RPS: 2, Burst: 4},
			"match":              {RPS: 0.3, Burst: 1},
			"match_batch":        {RPS: 5, Burst: 5},
		},
		MatchLockTimeoutSec:  45,
		BodySizeLimitBytes:   1024 * 1024,
		UpstreamMaxBodyBytes: 4 * 1024 * 1024,
		BatchMaxItems:        30,
		CacheMaxEntries:      3000,
		CacheMaxBytes:        128 * 1024 * 1024,
		CacheMaxItemBytes:    256 * 1024,
		ReplayCacheSec:       600,
		AutoBanEnabled:       true,
		AutoBanMinutes:       30,
	}
}

func loadRuntimeConfig(db *sql.DB) (RuntimeConfig, error) {
	cfg := defaultRuntimeConfig()
	var raw string
	err := db.QueryRow(`SELECT v FROM system_config WHERE k='runtime_config'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		// 首次启动时将默认配置落库，后续以数据库中的运行时配置为准。
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
	return normalizeRuntimeConfig(cfg), nil
}

func saveRuntimeConfig(db *sql.DB, cfg RuntimeConfig) error {
	cfg = normalizeRuntimeConfig(cfg)
	raw, _ := json.Marshal(cfg)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO system_config(k, v, updated_at) VALUES('runtime_config', ?, ?)
		ON CONFLICT(k) DO UPDATE SET v=excluded.v, updated_at=excluded.updated_at`, string(raw), now)
	return err
}

func normalizeRuntimeConfig(cfg RuntimeConfig) RuntimeConfig {
	if cfg.TimestampToleranceSec <= 0 {
		cfg.TimestampToleranceSec = 300
	}
	if cfg.BodySizeLimitBytes <= 0 {
		cfg.BodySizeLimitBytes = 1024 * 1024
	}
	if cfg.UpstreamMaxBodyBytes <= 0 {
		cfg.UpstreamMaxBodyBytes = 4 * 1024 * 1024
	}
	if cfg.BatchMaxItems <= 0 {
		cfg.BatchMaxItems = 30
	}
	if cfg.MatchLockTimeoutSec <= 0 {
		cfg.MatchLockTimeoutSec = 45
	}
	if cfg.CacheMaxEntries <= 0 {
		cfg.CacheMaxEntries = 3000
	}
	if cfg.CacheMaxBytes <= 0 {
		cfg.CacheMaxBytes = 128 * 1024 * 1024
	}
	if cfg.CacheMaxItemBytes <= 0 {
		cfg.CacheMaxItemBytes = 256 * 1024
	}
	if cfg.ReplayCacheSec <= 0 {
		cfg.ReplayCacheSec = 600
	}
	if cfg.CacheTTLMin == nil {
		cfg.CacheTTLMin = defaultRuntimeConfig().CacheTTLMin
	}
	if cfg.RateLimit == nil {
		cfg.RateLimit = defaultRuntimeConfig().RateLimit
	}
	return cfg
}
