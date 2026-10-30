// Package trace 实现轨迹捕捉的纯函数部分:请求头脱敏、消息规范化、会话判定与
// 增量推断。这些函数不接触数据库与 HTTP 服务端,便于单测、重放与规则迭代。
package trace

import (
	"encoding/json"
	"strings"
)

// redactedValue 是凭据类请求头的脱敏占位值。
const redactedValue = "[redacted]"

// sensitiveNames 是凭据类的匹配片段(不区分大小写,子串匹配):命中者的值被替换为
// 占位符,名称与出现次数保留,便于回溯「客户端发过哪些头/参数」。
// 片段取宽:漏一个(如 X-Signature、X-Credential、password)就等于凭据原文落盘。
var sensitiveNames = []string{
	"authorization",
	"api-key",
	"apikey",
	"cookie",
	"secret",
	"token",
	"signature",
	"credential",
	"password",
	"private-key",
}

// isSensitiveName 判断头名或参数名是否属于凭据类。
// 下划线与连字符先归一:api_key / x-api-key 是同一类参数。
func isSensitiveName(name string) bool {
	normalized := strings.NewReplacer("_", "-").Replace(strings.ToLower(name))
	for _, key := range sensitiveNames {
		if strings.Contains(normalized, key) {
			return true
		}
	}
	return false
}

// SanitizeHeaders 把入向请求头序列化为 canonical JSON(键按字典序排列),
// 凭据类头的值替换为占位符。结果直接落原始层,供判定回溯与人工排查。
// Host 不在 http.Header 中,由调用方从 http.Request.Host 补入。
func SanitizeHeaders(h map[string][]string) string {
	out := make(map[string][]string, len(h))
	for name, values := range h {
		if !isSensitiveName(name) {
			out[name] = values
			continue
		}
		masked := make([]string, len(values))
		for i := range masked {
			masked[i] = redactedValue
		}
		out[name] = masked
	}
	// encoding/json 对 map 按键排序输出,天然 canonical;
	// 值类型为 []string,编码不可能失败,故错误分支仅作防御。
	encoded, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// SanitizeQuery 脱敏查询串里的凭据类参数:键名命中敏感片段时值替换为占位符。
// 查询串同样不可再生,而 ?access_token=... 这类写法很常见,不能只脱敏头。
func SanitizeQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	changed := false
	for i, part := range parts {
		key, _, ok := strings.Cut(part, "=")
		if !ok || !isSensitiveName(key) {
			continue
		}
		parts[i] = key + "=" + redactedValue
		changed = true
	}
	if !changed {
		return raw
	}
	return strings.Join(parts, "&")
}
