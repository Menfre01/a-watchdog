package trace

import (
	"strings"
	"testing"
)

// TestSnapshotLinesOf 覆盖从落库派生列还原比对输入(视图层走的就是这条路径)。
func TestSnapshotLinesOf(t *testing.T) {
	snap, err := Serialize(msgs())
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	got := SnapshotLinesOf(snap.LineHashes, snap.LineSizes)
	if got.Lines != snap.Lines || len(got.LineHashes) != len(snap.LineHashes) || len(got.LineSizes) != len(snap.LineSizes) {
		t.Errorf("还原结果 = %+v, want %+v", got, snap.LinesInfo())
	}
	if empty := SnapshotLinesOf(nil, nil); empty.Lines != 0 {
		t.Errorf("空输入 = %+v", empty)
	}
}

// TestDeltaNote 覆盖各分类的判定理由:如实说明依据,不确定时明说。
func TestDeltaNote(t *testing.T) {
	cases := []struct {
		name string
		d    Delta
		want string
	}{
		{"首步", Delta{Kind: DeltaNew}, "无前序"},
		{"重复", Delta{Kind: DeltaRetry}, "完全相同"},
		{"追加", Delta{Kind: DeltaAdded, Kept: 2, Added: 1}, "前缀延伸"},
		{"分叉", Delta{Kind: DeltaFork, PrefixLen: 2}, "无法进一步区分"},
		{"摘要压缩", Delta{Kind: DeltaRewrite, Summarized: true, SharedSuffix: 3}, "摘要压缩"},
		{"历史替换", Delta{Kind: DeltaRewrite, SharedSuffix: 3}, "历史被替换"},
		{"上下文清零", Delta{Kind: DeltaChain, Cleared: true}, "上下文清零"},
		{"无重合", Delta{Kind: DeltaChain}, "与同链上一步无共同前缀也无共享尾部 → 无重合"},
		{"未知分类", Delta{Kind: DeltaKind("unknown")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.d.Note()
			if tc.want == "" {
				if got != "" {
					t.Errorf("Note() = %q, want 空", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("Note() = %q, 缺少 %q", got, tc.want)
			}
		})
	}
}

// TestSplitDataURL 覆盖 data URL 拆解的各分支。
func TestSplitDataURL(t *testing.T) {
	if _, _, ok := splitDataURL("https://example.com/a.png"); ok {
		t.Error("普通 URL 不应被当作 data URL")
	}
	if _, _, ok := splitDataURL("data:image/png,plain"); ok {
		t.Error("非 base64 的 data URL 不应命中")
	}
	payload, mediaType, ok := splitDataURL("data:image/png;base64,AAAA")
	if !ok || payload != "AAAA" || mediaType != "image/png" {
		t.Errorf("拆解 = %q/%q/%v", payload, mediaType, ok)
	}
}

// TestByteRefBlockInvalidBase64 覆盖非法 base64:只保留长度描述,不保留载荷。
func TestByteRefBlockInvalidBase64(t *testing.T) {
	block := byteRefBlock("!!!not-base64!!!", "image/png")
	if block["bytes"] != -1 {
		t.Errorf("非法载荷的 bytes = %v, want -1", block["bytes"])
	}
	if _, leaked := block["hash"]; leaked {
		t.Errorf("非法载荷不应有哈希: %+v", block)
	}
	if block["media_type"] != "image/png" {
		t.Errorf("媒体类型丢失: %+v", block)
	}
}

// TestDecodeArgumentsEmpty 覆盖空入参:解析为空对象。
func TestDecodeArgumentsEmpty(t *testing.T) {
	got, ok := decodeArguments("   ").(map[string]any)
	if !ok || len(got) != 0 {
		t.Errorf("空入参 = %#v, want 空对象", decodeArguments("   "))
	}
}

// TestOpenAIPartBlockBranches 覆盖 part 归一的边界分支。
func TestOpenAIPartBlockBranches(t *testing.T) {
	// 非 image_url 的 part 原样保留。
	text := openaiPartBlock(map[string]any{"type": "text", "text": "hi"})
	if text["type"] != "text" {
		t.Errorf("文本 part = %+v", text)
	}
	// image_url 缺 url 时原样保留,不产出空引用。
	broken := openaiPartBlock(map[string]any{"type": "image_url", "image_url": map[string]any{}})
	if broken["type"] != "image_url" {
		t.Errorf("缺 url 的 part = %+v", broken)
	}
}

// TestFirstHeaderValueBranches 覆盖头取值的空路径。
func TestFirstHeaderValueBranches(t *testing.T) {
	if got := firstHeaderValue(nil, "x-session"); got != "" {
		t.Errorf("空头集合 = %q", got)
	}
	if got := firstHeaderValue(map[string][]string{"X-Session": {}}, "x-session"); got != "" {
		t.Errorf("空值列表 = %q", got)
	}
	if got := firstHeaderValue(map[string][]string{"X-Session": {"v"}}, "x-session"); got != "v" {
		t.Errorf("取值 = %q", got)
	}
}

// TestCollectToolResultsIgnoresEmptyID 覆盖缺标识的结果条目被忽略。
func TestCollectToolResultsIgnoresEmptyID(t *testing.T) {
	steps := []StepMessages{{Seq: 1, Messages: []MainlineMessage{
		userResults(MainlineBlock{"type": "tool_result", "tool_use_id": "", "content": "x"}),
	}}}
	if got := ExtractTools(steps); len(got) != 0 {
		t.Errorf("缺标识的结果不应产出调用: %v", got)
	}
}

// TestNormalizeAnthropicSystemShapes 覆盖 system 的两种形态(字符串与块数组)。
func TestNormalizeAnthropicSystemShapes(t *testing.T) {
	for _, body := range []string{
		`{"system":"sys","messages":[]}`,
		`{"system":[{"type":"text","text":"sys"}],"messages":[]}`,
	} {
		msgs, err := Normalize("anthropic-messages", []byte(body))
		if err != nil {
			t.Fatalf("Normalize(%s) error = %v", body, err)
		}
		if len(msgs) != 1 || msgs[0].Role != "system" {
			t.Errorf("system 归一 = %+v", msgs)
		}
	}
	// null system 不产出伪消息。
	msgs, err := Normalize("anthropic-messages", []byte(`{"system":null,"messages":[]}`))
	if err != nil || len(msgs) != 0 {
		t.Errorf("null system = %+v, %v", msgs, err)
	}
}

// TestOpenAIContentNullAndEmpty 覆盖 content 为 null 与空串:不产出空块。
func TestOpenAIContentNullAndEmpty(t *testing.T) {
	msgs, err := Normalize("openai-chat", []byte(`{"messages":[{"role":"assistant","content":null},{"role":"assistant","content":""}]}`))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	for i, msg := range msgs {
		if len(msg.Blocks) != 0 {
			t.Errorf("第 %d 条消息产出空文本块: %+v", i, msg.Blocks)
		}
	}
}

// TestAnthropicContentInvalidShape 覆盖 content 形状非法:如实报错。
func TestAnthropicContentInvalidShape(t *testing.T) {
	if _, err := Normalize("anthropic-messages", []byte(`{"messages":[{"role":"user","content":123}]}`)); err == nil {
		t.Error("非法 content 形状未报错")
	}
}
