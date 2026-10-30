package trace

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// msgs 构造测试用消息序列(键序不同的 map,用于验证 canonical 输出)。
func msgs() []MainlineMessage {
	return []MainlineMessage{
		{Role: "system", Blocks: []MainlineBlock{{"type": "text", "text": "sys"}}},
		{Role: "user", Blocks: []MainlineBlock{{"b": 2, "a": 1, "type": "text", "text": "hi"}}},
	}
}

// TestSerializeIsStable 覆盖序列化稳定性:同输入多次输出字节一致,且键序稳定。
func TestSerializeIsStable(t *testing.T) {
	first, err := Serialize(msgs())
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	for i := 0; i < 5; i++ {
		got, err := Serialize(msgs())
		if err != nil {
			t.Fatalf("第 %d 次 Serialize() error = %v", i, err)
		}
		if !bytes.Equal(got.NDJSON, first.NDJSON) {
			t.Fatalf("第 %d 次输出不一致:\n%s\n%s", i, got.NDJSON, first.NDJSON)
		}
		if !bytes.Equal(got.LineHashes, first.LineHashes) || !bytes.Equal(got.LineSizes, first.LineSizes) {
			t.Fatalf("第 %d 次派生列不一致", i)
		}
	}
	// 键按字典序排列。
	if !strings.Contains(string(first.NDJSON), `"a":1,"b":2`) {
		t.Errorf("键序不稳定: %s", first.NDJSON)
	}
}

// TestSerializeDerivedColumns 覆盖派生列:行数、指纹长度、行长度一一对应。
func TestSerializeDerivedColumns(t *testing.T) {
	snap, err := Serialize(msgs())
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	if snap.Lines != 2 {
		t.Fatalf("Lines = %d, want 2", snap.Lines)
	}
	if len(snap.LineHashes) != 2*lineHashBytes {
		t.Errorf("指纹序列长度 = %d, want %d", len(snap.LineHashes), 2*lineHashBytes)
	}
	lines := ParseLines(snap.NDJSON)
	if len(lines) != snap.Lines {
		t.Fatalf("解析行数 = %d, want %d", len(lines), snap.Lines)
	}
	for i, line := range lines {
		if got := LineSize(snap.LineSizes, i); got != len(line) {
			t.Errorf("第 %d 行长度 = %d, want %d", i, got, len(line))
		}
	}
	// 每行都以换行结尾(含最后一行):切分规则与序列化共用。
	if !bytes.HasSuffix(snap.NDJSON, []byte("\n")) {
		t.Errorf("ndjson 末尾缺少换行: %q", snap.NDJSON)
	}
}

// TestSerializeSingleLineChangeIsolates 覆盖单行改动:只有该行的指纹与长度变化。
func TestSerializeSingleLineChangeIsolates(t *testing.T) {
	base, err := Serialize(msgs())
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	changed := msgs()
	changed[1].Blocks[0]["text"] = "hi!" // 只改第二行
	got, err := Serialize(changed)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	if bytes.Equal(LineHash(base.LineHashes, 0), LineHash(got.LineHashes, 0)) == false {
		t.Errorf("未改动的第 0 行指纹发生变化")
	}
	if bytes.Equal(LineHash(base.LineHashes, 1), LineHash(got.LineHashes, 1)) {
		t.Errorf("改动的第 1 行指纹未变化")
	}
	// 公共前缀应为 1。
	if n := CommonPrefixLen(base.LineHashes, got.LineHashes); n != 1 {
		t.Errorf("公共前缀 = %d, want 1", n)
	}
}

// TestCommonPrefixLen 覆盖前缀比对的边界。
func TestCommonPrefixLen(t *testing.T) {
	a, _ := Serialize(msgs())
	same, _ := Serialize(msgs())
	if n := CommonPrefixLen(a.LineHashes, same.LineHashes); n != 2 {
		t.Errorf("相同序列前缀 = %d, want 2", n)
	}
	one, _ := Serialize(msgs()[:1])
	if n := CommonPrefixLen(one.LineHashes, a.LineHashes); n != 1 {
		t.Errorf("前缀序列 vs 全序列 = %d, want 1", n)
	}
	if n := CommonPrefixLen(nil, a.LineHashes); n != 0 {
		t.Errorf("空前缀 = %d, want 0", n)
	}
}

// TestSerializeNumberPrecision 覆盖数值保真:大整数经序列化不失真。
func TestSerializeNumberPrecision(t *testing.T) {
	const big = "9007199254740993" // 2^53+1:float64 无法精确表示
	body := `{"messages":[{"role":"user","content":[{"type":"tool_use","id":"t","name":"f","input":{"n":` + big + `}}]}]}`
	parsed, err := Normalize("anthropic-messages", []byte(body))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	snap, err := Serialize(parsed)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	if !bytes.Contains(snap.NDJSON, []byte(big)) {
		t.Errorf("大整数失真: %s", snap.NDJSON)
	}
}

// TestParseLinesIgnoresTrailingEmpty 覆盖尾随空行被忽略。
func TestParseLinesIgnoresTrailingEmpty(t *testing.T) {
	got := ParseLines([]byte("a\nb\n\n\n"))
	if len(got) != 2 || string(got[0]) != "a" || string(got[1]) != "b" {
		t.Errorf("ParseLines = %q, want [a b]", got)
	}
	if lines := ParseLines(nil); len(lines) != 0 {
		t.Errorf("空输入解析出 %d 行", len(lines))
	}
}

// TestSerializeEmpty 覆盖空消息序列。
func TestSerializeEmpty(t *testing.T) {
	snap, err := Serialize(nil)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	if snap.Lines != 0 || len(snap.NDJSON) != 0 || len(snap.LineHashes) != 0 {
		t.Errorf("空序列化 = %+v", snap)
	}
}

// TestLineHashAndSizeBounds 覆盖越界访问。
func TestLineHashAndSizeBounds(t *testing.T) {
	snap, _ := Serialize(msgs())
	if LineHash(snap.LineHashes, -1) != nil || LineHash(snap.LineHashes, 2) != nil {
		t.Error("LineHash 越界未返回 nil")
	}
	if LineSize(snap.LineSizes, 2) != -1 {
		t.Error("LineSize 越界未返回 -1")
	}
}

// TestSerializeKeepsJSONNumber 覆盖 json.Number 的序列化形态(不带引号)。
func TestSerializeKeepsJSONNumber(t *testing.T) {
	parsed, err := Normalize("openai-chat", []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c","function":{"name":"f","arguments":"{\"n\":42}"}}]}]}`))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	snap, err := Serialize(parsed)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	var decoded struct {
		Blocks []map[string]any `json:"blocks"`
	}
	if err := json.Unmarshal(ParseLines(snap.NDJSON)[0], &decoded); err != nil {
		t.Fatalf("解析输出失败: %v", err)
	}
	if _, ok := decoded.Blocks[0]["input"].(map[string]any); !ok {
		t.Errorf("input 未序列化为对象: %#v", decoded.Blocks[0]["input"])
	}
}

// TestSerializeIgnoresVolatileAnnotations 覆盖易变注解不参与指纹与行长度:
// 客户端的 cache_control 标记每轮滚动,若参与比对会让每一步都退化为分叉。
func TestSerializeIgnoresVolatileAnnotations(t *testing.T) {
	withMark := []MainlineMessage{
		{Role: "system", Blocks: []MainlineBlock{
			{"type": "text", "text": "sys", "cache_control": map[string]any{"type": "ephemeral"}},
		}},
		{Role: "user", Blocks: []MainlineBlock{{"type": "text", "text": "hi"}}},
	}
	without := []MainlineMessage{
		{Role: "system", Blocks: []MainlineBlock{{"type": "text", "text": "sys"}}},
		{Role: "user", Blocks: []MainlineBlock{{"type": "text", "text": "hi"}}},
	}
	marked, err := Serialize(withMark)
	if err != nil {
		t.Fatalf("Serialize(带注解) error = %v", err)
	}
	plain, err := Serialize(without)
	if err != nil {
		t.Fatalf("Serialize(无注解) error = %v", err)
	}

	if !bytes.Equal(marked.LineHashes, plain.LineHashes) {
		t.Error("注解字段变化影响了指纹")
	}
	if !bytes.Equal(marked.LineSizes, plain.LineSizes) {
		t.Error("注解字段变化影响了行长度")
	}
	if n := CommonPrefixLen(marked.LineHashes, plain.LineHashes); n != 2 {
		t.Errorf("公共前缀 = %d, want 2", n)
	}
	// 快照本体仍保留注解(保真优先)。
	if !bytes.Contains(marked.NDJSON, []byte("cache_control")) {
		t.Error("注解字段应从快照本体中保留")
	}
}
