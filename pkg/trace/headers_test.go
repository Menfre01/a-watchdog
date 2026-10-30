package trace

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSanitizeHeadersRedactsCredentials 覆盖凭据类头:值被替换,头名保留。
func TestSanitizeHeadersRedactsCredentials(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string][]string
	}{
		{"authorization", map[string][]string{"Authorization": {"Bearer sk-secret"}}},
		{"x-api-key", map[string][]string{"x-api-key": {"sk-ant-xyz"}}},
		{"apikey", map[string][]string{"ApiKey": {"plain"}}},
		{"cookie", map[string][]string{"Cookie": {"session=abc"}}},
		{"x-client-secret", map[string][]string{"X-Client-Secret": {"s3cr3t"}}},
		{"x-access-token", map[string][]string{"x-access-token": {"tok"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeHeaders(tc.headers)
			var decoded map[string][]string
			if err := json.Unmarshal([]byte(got), &decoded); err != nil {
				t.Fatalf("解析结果失败: %v(%s)", err, got)
			}
			for name, values := range decoded {
				if len(values) == 0 {
					t.Fatalf("%s 的值丢失", name)
				}
				for _, v := range values {
					if v != redactedValue {
						t.Errorf("%s 的值 = %q, want %q", name, v, redactedValue)
					}
				}
				if strings.Contains(got, "sk-secret") || strings.Contains(got, "s3cr3t") {
					t.Errorf("凭据原文泄漏: %s", got)
				}
			}
		})
	}
}

// TestSanitizeHeadersKeepsSessionLabels 覆盖标识头:原样保留(判定要用)。
func TestSanitizeHeadersKeepsSessionLabels(t *testing.T) {
	in := map[string][]string{
		"X-Claude-Code-Session-Id": {"7f3a-2b1c"},
		"Session-Id":               {"sess-123"},
		"Content-Type":             {"application/json"},
		"Host":                     {"localhost:8080"},
	}
	got := SanitizeHeaders(in)
	for _, want := range []string{"7f3a-2b1c", "sess-123", "application/json", "localhost:8080"} {
		if !strings.Contains(got, want) {
			t.Errorf("结果缺少 %q: %s", want, got)
		}
	}
}

// TestSanitizeHeadersKeepsMultiValues 覆盖多值头:值的个数保留。
func TestSanitizeHeadersKeepsMultiValues(t *testing.T) {
	in := map[string][]string{
		"X-Forwarded-For": {"10.0.0.1", "10.0.0.2"},
		"Authorization":   {"a", "b", "c"},
	}
	got := SanitizeHeaders(in)
	var decoded map[string][]string
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}
	if len(decoded["X-Forwarded-For"]) != 2 {
		t.Errorf("X-Forwarded-For 值数 = %d, want 2", len(decoded["X-Forwarded-For"]))
	}
	if len(decoded["Authorization"]) != 3 {
		t.Errorf("Authorization 值数 = %d, want 3", len(decoded["Authorization"]))
	}
}

// TestSanitizeHeadersIsCanonical 覆盖键序稳定:同一输入多次输出一致,且键按字典序。
func TestSanitizeHeadersIsCanonical(t *testing.T) {
	in := map[string][]string{
		"Zeta":  {"1"},
		"Alpha": {"2"},
		"Mid":   {"3"},
	}
	first := SanitizeHeaders(in)
	for i := 0; i < 5; i++ {
		if got := SanitizeHeaders(in); got != first {
			t.Fatalf("第 %d 次输出不一致:\n%s\n%s", i, got, first)
		}
	}
	if want := `{"Alpha":["2"],"Mid":["3"],"Zeta":["1"]}`; first != want {
		t.Errorf("输出 = %s, want %s", first, want)
	}
}

// TestSanitizeHeadersEmpty 覆盖空头集合。
func TestSanitizeHeadersEmpty(t *testing.T) {
	if got := SanitizeHeaders(nil); got != "{}" {
		t.Errorf("空输入 = %s, want {}", got)
	}
}

// TestSanitizeHeadersExtraCredentials 覆盖扩展的敏感片段:漏一个就是凭据落盘。
func TestSanitizeHeadersExtraCredentials(t *testing.T) {
	in := map[string][]string{
		"X-Signature":     {"sig-abc"},
		"X-Credential":    {"cred-value"},
		"X-Private-Key":   {"priv-value"},
		"X-User-Password": {"pw-value"},
	}
	got := SanitizeHeaders(in)
	for _, leaked := range []string{"sig-abc", "cred-value", "priv-value", "pw-value"} {
		if strings.Contains(got, leaked) {
			t.Errorf("凭据泄漏 %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, redactedValue) {
		t.Errorf("未产生占位符: %s", got)
	}
}

// TestSanitizeQuery 覆盖查询串脱敏:?access_token=... 这类写法同样不可再生。
func TestSanitizeQuery(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"beta=true", "beta=true"},
		{"access_token=SECRET", "access_token=" + redactedValue},
		{"api_key=abc&beta=true", "api_key=" + redactedValue + "&beta=true"},
		{"password=x&signature=y", "password=" + redactedValue + "&signature=" + redactedValue},
		{"flag", "flag"},
	}
	for _, tc := range cases {
		if got := SanitizeQuery(tc.in); got != tc.want {
			t.Errorf("SanitizeQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
