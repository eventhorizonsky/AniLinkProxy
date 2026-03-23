package app

import (
	"database/sql"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
