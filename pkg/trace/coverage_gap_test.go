package trace

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDeltaNoteRemainingBranches 覆盖 Note 的剩余分支:
// 重试组序号、多行替换、末行更新的 KB / MB 量级。
func TestDeltaNoteRemainingBranches(t *testing.T) {
	cases := []struct {
		name string
		d    Delta
		want string
	}{
		{
			"重试组序号",
			Delta{Kind: DeltaRetry, RetryN: 2, RetryOf: 3},
			"第 2/3 次相同请求",
		},
		{
			"多行替换",
			Delta{Kind: DeltaFork, PrefixLen: 1, Removed: 2, Added: 3},
			"2 行被替换为 3 行",
		},
		{
			"末行扩到 KB",
			Delta{Kind: DeltaFork, PrefixLen: 1, TailUpdate: true, TailBefore: 1024, TailAfter: 1536},
			"从 1.0KB 扩到 1.5KB",
		},
		{
			"末行缩到 MB",
			Delta{Kind: DeltaFork, PrefixLen: 1, TailUpdate: true, TailBefore: 2 << 20, TailAfter: 1 << 20},
			"从 2.0MB 缩到 1.0MB",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.d.Note(); !strings.Contains(got, tc.want) {
				t.Errorf("Note() = %q, 缺少 %q", got, tc.want)
			}
		})
	}
}

// TestHumanBytes 覆盖字节量级的三个区间。
func TestHumanBytes(t *testing.T) {
	cases := map[int]string{
		512:     "512B",
		1024:    "1.0KB",
		1536:    "1.5KB",
		1 << 20: "1.0MB",
		2 << 20: "2.0MB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestDiffHunksInsertBeforeDelete 覆盖「先插入、后删除」的回溯顺序:
// 合并逻辑要把它对齐成替换块,而不是两个独立块。
func TestDiffHunksInsertBeforeDelete(t *testing.T) {
	got := diffOf(t, []string{"B", "C"}, []string{"A", "B"})
	if len(got) != 1 || got[0].Kind != DiffReplace {
		t.Fatalf("hunks = %s, want 单块 replace", hunkShape(got))
	}
}

// TestDecompressCorruptBody 覆盖解压读数失败:头部合法但校验和损坏。
func TestDecompressCorruptBody(t *testing.T) {
	compressed, err := Compress([]byte(strings.Repeat("payload-", 100)))
	if err != nil {
		t.Fatalf("Compress() error = %v", err)
	}
	corrupt := append([]byte(nil), compressed...)
	corrupt[len(corrupt)-5] ^= 0xff // 破坏 CRC32,读取时应报错
	if _, err := Decompress(corrupt); err == nil {
		t.Fatal("Decompress(损坏数据) error = nil, want error")
	}
}

// TestAnthropicBlocksStringError 覆盖直接传入非法字符串形态:
// Normalize 路径上不可能出现(外层已解析成功),此处守护内部契约。
func TestAnthropicBlocksStringError(t *testing.T) {
	if _, err := anthropicBlocks(json.RawMessage(`"unterminated`)); err == nil {
		t.Fatal("anthropicBlocks(非法字符串) error = nil")
	}
}

// TestOpenAIContentBlocksStringError 覆盖同样的内部契约。
func TestOpenAIContentBlocksStringError(t *testing.T) {
	if _, err := openaiContentBlocks(json.RawMessage(`"unterminated`)); err == nil {
		t.Fatal("openaiContentBlocks(非法字符串) error = nil")
	}
}

// TestBlobBlockFromAnthropicKeepsURL 覆盖 Anthropic 的 URL 形态:
// 远程图片保留地址,不产出空引用。
func TestBlobBlockFromAnthropicKeepsURL(t *testing.T) {
	got := blobBlockFromAnthropic(map[string]any{
		"type":   "image",
		"source": map[string]any{"type": "url", "url": "https://example.com/a.png"},
	})
	if got["type"] != "blob" || got["url"] != "https://example.com/a.png" {
		t.Fatalf("URL 形态 = %+v", got)
	}
}

// TestNormalizeOpenAIErrors 覆盖 OpenAI 侧的两个错误分支:
// 请求体非法,以及 role=tool 的内容形状非法。
func TestNormalizeOpenAIErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"请求体非法", `not-json`},
		{"工具结果内容非法", `{"messages":[{"role":"tool","content":42}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Normalize("openai-chat", []byte(tc.body)); err == nil {
				t.Fatal("非法输入未报错")
			}
		})
	}
}

// TestSerializeMarshalError 覆盖序列化失败:块内含不可编码的值。
func TestSerializeMarshalError(t *testing.T) {
	_, err := Serialize([]MainlineMessage{{
		Role:   "user",
		Blocks: []MainlineBlock{{"type": "text", "text": make(chan int)}},
	}})
	if err == nil {
		t.Fatal("Serialize(不可编码块) error = nil")
	}
}

// TestSystemFingerprintSkipsNonSystem 覆盖跳过非 system 消息:
// 指纹只取首条 system,与前面出现多少条其他消息无关。
func TestSystemFingerprintSkipsNonSystem(t *testing.T) {
	system := sysMsg("You are Claude Code")
	want := SystemFingerprint([]MainlineMessage{system})
	got := SystemFingerprint([]MainlineMessage{
		textMsg("user", "hi"),
		textMsg("assistant", "hello"),
		system,
	})
	if want == "" || got != want {
		t.Fatalf("跳过非 system 后指纹 = %q, want %q", got, want)
	}
}

// TestSystemFingerprintIgnoresCacheControl 覆盖块级注解不参与指纹。
func TestSystemFingerprintIgnoresCacheControl(t *testing.T) {
	marked := SystemFingerprint([]MainlineMessage{{
		Role: "system",
		Blocks: []MainlineBlock{{
			"type": "text", "text": "sys",
			"cache_control": map[string]any{"type": "ephemeral"},
		}},
	}})
	plain := SystemFingerprint([]MainlineMessage{sysMsg("sys")})
	if marked == "" || marked != plain {
		t.Fatalf("注解影响指纹: %q vs %q", marked, plain)
	}
}

// TestSystemFingerprintMarshalError 覆盖编码失败:返回空串,调用方退回前缀判据。
func TestSystemFingerprintMarshalError(t *testing.T) {
	got := SystemFingerprint([]MainlineMessage{{
		Role:   "system",
		Blocks: []MainlineBlock{{"type": "text", "text": make(chan int)}},
	}})
	if got != "" {
		t.Fatalf("编码失败时指纹 = %q, want 空", got)
	}
}

// TestSystemFingerprintNoSystem 覆盖无 system 消息:返回空串。
func TestSystemFingerprintNoSystem(t *testing.T) {
	if got := SystemFingerprint(nil); got != "" {
		t.Errorf("空消息 = %q, want 空", got)
	}
	if got := SystemFingerprint([]MainlineMessage{textMsg("user", "hi")}); got != "" {
		t.Errorf("无 system = %q, want 空", got)
	}
}

// TestStripVolatileLines 覆盖配额计数行的剔除:仅整行以标签开头者被丢弃。
func TestStripVolatileLines(t *testing.T) {
	in := "keep-a\n<total_tokens>100 tokens left</total_tokens>\n" +
		"  <total_tokens>99 tokens left</total_tokens>\nkeep-b"
	want := "keep-a\nkeep-b"
	if got := stripVolatileLines(in); got != want {
		t.Errorf("stripVolatileLines() = %q, want %q", got, want)
	}
	const plain = "no volatile lines here"
	if got := stripVolatileLines(plain); got != plain {
		t.Errorf("无计数行时 = %q, want 原样", got)
	}
}

// TestStartsNewTurnSide 覆盖旁路步不开新轮的判定。
func TestStartsNewTurnSide(t *testing.T) {
	if startsNewTurn(StepForTurn{Side: true, Delta: Delta{Kind: DeltaChain}}) {
		t.Error("旁路步不应开启新轮次")
	}
}
