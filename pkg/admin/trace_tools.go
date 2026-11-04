package admin

import (
	"context"
	"sort"
	"time"
)

// attachToolDurations 按时间把工具事件挂到「生成该次调用」的步上。
//
// 归属规则:工具在某个响应结束后才开始执行,因此取响结束时刻不晚于工具开始时刻的最近一步。
// 同一工具调用在请求历史里出现在下一步,不能用 IssuedSeq 直接归属,否则会整体后移一步。
// 同一步的多个工具可能并行,这里取区间并集,避免求和把墙钟时间放大。
func (a *Admin) attachToolDurations(ctx context.Context, an *sessionAnalysis, skeleton map[string]any) {
	rows, err := a.store.ListToolEvents(ctx, an.sessionID)
	if err != nil || len(rows) == 0 {
		return
	}
	type stepRef struct {
		seq int64
		end time.Time
	}
	var steps []stepRef
	for _, turn := range asTurns(skeleton) {
		for _, step := range asSteps(turn) {
			seq, ok := step["seq"].(int64)
			if !ok {
				continue
			}
			ts, _ := step["ts"].(string)
			start, err := time.Parse(time.RFC3339Nano, ts)
			if err != nil {
				continue
			}
			latency, _ := step["latency_ms"].(int64)
			steps = append(steps, stepRef{seq: seq, end: start.Add(time.Duration(latency) * time.Millisecond)})
		}
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].end.Before(steps[j].end) })

	intervals := make(map[int64][][2]time.Time)
	for _, call := range pairToolEvents(rows) {
		if call.StartedAt == "" || call.FinishedAt == "" {
			continue
		}
		start, err := time.Parse(time.RFC3339Nano, call.StartedAt)
		if err != nil {
			continue
		}
		end, err := time.Parse(time.RFC3339Nano, call.FinishedAt)
		if err != nil || !end.After(start) {
			continue
		}
		// 允许工具早于响应结束 500ms:流式响应落盘与钩子触发存在先后差。
		limit := start.Add(500 * time.Millisecond)
		idx := sort.Search(len(steps), func(i int) bool { return steps[i].end.After(limit) }) - 1
		if idx < 0 {
			continue
		}
		seq := steps[idx].seq
		intervals[seq] = append(intervals[seq], [2]time.Time{start, end})
	}

	for _, turn := range asTurns(skeleton) {
		for _, step := range asSteps(turn) {
			seq, ok := step["seq"].(int64)
			if !ok {
				continue
			}
			if ms, ok := unionMillis(intervals[seq]); ok {
				step["tool_ms"] = ms
			}
		}
	}
}

// unionMillis 返回区间并集的总毫秒数;无区间时返回 false。
func unionMillis(items [][2]time.Time) (int64, bool) {
	if len(items) == 0 {
		return 0, false
	}
	sort.Slice(items, func(i, j int) bool { return items[i][0].Before(items[j][0]) })
	var total time.Duration
	curStart, curEnd := items[0][0], items[0][1]
	for _, item := range items[1:] {
		if item[0].After(curEnd) {
			total += curEnd.Sub(curStart)
			curStart, curEnd = item[0], item[1]
			continue
		}
		if item[1].After(curEnd) {
			curEnd = item[1]
		}
	}
	total += curEnd.Sub(curStart)
	return total.Milliseconds(), true
}

// asTurns 把骨架的 turns 字段转成可遍历的切片。
func asTurns(skeleton map[string]any) []map[string]any {
	switch turns := skeleton["turns"].(type) {
	case []map[string]any:
		return turns
	case []any:
		out := make([]map[string]any, 0, len(turns))
		for _, turn := range turns {
			if m, ok := turn.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

// asSteps 把轮次的 steps 字段转成可遍历的切片。
func asSteps(turn map[string]any) []map[string]any {
	switch steps := turn["steps"].(type) {
	case []map[string]any:
		return steps
	case []any:
		out := make([]map[string]any, 0, len(steps))
		for _, step := range steps {
			if m, ok := step.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}
