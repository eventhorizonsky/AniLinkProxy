package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"proxy-project/backend/internal/utils"

	"golang.org/x/crypto/bcrypt"
)

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
	if err := s.verifyTurnstile(req.TurnstileToken, utils.ClientIP(r)); err != nil {
		writeJSON(w, http.StatusBadRequest, "TURNSTILE_INVALID", err.Error(), nil)
		return
	}
	if !strings.Contains(req.Email, "@") {
		writeJSON(w, http.StatusBadRequest, "EMAIL_INVALID", "invalid email", nil)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	ip := utils.ClientIP(r)
	keys := []string{
		"register:email:" + email,
		"register:ip:" + ip,
		"register:ua:" + utils.ShortHash(strings.ToLower(strings.TrimSpace(r.UserAgent()))),
	}
	if err := s.ensureEmailSendAllowedMulti(keys, time.Minute); err != nil {
		writeJSON(w, http.StatusTooManyRequests, "EMAIL_RATE_LIMITED", err.Error(), nil)
		return
	}
	code := utils.RandCode(6)
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
		Email     string `json:"email"`
		EmailCode string `json:"emailCode"`
		Password  string `json:"password"`
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
	appID := "app_" + utils.RandString(20)
	secret := utils.RandString(48)
	_, err = s.db.Exec(`INSERT INTO users(email, password_hash, app_id, app_secret, role, status, secret_shown, created_at, updated_at)
		VALUES(?,?,?,?,?,?,0,?,?)`, email, string(pwHash), appID, secret, roleUser, "active", now, now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, "REGISTER_FAILED", "email may already exists", nil)
		return
	}
	writeJSON(w, http.StatusOK, "OK", "register success", map[string]string{
		"appId":     appID,
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
	if err := s.verifyTurnstile(req.TurnstileToken, utils.ClientIP(r)); err != nil {
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
			"id":         u.ID,
			"email":      u.Email,
			"appId":      u.AppID,
			"role":       u.Role,
			"status":     u.Status,
			"secretShown": u.SecretSeen,
		},
	})
}
