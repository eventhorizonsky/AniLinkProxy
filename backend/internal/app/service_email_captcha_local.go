package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/dchest/captcha"
)

const (
	// localCaptchaLen 为本地验证码的位数（仅数字 0-9）。
	localCaptchaLen = 6
	// localCaptchaImgW/H 为本地验证码图片尺寸。
	localCaptchaImgW = 160
	localCaptchaImgH = 54
)

// handleCaptchaChallenge 生成一道纯本地图形验证码（由 github.com/dchest/captcha 渲染，
// 含波浪形变/贯穿线/圆形噪点等抗 OCR 混淆），以 base64 data URI 返回给前端展示。
// 答案与有效期由 dchest/captcha 自带的内存存储管理（单次使用、超时自动回收）。
func (s *APIServer) handleCaptchaChallenge(w http.ResponseWriter, r *http.Request) {
	// 对获取行为按 IP 做基础限流，防止被批量刷图消耗资源。
	ip := s.clientIP(r)
	if !s.authRL.Allow("captcha:challenge:"+ip, EndpointLimit{RPS: 0.5, Burst: 10}) {
		writeJSON(w, http.StatusTooManyRequests, "CAPTCHA_RATE_LIMITED", "请勿频繁刷新验证码", nil)
		return
	}
	id := captcha.NewLen(localCaptchaLen)
	var buf bytes.Buffer
	if err := captcha.WriteImage(&buf, id, localCaptchaImgW, localCaptchaImgH); err != nil {
		log.Printf("captcha render: %v", err)
		writeJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "验证码生成失败", nil)
		return
	}
	writeJSON(w, http.StatusOK, "OK", "", map[string]interface{}{
		"challengeId": id,
		"image":       "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
		"expiresIn":   int(captcha.Expiration.Seconds()),
	})
}

// verifyLocalCaptcha 校验纯本地验证码：token 形如 "<challengeId>:<answer>"。
// 由 dchest/captcha 校验（单次使用，答案仅含数字）。
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
	if captcha.VerifyString(id, answer) {
		return verifyOK, nil
	}
	return verifyRejected, errors.New("local captcha wrong answer")
}
