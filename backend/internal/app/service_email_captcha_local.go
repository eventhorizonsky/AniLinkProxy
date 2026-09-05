package app

import (
	"crypto/rand"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"proxy-project/backend/internal/utils"
)

// localCaptchaTTL 为纯本地验证码的有效期（5 分钟）。
const localCaptchaTTL = 5 * time.Minute

// handleCaptchaChallenge 生成一道纯本地验证码题目（如“3 + 5 = ?”），返回 challengeId 与 question。
// 题目与答案仅存于服务端内存，前端仅拿到题目文本；用户作答后随 token 一并提交校验。
func (s *APIServer) handleCaptchaChallenge(w http.ResponseWriter, r *http.Request) {
	// 对获取行为按 IP 做基础限流，防止被批量刷题消耗内存。
	ip := s.clientIP(r)
	if !s.authRL.Allow("captcha:challenge:"+ip, EndpointLimit{RPS: 0.5, Burst: 10}) {
		writeJSON(w, http.StatusTooManyRequests, "CAPTCHA_RATE_LIMITED", "请勿频繁刷新验证码", nil)
		return
	}
	id, question := s.issueCaptchaChallenge()
	writeJSON(w, http.StatusOK, "OK", "", map[string]interface{}{
		"challengeId": id,
		"question":    question,
		"expiresIn":   int(localCaptchaTTL.Seconds()),
	})
}

// issueCaptchaChallenge 生成一道两数相加（结果非负）的题目并保存答案，返回挑战 id 与题目文本。
func (s *APIServer) issueCaptchaChallenge() (id, question string) {
	a := int(randIntn(40)) + 1 // 1..40
	b := int(randIntn(40)) + 1 // 1..40
	answer := a + b
	question = strconv.Itoa(a) + " + " + strconv.Itoa(b) + " = ?"

	now := time.Now()
	id = utils.RandString(24)
	s.captchaMu.Lock()
	defer s.captchaMu.Unlock()
	s.pruneCaptchaChallengesLocked(now)
	s.captchaChallenges[id] = captchaChallenge{
		Answer:   strconv.Itoa(answer),
		ExpireAt: now.Add(localCaptchaTTL),
	}
	return id, question
}

// verifyLocalCaptcha 校验纯本地验证码：token 形如 "<challengeId>:<answer>"。
func (s *APIServer) verifyLocalCaptcha(token string) (verifyOutcome, error) {
	token = strings.TrimSpace(token)
	sep := strings.LastIndex(token, ":")
	if sep <= 0 {
		return verifyRejected, errors.New("local captcha token malformed")
	}
	id := strings.TrimSpace(token[:sep])
	answer := strings.TrimSpace(token[sep+1:])
	if id == "" || answer == "" {
		return verifyRejected, errors.New("local captcha token malformed")
	}
	now := time.Now()
	s.captchaMu.Lock()
	defer s.captchaMu.Unlock()
	ch, ok := s.captchaChallenges[id]
	if !ok || now.After(ch.ExpireAt) {
		delete(s.captchaChallenges, id)
		return verifyRejected, errors.New("local captcha challenge expired")
	}
	// 单次使用：无论对错立即销毁，防止 token 重放。
	delete(s.captchaChallenges, id)
	got, gErr := strconv.Atoi(answer)
	want, wErr := strconv.Atoi(ch.Answer)
	if gErr == nil && wErr == nil && got == want {
		return verifyOK, nil
	}
	return verifyRejected, errors.New("local captcha wrong answer")
}

// pruneCaptchaChallengesLocked 清理已过期的验证码题目；调用方需持有 captchaMu。
func (s *APIServer) pruneCaptchaChallengesLocked(now time.Time) {
	for k, ch := range s.captchaChallenges {
		if now.After(ch.ExpireAt) {
			delete(s.captchaChallenges, k)
		}
	}
}

// randIntn 返回 [0, max) 的随机整数；失败时退化为 0。
func randIntn(max int64) int64 {
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return 0
	}
	return n.Int64()
}
