package trace

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ParseMessages 解析整段快照 ndjson 为主干消息序列。
// 数字以 json.Number 保留原样,与序列化侧一致。
func ParseMessages(ndjson []byte) ([]MainlineMessage, error) {
	lines := ParseLines(ndjson)
	return parseLines(lines, 0, len(lines))
}

// ParseMessagesRange 只解析 [start, end) 区间的行。
// 增量分类与轮次边界只依赖行指纹,内容按需解析——历史行通常无需解析(design.md D5)。
// 越界区间按可用范围收敛;空区间返回空序列。
func ParseMessagesRange(ndjson []byte, start, end int) ([]MainlineMessage, error) {
	lines := ParseLines(ndjson)
	if start < 0 {
		start = 0
	}
	if end < 0 || end > len(lines) {
		end = len(lines)
	}
	if start >= end {
		return nil, nil
	}
	return parseLines(lines, start, end)
}

// parseLines 解析 lines[start:end) 为消息序列。
func parseLines(lines [][]byte, start, end int) ([]MainlineMessage, error) {
	out := make([]MainlineMessage, 0, end-start)
	for i := start; i < end; i++ {
		msg, err := decodeMessage(lines[i])
		if err != nil {
			return nil, fmt.Errorf("trace: 解析第 %d 行: %w", i, err)
		}
		out = append(out, msg)
	}
	return out, nil
}

// decodeMessage 解析单行为主干消息:块内数字保留原样(不转为 float64)。
func decodeMessage(line []byte) (MainlineMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	var msg MainlineMessage
	if err := dec.Decode(&msg); err != nil {
		return MainlineMessage{}, err
	}
	return msg, nil
}
