package admin

import (
	"testing"

	"awatchdog/pkg/config/db"
	"awatchdog/pkg/trace"
)

// tgz 压缩测试载荷。
func tgz(t *testing.T, raw string) []byte {
	t.Helper()
	gz, err := trace.Compress([]byte(raw))
	if err != nil {
		t.Fatalf("Compress() error = %v", err)
	}
	return gz
}

// TestDecodePayload 覆盖载荷解压与解析:合法 JSON、非法 JSON、截断与损坏。
func TestDecodePayload(t *testing.T) {
	if got := decodePayload(tgz(t, `{"x":1}`), false); got == nil {
		t.Error("合法 JSON 应解析为对象")
	}
	if got := decodePayload([]byte("bad"), false); got != nil {
		t.Errorf("损坏 gzip = %v, want nil", got)
	}
	if got := decodePayload(tgz(t, "not-json"), false); got != "not-json" {
		t.Errorf("非法 JSON = %v, want 原文", got)
	}
	if got := decodePayload(tgz(t, "not-json"), true); got != "not-json…" {
		t.Errorf("截断的非法 JSON = %v, want 原文加省略号", got)
	}
}

// TestPairToolEvents 覆盖配对:重复事件、仅结束事件、坏时间戳与截断载荷。
func TestPairToolEvents(t *testing.T) {
	rows := []db.ToolEvent{
		// call1:重复 start 只保留首个开始时间;重复 end 不覆盖结束时间。
		{Client: "c", ToolCallID: "call1", ToolName: "Bash", Phase: "start",
			Ts: "2026-10-13T10:00:00Z", InputGzip: tgz(t, `{"cmd":"ls"}`), InputTruncated: 1},
		{Client: "c", ToolCallID: "call1", ToolName: "Bash", Phase: "start",
			Ts: "2026-10-13T10:00:01Z", InputGzip: tgz(t, `{"cmd":"dup"}`)},
		{Client: "c", ToolCallID: "call1", ToolName: "Bash", Phase: "end",
			Ts: "2026-10-13T10:00:03Z", Status: "ok", ResultGzip: tgz(t, `{"out":1}`)},
		{Client: "c", ToolCallID: "call1", ToolName: "Bash", Phase: "end",
			Ts: "2026-10-13T10:00:05Z", Status: "ignored", ResultGzip: tgz(t, `{"late":1}`)},
		// call2:只有 end:输入从 end 事件补取,结果非法 JSON 且标截断。
		{Client: "c", ToolCallID: "call2", ToolName: "Read", Phase: "end",
			Ts: "2026-10-13T10:00:10Z", Status: "error",
			InputGzip: tgz(t, `{"x":1}`), ResultGzip: tgz(t, "not-json"), ResultTruncated: 1},
		// call3:开始时间非法:不给时长。
		{Client: "c", ToolCallID: "call3", ToolName: "Bash", Phase: "start", Ts: "bad"},
		{Client: "c", ToolCallID: "call3", ToolName: "Bash", Phase: "end",
			Ts: "2026-10-13T10:00:20Z", Status: "ok"},
		// call4:结束早于开始:不给时长。
		{Client: "c", ToolCallID: "call4", ToolName: "Bash", Phase: "start",
			Ts: "2026-10-13T10:00:30Z"},
		{Client: "c", ToolCallID: "call4", ToolName: "Bash", Phase: "end",
			Ts: "2026-10-13T10:00:29Z", Status: "ok"},
		// call5:结果载荷损坏:结果为空,但时长照给。
		{Client: "c", ToolCallID: "call5", ToolName: "Bash", Phase: "start",
			Ts: "2026-10-13T10:00:40Z"},
		{Client: "c", ToolCallID: "call5", ToolName: "Bash", Phase: "end",
			Ts: "2026-10-13T10:00:41Z", Status: "ok", ResultGzip: []byte("bad")},
	}
	got := pairToolEvents(rows)
	if len(got) != 5 {
		t.Fatalf("调用数 = %d, want 5", len(got))
	}
	byID := map[string]toolCall{}
	for _, call := range got {
		byID[call.ToolCallID] = call
	}

	call1 := byID["call1"]
	if call1.StartedAt != "2026-10-13T10:00:00Z" {
		t.Errorf("重复 start 覆盖了开始时间: %q", call1.StartedAt)
	}
	if call1.FinishedAt != "2026-10-13T10:00:03Z" || call1.Status != "ok" {
		t.Errorf("重复 end 覆盖了结束: %#v", call1)
	}
	if !call1.Complete || call1.DurationMS == nil || *call1.DurationMS != 3000 {
		t.Errorf("call1 时长 = %#v, want 3000ms", call1)
	}
	if call1.Input == nil || call1.InputTruncated {
		t.Errorf("call1 入参 = %#v", call1.Input)
	}

	call2 := byID["call2"]
	if call2.Complete || call2.DurationMS != nil {
		t.Errorf("仅有结束事件不应给时长: %#v", call2)
	}
	if call2.FinishedAt != "2026-10-13T10:00:10Z" || call2.Status != "error" {
		t.Errorf("call2 结束状态 = %#v", call2)
	}
	if call2.Input == nil {
		t.Error("call2 入参应从 end 事件补取")
	}
	if call2.Result != "not-json…" || !call2.ResultTruncated {
		t.Errorf("call2 结果 = %#v, want 截断原文", call2.Result)
	}

	if byID["call3"].Complete || byID["call4"].Complete {
		t.Errorf("坏时间戳或倒挂不应标完成: %#v / %#v", byID["call3"], byID["call4"])
	}
	call5 := byID["call5"]
	if !call5.Complete || call5.Result != nil {
		t.Errorf("call5 = %#v, want 时长有值、结果为空", call5)
	}
	if call5.DurationMS == nil || *call5.DurationMS != 1000 {
		t.Errorf("call5 时长 = %v, want 1000ms", call5.DurationMS)
	}
	// 输出顺序按首次出现,不按 map 遍历。
	wantOrder := []string{"call1", "call2", "call3", "call4", "call5"}
	for i, want := range wantOrder {
		if got[i].ToolCallID != want {
			t.Errorf("第 %d 项 = %s, want %s", i, got[i].ToolCallID, want)
		}
	}
}
