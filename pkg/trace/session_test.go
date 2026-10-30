package trace

import (
	"strings"
	"testing"
	"time"
)

// fixedNow 是测试用的固定时刻(UTC),用于匿名分桶断言。
var fixedNow = time.Date(2026, 10, 13, 9, 40, 0, 0, time.UTC)

// TestResolveSessionTable 覆盖判定主路径:显式标识、降级、匿名分桶与理由。
func TestResolveSessionTable(t *testing.T) {
	longID := strings.Repeat("a", sessionIDMaxLen+1)
	cases := []struct {
		name        string
		keyID       int64
		headers     map[string][]string
		wantSession string
		wantSource  string
		wantNoteHas []string
	}{
		{
			name:        "Claude Code 显式标识",
			keyID:       1,
			headers:     map[string][]string{"x-claude-code-session-id": {"7f3a-2b1c"}},
			wantSession: "1:7f3a-2b1c",
			wantSource:  "explicit",
			wantNoteHas: []string{"explicit:", "x-claude-code-session-id"},
		},
		{
			name:        "Codex thread-id",
			keyID:       2,
			headers:     map[string][]string{"thread-id": {"codex-88c"}},
			wantSession: "2:codex-88c",
			wantSource:  "explicit",
			wantNoteHas: []string{"explicit:", "thread-id"},
		},
		{
			name:        "下划线拼写",
			keyID:       2,
			headers:     map[string][]string{"session_id": {"snake_case_ok"}},
			wantSession: "2:snake_case_ok",
			wantSource:  "explicit",
			wantNoteHas: []string{"explicit:", "session_id"},
		},
		{
			name:        "头名大小写不敏感",
			keyID:       1,
			headers:     map[string][]string{"X-Claude-Code-Session-Id": {"Mixed-Case"}},
			wantSession: "1:Mixed-Case",
			wantSource:  "explicit",
			wantNoteHas: []string{"explicit:"},
		},
		{
			name:        "标识带首尾空白",
			keyID:       1,
			headers:     map[string][]string{"session-id": {"  padded  "}},
			wantSession: "1:padded",
			wantSource:  "explicit",
			wantNoteHas: []string{"explicit:"},
		},
		{
			name:        "无标识归入按天匿名",
			keyID:       3,
			headers:     map[string][]string{},
			wantSession: "3:anon-20261013",
			wantSource:  "anon",
			wantNoteHas: []string{"no identifier"},
		},
		{
			name:        "超长标识降级匿名",
			keyID:       1,
			headers:     map[string][]string{"session-id": {longID}},
			wantSession: "1:anon-20261013",
			wantSource:  "anon",
			wantNoteHas: []string{"degraded: invalid identifier", "no identifier"},
		},
		{
			name:        "非法字符降级匿名",
			keyID:       1,
			headers:     map[string][]string{"session-id": {"bad/../path"}},
			wantSession: "1:anon-20261013",
			wantSource:  "anon",
			wantNoteHas: []string{"degraded: invalid identifier"},
		},
		{
			name:        "空值视为无标识",
			keyID:       1,
			headers:     map[string][]string{"session-id": {""}},
			wantSession: "1:anon-20261013",
			wantSource:  "anon",
			wantNoteHas: []string{"no identifier"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveSession(tc.keyID, tc.headers, fixedNow)
			if got.SessionID != tc.wantSession {
				t.Errorf("SessionID = %q, want %q", got.SessionID, tc.wantSession)
			}
			if got.Source != tc.wantSource {
				t.Errorf("Source = %q, want %q", got.Source, tc.wantSource)
			}
			for _, want := range tc.wantNoteHas {
				if !strings.Contains(got.Note, want) {
					t.Errorf("Note = %q, 缺少 %q", got.Note, want)
				}
			}
		})
	}
}

// TestResolveSessionKeepsKeysApart 覆盖跨 key 不归并:同标识在不同 key 下独立成会话。
func TestResolveSessionKeepsKeysApart(t *testing.T) {
	headers := map[string][]string{"session-id": {"shared"}}
	a := ResolveSession(1, headers, fixedNow)
	b := ResolveSession(2, headers, fixedNow)
	if a.SessionID == b.SessionID {
		t.Fatalf("不同 key 归并到同一会话: %q", a.SessionID)
	}
	if a.SessionID != "1:shared" || b.SessionID != "2:shared" {
		t.Errorf("会话 id = %q / %q", a.SessionID, b.SessionID)
	}
}

// TestResolveSessionAnonBucketsByDay 覆盖按天分桶:同日相同、跨日不同。
func TestResolveSessionAnonBucketsByDay(t *testing.T) {
	sameDay := fixedNow.Add(6 * time.Hour)
	nextDay := fixedNow.Add(24 * time.Hour)
	a := ResolveSession(1, nil, fixedNow)
	b := ResolveSession(1, nil, sameDay)
	c := ResolveSession(1, nil, nextDay)
	if a.SessionID != b.SessionID {
		t.Errorf("同日分桶不同: %q vs %q", a.SessionID, b.SessionID)
	}
	if a.SessionID == c.SessionID {
		t.Errorf("跨日分桶相同: %q", a.SessionID)
	}
	if !strings.HasPrefix(a.SessionID, "1:anon-") {
		t.Errorf("匿名会话 id = %q", a.SessionID)
	}
}

// TestResolveSessionPrefersFirstValue 覆盖多值头:取首个值。
func TestResolveSessionPrefersFirstValue(t *testing.T) {
	got := ResolveSession(1, map[string][]string{"session-id": {"first", "second"}}, fixedNow)
	if got.SessionID != "1:first" {
		t.Errorf("SessionID = %q, want 1:first", got.SessionID)
	}
}

// TestResolveSessionFallsBackToNextHeader 覆盖降级链:首个标识非法时尝试后续头名。
func TestResolveSessionFallsBackToNextHeader(t *testing.T) {
	headers := map[string][]string{
		"x-claude-code-session-id": {strings.Repeat("x", sessionIDMaxLen+1)},
		"session-id":               {"valid-one"},
	}
	got := ResolveSession(1, headers, fixedNow)
	if got.SessionID != "1:valid-one" || got.Source != "explicit" {
		t.Errorf("未降级到后续头名: %+v", got)
	}
}

// TestNormalizeSessionID 覆盖规范化边界。
func TestNormalizeSessionID(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"abc-123_x.y", "abc-123_x.y", true},
		{"  trim me  ", "", false}, // trim 后仍含内部空格:不在白名单
		{"", "", false},
		{"  ", "", false},
		{strings.Repeat("a", sessionIDMaxLen), strings.Repeat("a", sessionIDMaxLen), true},
		{strings.Repeat("a", sessionIDMaxLen+1), "", false},
		{"bad:colon", "", false},
		{"bad/slash", "", false},
		{"中文标识", "", false},
	}
	for _, tc := range cases {
		got, ok := normalizeSessionID(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("normalizeSessionID(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
