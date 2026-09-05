package app

import (
	"bytes"
	"crypto/subtle"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"proxy-project/backend/internal/utils"
)

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

// verifyCaptcha 按“CaptchaLa 优先、额度不足降级 Cloudflare Turnstile”的规则校验验证码。
// provider 为前端声明的验证码来源（captchala / turnstile），token 为对应组件生成的 token，
// expectedAction 为该场景应匹配的业务标识（如 login / register），用于校验 token 作用域。
func (s *APIServer) verifyCaptcha(provider, token, remoteIP, expectedAction string) (verifyOutcome, error) {
	switch strings.TrimSpace(provider) {
	case captchaProviderLocal:
		// 纯本地验证码：答辩校验在服务端内存完成，不发起任何外部请求。
		return s.verifyLocalCaptcha(token)
	case captchaProviderCaptchala:
		if s.cfg.CaptchaLaSecretKey == "" {
			// CaptchaLa 未配置完整 → 交给兜底提供方。
			return verifyFallback, errors.New("captchala not configured")
		}
		outcome, err := s.verifyCaptchaLa(token, remoteIP, expectedAction)
		if outcome == verifyFallback {
			log.Printf("captchala fallback (verify): %v", err)
		}
		return outcome, err
	case captchaProviderTurnstile, "":
		if err := s.verifyTurnstile(token, remoteIP); err != nil {
			return verifyRejected, err
		}
		return verifyOK, nil
	default:
		return verifyRejected, fmt.Errorf("unknown captcha provider: %s", provider)
	}
}

// verifyCaptchaLa 调用 CaptchaLa 的服务端校验接口 POST /v1/validate。
// 依据接口语义区分三类结果：验证失败（rejected）、额度/服务不可用（fallback）、通过（ok）。
// 网络/超时等瞬时错误会按配置重试数次；仍失败则视为 fallback，交由上层降级（不会误判用户为机器人）。
func (s *APIServer) verifyCaptchaLa(token, remoteIP, expectedAction string) (verifyOutcome, error) {
	if strings.TrimSpace(token) == "" {
		return verifyRejected, errors.New("captchala token is required")
	}
	payload := map[string]string{"pass_token": strings.TrimSpace(token)}
	if remoteIP != "" {
		payload["client_ip"] = remoteIP
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return verifyFallback, fmt.Errorf("captchala marshal failed: %w", err)
	}

	// 默认使用 http.DefaultTransport（尊重 HTTPS_PROXY/NO_PROXY 环境变量，
	// 便于通过代理访问被墙的 Cloudflare 域名），并为单次请求设置超时。
	timeout := s.cfg.CaptchaLaVerifyTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	attempts := s.cfg.CaptchaLaVerifyRetries + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			// 指数退避后再重试，缓解瞬时网络抖动（封顶 2s）。
			backoff := time.Duration(i) * 400 * time.Millisecond
			if cap := 2 * time.Second; backoff > cap {
				backoff = cap
			}
			time.Sleep(backoff)
		}
		req, err := http.NewRequest(http.MethodPost, s.cfg.CaptchaLaVerifyURL, bytes.NewReader(body))
		if err != nil {
			return verifyFallback, fmt.Errorf("captchala request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-App-Key", s.cfg.CaptchaLaSiteKey)
		req.Header.Set("X-App-Secret", s.cfg.CaptchaLaSecretKey)
		resp, err := client.Do(req)
		if err != nil {
			// 网络/超时等未收到响应的瞬时错误：记录并重试（仍失败则作为 fallback 交给上层）。
			lastErr = fmt.Errorf("captchala verify failed: %w", err)
			continue
		}
		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("captchala read failed (status=%d): %w", resp.StatusCode, readErr)
			continue
		}

		var result struct {
			Code    int    `json:"code"`
			Error   string `json:"error"`
			Message string `json:"message"`
			Data    struct {
				Valid    bool   `json:"valid"`
				Degraded bool   `json:"degraded"`
				Reason   string `json:"reason"`
				Action   string `json:"action"`
				Error    string `json:"error"`
			} `json:"data"`
		}
		// 响应无法解析时视为服务/额度层面异常，交由上层降级（不要因此误判用户为机器人）。
		if err = json.Unmarshal(raw, &result); err != nil {
			return verifyFallback, fmt.Errorf("captchala parse failed (status=%d): %w", resp.StatusCode, err)
		}
		// 错误码可能出现在顶层 error（如 service_online）或 data.error（如 token_not_found）。
		errCode := result.Error
		if errCode == "" {
			errCode = result.Data.Error
		}
		if errCode != "" {
			// 命中“验证失败”错误码（token 无效/过期等）→ 拒绝；其余（额度/限流/服务异常等）→ 降级。
			if captchalaIsVerificationCode(s.cfg.CaptchaLaVerifyErrKeys, errCode) {
				return verifyRejected, fmt.Errorf("captchala rejected: %s", errCode)
			}
			return verifyFallback, fmt.Errorf("captchala unavailable: %s", errCode)
		}
		if result.Data.Valid {
			// token 作用域不符：例如用 pay 场景的 token 登录 → 拒绝。
			if expectedAction != "" && result.Data.Action != "" && result.Data.Action != expectedAction {
				return verifyRejected, fmt.Errorf("captchala action mismatch: got %s want %s", result.Data.Action, expectedAction)
			}
			return verifyOK, nil
		}
		// 额度耗尽（degraded）→ 降级；其余 valid=false 视为验证失败 → 拒绝。
		if result.Data.Degraded || strings.Contains(strings.ToLower(result.Data.Reason), "quota") {
			return verifyFallback, fmt.Errorf("captchala degraded: %s", result.Data.Reason)
		}
		return verifyRejected, errors.New("captchala rejected: token invalid")
	}
	if lastErr == nil {
		lastErr = errors.New("captchala verify failed")
	}
	return verifyFallback, lastErr
}

// captchalaIsVerificationCode 判断 CaptchaLa 返回的错误码是否属于“验证不通过”，
// 而非额度不足/服务异常。判定关键字可通过 CAPTCHALA_VERIFY_ERROR_CODES 配置，默认见 config.go。
func captchalaIsVerificationCode(rawKeys, code string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return false
	}
	for _, k := range strings.Split(rawKeys, ",") {
		if strings.ToLower(strings.TrimSpace(k)) == code {
			return true
		}
	}
	return false
}

func (s *APIServer) ensureEmailSendAllowed(rateKey string, interval time.Duration) error {
	now := time.Now().UTC()
	var lastSent string
	err := s.db.QueryRow(`SELECT last_sent_at FROM email_send_rate WHERE rate_key=?`, rateKey).Scan(&lastSent)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("rate limit check failed")
	}
	if err == nil {
		if t, parseErr := time.Parse(time.RFC3339, lastSent); parseErr == nil {
			if wait := interval - now.Sub(t); wait > 0 {
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
			if t, parseErr := time.Parse(time.RFC3339, lastSent); parseErr == nil {
				if wait := interval - now.Sub(t); wait > 0 {
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
		if _, upErr := tx.Exec(`INSERT INTO email_send_rate(rate_key, last_sent_at) VALUES(?,?) ON CONFLICT(rate_key) DO UPDATE SET last_sent_at=excluded.last_sent_at`, key, now.Format(time.RFC3339)); upErr != nil {
			return errors.New("rate limit write failed")
		}
	}
	return tx.Commit()
}

func (s *APIServer) storeEmailCode(email, purpose, code string, ttl time.Duration) error {
	now := time.Now().UTC()
	exp := now.Add(ttl)
	_, err := s.db.Exec(`INSERT INTO email_codes(email,purpose,code_hash,expire_at,created_at) VALUES(?,?,?,?,?)`, email, purpose, utils.ShaHex(strings.TrimSpace(code)), exp.Format(time.RFC3339), now.Format(time.RFC3339))
	return err
}
func (s *APIServer) verifyEmailCode(email, purpose, code string) (bool, error) {
	now := time.Now().UTC()
	rateKey := purpose + ":" + strings.ToLower(strings.TrimSpace(email))
	var failCount int
	var lockUntil sql.NullString
	_ = s.db.QueryRow(`SELECT fail_count, lock_until FROM email_code_attempts WHERE rate_key=?`, rateKey).Scan(&failCount, &lockUntil)
	if lockUntil.Valid {
		if t, err := time.Parse(time.RFC3339, lockUntil.String); err == nil && now.Before(t) {
			return false, errors.New("email code attempts exceeded")
		}
	}

	rows, err := s.db.Query(`SELECT id, code_hash, expire_at FROM email_codes WHERE email=? AND purpose=? ORDER BY id DESC LIMIT 5`, email, purpose)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	target := utils.ShaHex(strings.TrimSpace(code))
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
		failCount++
		lock := sql.NullString{}
		if failCount >= 8 {
			lock = sql.NullString{
				String: now.Add(10 * time.Minute).Format(time.RFC3339),
				Valid:  true,
			}
		}
		var lockVal interface{}
		if lock.Valid {
			lockVal = lock.String
		}
		_, _ = s.db.Exec(`INSERT INTO email_code_attempts(rate_key, fail_count, lock_until, updated_at) VALUES(?,?,?,?)
			ON CONFLICT(rate_key) DO UPDATE SET fail_count=excluded.fail_count, lock_until=excluded.lock_until, updated_at=excluded.updated_at`,
			rateKey, failCount, lockVal, now.Format(time.RFC3339))
		return false, nil
	}
	_, _ = s.db.Exec(`DELETE FROM email_codes WHERE email=? AND purpose=?`, email, purpose)
	_, _ = s.db.Exec(`DELETE FROM email_code_attempts WHERE rate_key=?`, rateKey)
	return true, nil
}
func (s *APIServer) sendEmail(to, subject, body string) error {
	if s.cfg.SMTPHost == "" || s.cfg.SMTPUser == "" || s.cfg.SMTPPass == "" || s.cfg.SMTPFrom == "" {
		return errors.New("smtp not configured")
	}
	addr := net.JoinHostPort(s.cfg.SMTPHost, strconv.Itoa(s.cfg.SMTPPort))
	msg := "From: " + s.cfg.SMTPFrom + "\r\n" + "To: " + to + "\r\n" + "Subject: " + subject + "\r\n" + "MIME-Version: 1.0\r\n" + "Content-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n"
	auth := smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, s.cfg.SMTPHost)
	tlsCfg := &tls.Config{ServerName: s.cfg.SMTPHost, MinVersion: tls.VersionTLS12}
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
