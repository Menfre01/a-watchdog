package trace

import (
	"testing"
)

// TestBlobBlockFromAnthropicBranches 覆盖 shape 异常:原样保留而不是产出空引用。
func TestBlobBlockFromAnthropicBranches(t *testing.T) {
	cases := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{"缺 source", map[string]any{"type": "image"}, "image"},
		{"source 非对象", map[string]any{"type": "image", "source": "oops"}, "image"},
		{"source 空对象", map[string]any{"type": "document", "source": map[string]any{}}, "document"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := blobBlockFromAnthropic(tc.obj)
			if got["type"] != tc.want {
				t.Errorf("类型 = %v, want %v(原样保留)", got["type"], tc.want)
			}
		})
	}
}

// TestNormalizePropagatesBlockErrors 覆盖块级解析错误:向上报错而非静默跳过。
func TestNormalizePropagatesBlockErrors(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		body     string
	}{
		{"anthropic 块非对象", "anthropic-messages", `{"messages":[{"role":"user","content":["not-an-object"]}]}`},
		{"anthropic system 形状非法", "anthropic-messages", `{"system":123,"messages":[]}`},
		{"openai 块非对象", "openai-chat", `{"messages":[{"role":"user","content":["not-an-object"]}]}`},
		{"openai content 形状非法", "openai-chat", `{"messages":[{"role":"user","content":42}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Normalize(tc.protocol, []byte(tc.body)); err == nil {
				t.Error("非法输入未报错")
			}
		})
	}
}

// TestCollectToolResultsKeepsEarliest 覆盖同标识多次出现:保留最早的一份。
func TestCollectToolResultsKeepsEarliest(t *testing.T) {
	steps := []StepMessages{
		{Seq: 1, Messages: []MainlineMessage{assistantCalls(use("t1", "Read", "{}"))}},
		{Seq: 2, Messages: []MainlineMessage{userResults(result("t1", "first", false))}},
		{Seq: 3, Messages: []MainlineMessage{userResults(result("t1", "second", true))}},
	}
	calls := ExtractTools(steps)
	if len(calls) != 1 {
		t.Fatalf("调用数 = %d, want 1", len(calls))
	}
	if calls[0].ResultSeq != 2 || calls[0].IsError {
		t.Errorf("应保留最早的结果: %+v", calls[0])
	}
}

// TestSanitizeQueryKeepsNonSensitive 覆盖无敏感参数时原样返回。
func TestSanitizeQueryKeepsNonSensitive(t *testing.T) {
	const raw = "beta=true&limit=10"
	if got := SanitizeQuery(raw); got != raw {
		t.Errorf("SanitizeQuery(%q) = %q, want 原样", raw, got)
	}
}

// TestGroupTurnsLeadingInjectedInput 覆盖首步即注入内容:仍开启首个轮次。
func TestGroupTurnsLeadingInjectedInput(t *testing.T) {
	steps := []StepForTurn{
		{Seq: 1, Delta: Delta{Kind: DeltaNew}, Added: []MainlineMessage{
			textMsg("user", "<system-reminder>init</system-reminder>"),
		}},
		{Seq: 2, Delta: Delta{Kind: DeltaAdded}, Added: []MainlineMessage{resultMsg("ok")}},
	}
	turns := GroupTurns(steps)
	if len(turns) != 1 || len(turns[0].StepSeqs) != 2 {
		t.Fatalf("分组 = %+v, want 单轮两步", turns)
	}
}

// TestSplitDataURLNoComma 覆盖缺少分隔符的畸形 data URL。
func TestSplitDataURLNoComma(t *testing.T) {
	if _, _, ok := splitDataURL("data:image/png;base64"); ok {
		t.Error("缺少逗号的 data URL 不应命中")
	}
}
