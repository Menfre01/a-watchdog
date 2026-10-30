package trace

import "testing"

// assistantCalls 构造一条含若干 tool_use 的 assistant 消息。
func assistantCalls(calls ...MainlineBlock) MainlineMessage {
	return MainlineMessage{Role: "assistant", Blocks: calls}
}

// userResults 构造一条含若干 tool_result 的 user 消息。
func userResults(results ...MainlineBlock) MainlineMessage {
	return MainlineMessage{Role: "user", Blocks: results}
}

// use 构造 tool_use 块。
func use(id, name string, input any) MainlineBlock {
	return MainlineBlock{"type": "tool_use", "id": id, "name": name, "input": input}
}

// result 构造 tool_result 块。
func result(id string, content any, isError bool) MainlineBlock {
	return MainlineBlock{"type": "tool_result", "tool_use_id": id, "content": content, "is_error": isError}
}

// TestExtractToolsPairsAcrossSteps 覆盖跨 step 配对:调用在发起步,结果在后续步。
func TestExtractToolsPairsAcrossSteps(t *testing.T) {
	steps := []StepMessages{
		{Seq: 1, Messages: []MainlineMessage{
			{Role: "user", Blocks: []MainlineBlock{{"type": "text", "text": "hi"}}},
			assistantCalls(use("t1", "Read", map[string]any{"path": "a.go"})),
		}},
		{Seq: 2, Messages: []MainlineMessage{
			userResults(result("t1", []any{map[string]any{"type": "text", "text": "file body"}}, false)),
		}},
	}
	calls := ExtractTools(steps)
	if len(calls) != 1 {
		t.Fatalf("调用数 = %d, want 1", len(calls))
	}
	got := calls[0]
	if got.ID != "t1" || got.Name != "Read" || got.Status != ToolOK {
		t.Errorf("调用 = %+v", got)
	}
	if got.IssuedSeq != 1 || got.ResultSeq != 2 {
		t.Errorf("发起/结果 step = %d/%d, want 1/2", got.IssuedSeq, got.ResultSeq)
	}
	if got.Result == nil || got.IsError {
		t.Errorf("结果 = %#v / isError = %v", got.Result, got.IsError)
	}
}

// TestExtractToolsMultipleCallsPerStep 覆盖同轮多调用:一次 assistant 消息含多个 tool_use。
func TestExtractToolsMultipleCallsPerStep(t *testing.T) {
	steps := []StepMessages{
		{Seq: 1, Messages: []MainlineMessage{
			assistantCalls(
				use("a", "Read", map[string]any{"p": 1}),
				use("b", "Grep", map[string]any{"p": 2}),
			),
		}},
		{Seq: 2, Messages: []MainlineMessage{
			userResults(result("a", "A", false), result("b", "B", false)),
		}},
	}
	calls := ExtractTools(steps)
	if len(calls) != 2 {
		t.Fatalf("调用数 = %d, want 2", len(calls))
	}
	if calls[0].Name != "Read" || calls[1].Name != "Grep" {
		t.Errorf("顺序 = %s, %s(应按出现顺序)", calls[0].Name, calls[1].Name)
	}
	for _, call := range calls {
		if call.Status != ToolOK {
			t.Errorf("%s 状态 = %q, want ok", call.Name, call.Status)
		}
	}
}

// TestExtractToolsMissingResult 覆盖结果缺失:标记为「结果未见」,不伪装成功。
func TestExtractToolsMissingResult(t *testing.T) {
	steps := []StepMessages{
		{Seq: 1, Messages: []MainlineMessage{assistantCalls(use("lost", "Bash", "{}"))}},
		{Seq: 2, Messages: []MainlineMessage{
			userResults(result("other", "unrelated", false)),
		}},
	}
	calls := ExtractTools(steps)
	if len(calls) != 1 {
		t.Fatalf("调用数 = %d, want 1", len(calls))
	}
	if calls[0].Status != ToolUnseen {
		t.Errorf("状态 = %q, want unseen", calls[0].Status)
	}
	if calls[0].Result != nil || calls[0].ResultSeq != 0 {
		t.Errorf("未见的结果不应有内容: %+v", calls[0])
	}
}

// TestExtractToolsErrorResult 覆盖失败结果:is_error 为真 → 状态失败。
func TestExtractToolsErrorResult(t *testing.T) {
	steps := []StepMessages{
		{Seq: 1, Messages: []MainlineMessage{assistantCalls(use("t1", "Bash", "{}"))}},
		{Seq: 2, Messages: []MainlineMessage{userResults(result("t1", "FAIL", true))}},
	}
	calls := ExtractTools(steps)
	if len(calls) != 1 || calls[0].Status != ToolError || !calls[0].IsError {
		t.Fatalf("失败调用 = %+v", calls)
	}
}

// TestExtractToolsResultFarAhead 覆盖结果出现在更后面的 step(跨多步配对)。
func TestExtractToolsResultFarAhead(t *testing.T) {
	steps := []StepMessages{
		{Seq: 1, Messages: []MainlineMessage{assistantCalls(use("t1", "Read", "{}"))}},
		{Seq: 2, Messages: []MainlineMessage{{Role: "assistant", Blocks: []MainlineBlock{{"type": "text", "text": "waiting"}}}}},
		{Seq: 3, Messages: []MainlineMessage{userResults(result("t1", "content", false))}},
	}
	calls := ExtractTools(steps)
	if len(calls) != 1 || calls[0].ResultSeq != 3 || calls[0].Status != ToolOK {
		t.Fatalf("跨多步配对 = %+v", calls)
	}
}

// TestExtractToolsPairsSameStepResult 覆盖同一请求内同时带回调用与结果:
// 客户端可能一次性补两条消息,此时也视为已配对(结果不得早于发起步)。
func TestExtractToolsPairsSameStepResult(t *testing.T) {
	steps := []StepMessages{
		{Seq: 1, Messages: []MainlineMessage{
			assistantCalls(use("t1", "Read", "{}")),
			userResults(result("t1", "content", false)),
		}},
	}
	calls := ExtractTools(steps)
	if len(calls) != 1 {
		t.Fatalf("调用数 = %d, want 1", len(calls))
	}
	if calls[0].Status != ToolOK || calls[0].ResultSeq != 1 {
		t.Errorf("同请求内的结果应配对: %+v", calls[0])
	}
}

// TestExtractToolsKeepsRawInput 覆盖入参保真:解析失败的字符串原样带出。
func TestExtractToolsKeepsRawInput(t *testing.T) {
	steps := []StepMessages{
		{Seq: 1, Messages: []MainlineMessage{assistantCalls(use("t1", "F", "{not json"))}},
	}
	calls := ExtractTools(steps)
	if len(calls) != 1 {
		t.Fatalf("调用数 = %d", len(calls))
	}
	if s, ok := calls[0].Input.(string); !ok || s != "{not json" {
		t.Errorf("入参 = %#v, want 原样字符串", calls[0].Input)
	}
}

// TestExtractToolsEmpty 覆盖无调用的会话。
func TestExtractToolsEmpty(t *testing.T) {
	if calls := ExtractTools(nil); len(calls) != 0 {
		t.Errorf("空输入 = %v", calls)
	}
	steps := []StepMessages{{Seq: 1, Messages: []MainlineMessage{{Role: "user"}}}}
	if calls := ExtractTools(steps); len(calls) != 0 {
		t.Errorf("无调用 = %v", calls)
	}
}

// TestExtractToolsSameIDInMultipleSteps 覆盖同一 tool_use 出现在多个步的新增区间:
// 客户端会继续补全助手消息(实测:seq=70 的一个 tool_use,到 seq=71 变成两个,
// 原消息被重新计入新增)。此时必须只取首次出现——否则后到的副本找不到结果
// (结果已按首次出现索引),会被误报为「结果未见」并把同一次调用重复列出。
func TestExtractToolsSameIDInMultipleSteps(t *testing.T) {
	steps := []StepMessages{
		// 第 70 步:助手消息含一个 tool_use。
		{Seq: 70, Messages: []MainlineMessage{assistantCalls(use("c1", "Bash", map[string]any{"cmd": "a"}))}},
		// 第 71 步:同一条助手消息被补全(多了一个 tool_use)→ c1 再次进入新增区间。
		{Seq: 71, Messages: []MainlineMessage{
			assistantCalls(
				use("c1", "Bash", map[string]any{"cmd": "a"}),
				use("c2", "Bash", map[string]any{"cmd": "b"}),
			),
			userResults(result("c1", "out-1", false), result("c2", "out-2", false)),
		}},
	}
	calls := ExtractTools(steps)
	if len(calls) != 2 {
		t.Fatalf("调用数 = %d, want 2(重复的 c1 不应再列一次): %+v", len(calls), calls)
	}
	for _, call := range calls {
		if call.Status != ToolOK {
			t.Errorf("%s 状态 = %q, want ok", call.ID, call.Status)
		}
	}
	// c1 归属首次出现的步。
	if calls[0].ID != "c1" || calls[0].IssuedSeq != 70 || calls[0].ResultSeq != 71 {
		t.Errorf("c1 = %+v, want issued=70 result=71", calls[0])
	}
	if calls[1].ID != "c2" || calls[1].IssuedSeq != 71 {
		t.Errorf("c2 = %+v, want issued=71", calls[1])
	}
}
