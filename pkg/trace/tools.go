package trace

// ToolStatus 是工具调用的状态。
type ToolStatus string

const (
	ToolOK     ToolStatus = "ok"     // 结果存在且非错误
	ToolError  ToolStatus = "error"  // 结果存在且 is_error 为真
	ToolUnseen ToolStatus = "unseen" // 结果在后续 step 中未出现
)

// StepMessages 是一个 step 的新增消息及其序号。
// 调用抽自发起步的新增行,结果从后续步的请求历史中得到(响应不落盘)。
type StepMessages struct {
	Seq      int64
	Messages []MainlineMessage
}

// ToolCall 是一次工具调用的明细。
type ToolCall struct {
	ID        string     // tool_use 的标识(配对键)
	Name      string     // 工具名(带 mcp__ 前缀者来自 MCP 服务器)
	Input     any        // 入参:对象,或解析失败时保留的原始字符串
	Result    any        // 结果内容(结果的 content 字段);未见时为 nil
	Status    ToolStatus // 成功 / 失败 / 结果未见
	IsError   bool       // 结果的 is_error 标记
	IssuedSeq int64      // 发起调用的 step 序号
	ResultSeq int64      // 结果出现的 step 序号;0 表示未见
}

// ExtractTools 抽取工具调用并与结果配对,按出现顺序返回。
// 配对规则:同一 tool_use_id 的结果必须出现在发起步之后的某个 step(design.md D5);
// 结果缺失时不伪装为成功,而是标记为「结果未见」。
func ExtractTools(steps []StepMessages) []ToolCall {
	results := collectToolResults(steps)
	calls := make([]ToolCall, 0)
	// 同一个 tool_use 会在多个步的新增区间里出现:客户端会继续补全该助手消息
	// (实测:seq=70 的一个 tool_use,到 seq=71 变成两个,原消息被重新计入新增)。
	// 只取**首次出现**——结果的索引同样取首次出现,后到的副本按 seq 比较会
	// 判成「结果未见」(结果早于副本所在的步),并把同一次调用重复列出。
	seen := make(map[string]bool, len(steps))
	for _, step := range steps {
		for _, msg := range step.Messages {
			for _, block := range msg.Blocks {
				if typeOf(map[string]any(block)) != "tool_use" {
					continue
				}
				id := stringField(block, "id")
				if id != "" && seen[id] {
					continue
				}
				if id != "" {
					seen[id] = true
				}
				call := ToolCall{
					ID:        id,
					Name:      stringField(block, "name"),
					Input:     block["input"],
					Status:    ToolUnseen,
					IssuedSeq: step.Seq,
				}
				// 结果不得早于发起步;同一请求内同时带回调用与结果时也视为已配对
				// (客户端可能一次性补两条消息)。
				if ref, ok := results[call.ID]; ok && ref.seq >= step.Seq {
					call.Result = ref.content
					call.IsError = ref.isError
					call.ResultSeq = ref.seq
					call.Status = ToolOK
					if ref.isError {
						call.Status = ToolError
					}
				}
				calls = append(calls, call)
			}
		}
	}
	return calls
}

// toolResultRef 是索引到的工具结果。
type toolResultRef struct {
	content any
	isError bool
	seq     int64
}

// collectToolResults 按 tool_use_id 索引工具结果,同 id 多次出现时保留最早的一个。
func collectToolResults(steps []StepMessages) map[string]toolResultRef {
	results := make(map[string]toolResultRef)
	for _, step := range steps {
		for _, msg := range step.Messages {
			for _, block := range msg.Blocks {
				if typeOf(map[string]any(block)) != "tool_result" {
					continue
				}
				id := stringField(block, "tool_use_id")
				if id == "" {
					continue
				}
				if _, exists := results[id]; exists {
					continue
				}
				isError, _ := block["is_error"].(bool)
				results[id] = toolResultRef{content: block["content"], isError: isError, seq: step.Seq}
			}
		}
	}
	return results
}
