package app

import (
	"container/list"
	"database/sql"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	roleUser  = "user"
	roleAdmin = "admin"
)

// captchaProvider 标识验证码服务提供方。
const (
	captchaProviderCaptchala = "captchala"
	captchaProviderTurnstile = "turnstile"
	// captchaProviderLocal 为纯本地验证码：无需外部服务，题目与校验均在服务端内存完成。
	captchaProviderLocal = "local"
)

// verifyOutcome 表示一次验证码校验的结果，用于驱动“CaptchaLa 优先、额度不足回退 Cloudflare”的流程。
type verifyOutcome int

const (
	// verifyOK 校验通过。
	verifyOK verifyOutcome = iota
	// verifyRejected 校验确认失败（token 无效/已过期等，用户系机器人），不应回退，直接拒绝。
	verifyRejected
	// verifyFallback 当前提供方暂时不可用（额度不足/服务异常等），应回退到其它提供方。
	verifyFallback
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

	// CaptchaLa 为优先使用的验证码提供方；额度不足时降级到 Turnstile。
	CaptchaLaSiteKey       string
	CaptchaLaSecretKey     string
	CaptchaLaVerifyURL     string
	CaptchaLaVerifyErrKeys string
	// CaptchaLaVerifyTimeout 为服务端校验单次超时（默认 10s）；CAPTCHALA_VERIFY_TIMEOUT_SEC 配置。
	CaptchaLaVerifyTimeout time.Duration
	// CaptchaLaVerifyRetries 为网络/超时等瞬时错误时的额外重试次数（默认 1）；CAPTCHALA_VERIFY_RETRIES 配置。
	CaptchaLaVerifyRetries int

	// CaptchaProvider 用于强制指定验证码提供方：配置为 "local" 时使用纯本地验证码，
	// 否则（默认留空）按“CaptchaLa 优先、回退 Turnstile”自动检测。
	CaptchaProvider string

	AdminAllowedOrigin string
	TrustedProxyCIDRs  string

	// SecretWrapKey 为 32 字节 AES-GCM 密钥（建议 base64 置于 SECRET_WRAP_KEY）；为空则 AppSecret 明文落库（兼容旧数据）。
	SecretWrapKey []byte
	// AuthCookieSecure 为 true 时 Set-Cookie 带 Secure（HTTPS 生产环境应开启）。
	AuthCookieSecure bool
}

type RuntimeConfig struct {
	TimestampCheckEnabled bool                     `json:"timestampCheckEnabled"`
	TimestampToleranceSec int64                    `json:"timestampToleranceSec"`
	CacheTTLMin           map[string]int           `json:"cacheTtlMin"`
	RateLimit             map[string]EndpointLimit `json:"rateLimit"`
	MatchLockTimeoutSec   int                      `json:"matchLockTimeoutSec"`
	BodySizeLimitBytes    int64                    `json:"bodySizeLimitBytes"`
	UpstreamMaxBodyBytes  int64                    `json:"upstreamMaxBodyBytes"`
	BatchMaxItems         int                      `json:"batchMaxItems"`
	CacheMaxEntries       int                      `json:"cacheMaxEntries"`
	CacheMaxBytes         int64                    `json:"cacheMaxBytes"`
	CacheMaxItemBytes     int64                    `json:"cacheMaxItemBytes"`
	ReplayCacheSec        int64                    `json:"replayCacheSec"`
	AutoBanEnabled        bool                     `json:"autoBanEnabled"`
	AutoBanMinutes        int                      `json:"autoBanMinutes"`
}

type EndpointLimit struct {
	RPS   float64 `json:"rps"`
	Burst float64 `json:"burst"`
}

type User struct {
	ID                 int64
	Email              string
	Password           string
	AppID              string
	AppSecret          string
	SecretSeen         bool
	Role               string
	Status             string
	BanReason          sql.NullString
	BanUntil           sql.NullString
	CommentPushEnabled bool
	CreatedAt          string
}

// captchaChallenge 表示一道纯本地验证码题目，答案仅存于服务端内存。
type captchaChallenge struct {
	Answer   string
	ExpireAt time.Time
}

type APIServer struct {
	cfg              AppConfig
	db               *sql.DB
	httpClient       *http.Client
	trustedProxyNets []*net.IPNet

	runtimeMu sync.RWMutex
	runtime   RuntimeConfig

	cache  *MemoryCache
	rl     *RateLimiter
	authRL *RateLimiter

	matchMu   sync.Mutex
	matchLock map[string]time.Time

	replayMu   sync.Mutex
	replaySeen map[string]time.Time

	// captchaMu 保护纯本地验证码题目存储（captchaChallenges）。
	captchaMu         sync.Mutex
	captchaChallenges map[string]captchaChallenge

	metricCh chan metricEvent
	riskCh   chan riskEvent
}

type bucket struct {
	Tokens     float64
	LastRefill time.Time
	LastUsed   time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	lastGC  time.Time
}

type cacheValue struct {
	Value    []byte
	ExpireAt time.Time
	Size     int64
}

type MemoryCache struct {
	mu           sync.RWMutex
	data         map[string]cacheValue
	maxEntries   int
	maxBytes     int64
	maxItemBytes int64
	currentBytes int64
	order        *list.List
	index        map[string]*list.Element
}

type cacheOrderEntry struct {
	Key string
}

type jsonResp struct {
	Code    string      `json:"code"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type metricEvent struct {
	AppID      string
	Endpoint   string
	StatusCode string
	LatencyMS  int64
}

type riskEvent struct {
	User   User
	Level  string
	Rule   string
	Metric float64
	Detail string
}

type authClaims struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}
