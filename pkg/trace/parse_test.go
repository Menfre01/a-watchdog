package trace

import (
	"encoding/json"
	"strings"
	"testing"
)

// parsedThree 构造一个三行快照(含未知块与 tool 块)用于解析测试。
func parsedThree(t *testing.T) []byte {
	t.Helper()
	msgs := []MainlineMessage{
		{Role: "system", Blocks: []MainlineBlock{{"type": "text", "text": "sys"}}},
		{Role: "user", Blocks: []MainlineBlock{
			{"type": "text", "text": "hi"},
			{"type": "custom_thing", "payload": map[string]any{"a": json.Number("1")}},
		}},
		{Role: "assistant", Blocks: []MainlineBlock{
			{"type": "tool_use", "id": "t1", "name": "calc", "input": map[string]any{"n": json.Number("9007199254740993")}},
		}},
	}
	snap, err := Serialize(msgs)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	return snap.NDJSON
}

// TestParseMessagesRoundTrip 覆盖整段解析:角色、块类型与未知块都还原。
func TestParseMessagesRoundTrip(t *testing.T) {
	ndjson := parsedThree(t)
	msgs, err := ParseMessages(ndjson)
	if err != nil {
		t.Fatalf("ParseMessages() error = %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("消息数 = %d, want 3", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[2].Role != "assistant" {
		t.Errorf("角色 = %q / %q", msgs[0].Role, msgs[2].Role)
	}
	// 未知块原样保留。
	unknown := msgs[1].Blocks[1]
	if unknown["type"] != "custom_thing" {
		t.Errorf("未知块类型 = %v", unknown["type"])
	}
	if _, ok := unknown["payload"]; !ok {
		t.Errorf("未知块载荷丢失: %+v", unknown)
	}
}

// TestParseMessagesKeepsNumberPrecision 覆盖数值保真:大整数解析后仍是 json.Number。
func TestParseMessagesKeepsNumberPrecision(t *testing.T) {
	ndjson := parsedThree(t)
	msgs, err := ParseMessages(ndjson)
	if err != nil {
		t.Fatalf("ParseMessages() error = %v", err)
	}
	input, ok := msgs[2].Blocks[0]["input"].(map[string]any)
	if !ok {
		t.Fatalf("input 类型 = %T", msgs[2].Blocks[0]["input"])
	}
	n, ok := input["n"].(json.Number)
	if !ok {
		t.Fatalf("n 类型 = %T, want json.Number", input["n"])
	}
	if n.String() != "9007199254740993" {
		t.Errorf("n = %s, 精度失真", n)
	}
}

// TestParseMessagesRange 覆盖按行区间解析:只解析指定行,越界收敛。
func TestParseMessagesRange(t *testing.T) {
	ndjson := parsedThree(t)
	cases := []struct {
		name       string
		start, end int
		wantRoles  []string
	}{
		{"单行", 1, 2, []string{"user"}},
		{"尾部两行", 1, 3, []string{"user", "assistant"}},
		{"整段", 0, 3, []string{"system", "user", "assistant"}},
		{"空区间", 2, 2, nil},
		{"起点越界收敛", 2, 99, []string{"assistant"}},
		{"负起点收敛", -5, 1, []string{"system"}},
		{"全越界", 9, 12, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseMessagesRange(ndjson, tc.start, tc.end)
			if err != nil {
				t.Fatalf("ParseMessagesRange() error = %v", err)
			}
			roles := make([]string, 0, len(got))
			for _, m := range got {
				roles = append(roles, m.Role)
			}
			if strings.Join(roles, ",") != strings.Join(tc.wantRoles, ",") {
				t.Errorf("角色 = %v, want %v", roles, tc.wantRoles)
			}
		})
	}
}

// TestParseMessagesInvalidLine 覆盖非法行:报错并指出行号。
func TestParseMessagesInvalidLine(t *testing.T) {
	_, err := ParseMessages([]byte("{\"role\":\"user\"}\nnot-json\n"))
	if err == nil {
		t.Fatal("非法行未报错")
	}
	if !strings.Contains(err.Error(), "第 1 行") {
		t.Errorf("错误未指出行号: %v", err)
	}
}

// TestParseMessagesEmpty 覆盖空输入。
func TestParseMessagesEmpty(t *testing.T) {
	msgs, err := ParseMessages(nil)
	if err != nil || len(msgs) != 0 {
		t.Errorf("空输入 = %v, %v", msgs, err)
	}
}
