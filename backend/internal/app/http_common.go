package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
)

const maxAdminJSONBodyBytes = 64 << 10

func writeJSON(w http.ResponseWriter, status int, code, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(jsonResp{Code: code, Message: message, Data: data})
}

func statusForCode(code string) int {
	switch code {
	case "AUTH_HEADER_MISSING", "AUTH_INVALID_APP", "AUTH_SIGNATURE_INVALID", "AUTH_TIMESTAMP_INVALID", "AUTH_TIMESTAMP_EXPIRED", "AUTH_REPLAY_DETECTED":
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

// decodeJSONStrict 限制 JSON 请求体大小并解码；失败时已写入响应，返回 false。
func decodeJSONStrict(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "request body too large", nil)
			return false
		}
		writeJSON(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json", nil)
		return false
	}
	return true
}

// decodeJSONOptional 与 decodeJSONStrict 相同，但允许空 body（EOF），用于可选 JSON 体的接口。
func decodeJSONOptional(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminJSONBodyBytes)
	err := json.NewDecoder(r.Body).Decode(v)
	if err == nil {
		return true
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeJSON(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "request body too large", nil)
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	writeJSON(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json", nil)
	return false
}
