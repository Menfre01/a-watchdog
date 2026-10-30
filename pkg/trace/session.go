package trace

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// sessionIDMaxLen 是标识串的长度上限:超出的值不采信(design.md D4 的安全边界)。
const sessionIDMaxLen = 128

// sessionHeaderNames 是会话标识头(按优先级)。前一个是 Claude Code 的,
// 后四个覆盖 Codex 的两种拼写与写法。
var sessionHeaderNames = []string{
	"x-claude-code-session-id",
	"session-id",
	"session_id",
	"thread-id",
	"thread_id",
}

// SessionDecision 是一次会话判定的结果。
type SessionDecision struct {
	SessionID string // <key_id>:<标识串>,或 <key_id>:anon-<YYYYMMDD>
	Source    string // "explicit" | "anon"
	Note      string // 判定理由(命中头名 / 降级事件 / 匿名兜底)
}

// ResolveSession 判定会话归属:显式标识优先;未携带或标识不合规时归入按天匿名会话。
// now 用于匿名分桶(UTC 日期),判定只依赖请求头,与消息内容无关(design.md D4)。
func ResolveSession(keyID int64, headers map[string][]string, now time.Time) SessionDecision {
	prefix := strconv.FormatInt(keyID, 10) + ":"
	degraded := ""
	for _, name := range sessionHeaderNames {
		raw := firstHeaderValue(headers, name)
		if raw == "" {
			continue
		}
		if id, ok := normalizeSessionID(raw); ok {
			return SessionDecision{
				SessionID: prefix + id,
				Source:    "explicit",
				Note:      "explicit: " + name,
			}
		}
		// 标识不合规:记录首个降级事件,继续尝试其余头名。
		if degraded == "" {
			degraded = fmt.Sprintf("degraded: invalid identifier (len %d)", len(strings.TrimSpace(raw)))
		}
	}
	return SessionDecision{
		SessionID: prefix + "anon-" + now.UTC().Format("20060102"),
		Source:    "anon",
		Note:      anonNote(degraded),
	}
}

// anonNote 组装匿名会话的判定理由:有降级事件时前置,便于误判排查。
func anonNote(degraded string) string {
	const base = "no identifier (anon by date)"
	if degraded == "" {
		return base
	}
	return degraded + "; " + base
}

// normalizeSessionID 规范化标识串:去掉首尾空白后校验长度与字符集。
// 字符集限定为 [A-Za-z0-9._-],由此 session_id 可安全进入 URL 路径与前端渲染。
func normalizeSessionID(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > sessionIDMaxLen {
		return "", false
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			return "", false
		}
	}
	return trimmed, true
}

// firstHeaderValue 取头名的首个值(大小写不敏感);缺失返回空串。
func firstHeaderValue(headers map[string][]string, name string) string {
	for key, values := range headers {
		if !strings.EqualFold(key, name) {
			continue
		}
		if len(values) > 0 {
			return values[0]
		}
		return ""
	}
	return ""
}
