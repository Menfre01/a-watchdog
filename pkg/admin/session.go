package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// sessionCookieName 是管理员会话 cookie 名。
	sessionCookieName = "awd_session"
	// csrfHeader 是变更类请求必须携带的自定义头(CSRF 缓解)。
	csrfHeader = "X-DS-Context-CSRF"
	// sessionTTL 是会话有效期。
	sessionTTL = 12 * time.Hour
)

// newSessionToken 生成会话令牌:过期时间戳 + HMAC-SHA256 签名(无服务端状态)。
func (a *Admin) newSessionToken(now time.Time) string {
	exp := strconv.FormatInt(now.Add(sessionTTL).Unix(), 10)
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(exp))
	return exp + "." + hex.EncodeToString(mac.Sum(nil))
}

// validSession 校验会话 cookie 的签名与有效期。
func (a *Admin) validSession(r *http.Request) bool {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	exp, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	want, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(exp))
	if !hmac.Equal(mac.Sum(nil), want) {
		return false
	}
	ts, err := strconv.ParseInt(exp, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < ts
}

// handleLogin 校验口令并签发会话 cookie。
func (a *Admin) handleLogin(w http.ResponseWriter, r *http.Request) {
	if a.password == "" {
		writeJSONError(w, http.StatusUnauthorized, "admin password not configured")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(a.password)) != 1 {
		writeJSONError(w, http.StatusUnauthorized, "wrong password")
		return
	}
	// #nosec G124 -- Secure 由连接是否 TLS 动态决定(r.TLS),gosec 无法静态证明;
	// 本机明文 HTTP 场景刻意不设 Secure(否则浏览器丢弃该 cookie)。
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    a.newSessionToken(time.Now()),
		Path:     "/console",
		HttpOnly: true,
		// 默认场景是本机明文 HTTP(Secure 会被浏览器丢弃);经 TLS 访问时自动携带 Secure。
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleLogout 清空会话 cookie。
func (a *Admin) handleLogout(w http.ResponseWriter, r *http.Request) {
	// #nosec G124 -- 同上:Secure 随连接 TLS 动态设置。
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/console",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleSession 供前端探测登录态(guard 已保证通过才到此)。
func (a *Admin) handleSession(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
