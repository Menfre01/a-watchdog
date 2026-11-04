package admin

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"awatchdog/pkg/config/db"
	"awatchdog/pkg/trace"
)

// turnTitleMaxRunes 是轮次标题的最大字符数。
const turnTitleMaxRunes = 80

// skeletonJSON 组装轨迹骨架:turns → steps,step 只含指纹级字段与判定理由。
func (an *sessionAnalysis) skeletonJSON() map[string]any {
	indexOf := make(map[int64]int, len(an.steps))
	var usageIn, usageOut, usageCache int64
	// 主路与旁路各自累加:总计含旁路,拆分让读者不必自己相减。
	main := map[string]int64{"steps": 0, "in": 0, "out": 0, "cache": 0}
	side := map[string]int64{"steps": 0, "in": 0, "out": 0, "cache": 0}
	for i, step := range an.steps {
		indexOf[step.Seq] = i
		in, out, cache := derefInt(step.UsageIn), derefInt(step.UsageOut), derefInt(step.UsageCache)
		usageIn += in
		usageOut += out
		usageCache += cache
		bucket := main
		if an.sideSteps[step.Seq] {
			bucket = side
		}
		bucket["steps"]++
		bucket["in"] += in
		bucket["out"] += out
		bucket["cache"] += cache
	}

	turns := make([]map[string]any, 0, len(an.turns))
	// harness 侧耗时是会话级的:同链下一步可能落在别的轮次里。
	harness := an.harnessBySeq()
	for _, turn := range an.turns {
		steps := make([]map[string]any, 0, len(turn.StepSeqs))
		compresses, retries, failures, side := 0, 0, 0, 0
		for _, seq := range turn.StepSeqs {
			i, ok := indexOf[seq]
			if !ok {
				continue
			}
			steps = append(steps, an.stepSkeletonJSON(i))
			if an.sideSteps[seq] {
				side++
				continue // 旁路步不参与轮次的压缩/重试/失败统计
			}
			if an.deltas[i].Summarized {
				compresses++
			}
			if an.deltas[i].RetryOf > 1 {
				retries++
			}
			if failed(an.steps[i]) {
				failures++
			}
		}
		startedAt := ""
		for _, seq := range turn.StepSeqs {
			// 轮次起始时间取首个对话步:旁路请求可能先于它到达。
			if an.sideSteps[seq] {
				continue
			}
			if i, ok := indexOf[seq]; ok {
				startedAt = an.steps[i].Ts
				break
			}
		}
		// 时间轴偏移在服务端算:前端解析 RFC3339 会重复实现口径(design.md D1)。
		offsets, duration, axisOK := an.turnAxis(turn.StepSeqs, indexOf)
		if axisOK {
			for _, step := range steps {
				if seq, ok := step["seq"].(int64); ok {
					if off, ok := offsets[seq]; ok {
						step["at_ms"] = off
					}
				}
			}
		}
		for _, step := range steps {
			seq, ok := step["seq"].(int64)
			if !ok {
				continue
			}
			if ms, ok := harness[seq]; ok {
				step["harness_ms"] = ms
			}
		}
		turns = append(turns, map[string]any{
			"index":       turn.Index,
			"started_at":  startedAt,
			"duration_ms": duration,
			"parallel":    turn.Parallel,
			"compresses":  compresses,
			"retries":     retries,
			"failures":    failures,
			"side_count":  side,
			"story":       an.turnStory(turn.StepSeqs, indexOf),
			"steps":       steps,
		})
	}
	return map[string]any{
		"session_id":     an.sessionID,
		"session_source": an.source,
		"session_note":   an.note,
		"first_seen":     an.firstSeen,
		"last_seen":      an.lastSeen,
		"steps":          len(an.steps),
		"usage_in":       usageIn,
		"usage_out":      usageOut,
		"usage_cache":    usageCache,
		"main":           usageSplitJSON(main),
		"side":           usageSplitJSON(side),
		"turns":          turns,
		"kinds":          an.kindDistribution(),
		"chains":         an.chainSummary(),
	}
}

// chainSummary 汇总会话内的链:含会话首步的那条是主链,其余是旁路链。
// 链归属是推断,判据是两级指纹(见 trace.AnalyzeSession)。任一步缺 system 指纹时
// 归属退化为不拆分,此时不给链数:退化的「1 条链」不是事实(admin-console 规范)。
func (an *sessionAnalysis) chainSummary() map[string]any {
	if len(an.steps) > 1 {
		for _, line := range an.lines {
			if line.SystemFP == "" {
				return map[string]any{
					"degraded": true,
					"note":     "缺 system 指纹,链不可分",
				}
			}
		}
	}
	// 旁路链编号从 1 起连续给出,这里按值去重计数,不依赖编号连续。
	numbered := make(map[int]bool, len(an.sideChainOf))
	for _, n := range an.sideChainOf {
		numbered[n] = true
	}
	sideSteps := 0
	var sideIn, sideOut int64
	for _, step := range an.steps {
		if !an.sideSteps[step.Seq] {
			continue
		}
		sideSteps++
		sideIn += derefInt(step.UsageIn)
		sideOut += derefInt(step.UsageOut)
	}
	return map[string]any{
		"main":           1,
		"side":           len(numbered),
		"side_steps":     sideSteps,
		"side_usage_in":  sideIn,
		"side_usage_out": sideOut,
	}
}

// harnessBySeq 给出每步之后的 harness 侧耗时:同链下一步的开始时间减去本步结束时间。
//
// 这是**推算值**,含三部分且不可拆分:工具在客户端的执行、客户端自身的处理、
// 轮次边界处人的思考。因此不得命名为「工具耗时」。窗口还含客户端同期为其它链
// 所做的工作与不生成 step 的其它请求,故它不是该步独占的工作量。
//
// 链末步没有后继,不输出该值。推算结果为负时(并行请求导致后继早于本步结束)
// 同样不输出:那是「不可测」,不是「没花时间」,界面据此显示「—」。
func (an *sessionAnalysis) harnessBySeq() map[int64]int64 {
	// 同链后继:prevIdxOf 记的是「同链上一步」,取反即可。
	// 旁路步因此不会被算进主路步的 harness 侧耗时(admin-console 规范)。
	next := make(map[int]int, len(an.prevIdxOf))
	for cur, prev := range an.prevIdxOf {
		next[prev] = cur
	}
	out := make(map[int64]int64, len(next))
	for prevIdx, curIdx := range next {
		prevEnd, err := stepEnd(an.steps[prevIdx])
		if err != nil {
			continue
		}
		curStart, err := time.Parse(time.RFC3339Nano, an.steps[curIdx].Ts)
		if err != nil {
			continue
		}
		gap := curStart.Sub(prevEnd).Milliseconds()
		if gap < 0 {
			continue
		}
		out[an.steps[prevIdx].Seq] = gap
	}
	return out
}

// stepEnd 返回该步的结束时间:开始时间 + 记录侧实测耗时。
func stepEnd(step db.ListSessionStepsRow) (time.Time, error) {
	start, err := time.Parse(time.RFC3339Nano, step.Ts)
	if err != nil {
		return time.Time{}, err
	}
	return start.Add(time.Duration(step.LatencyMs) * time.Millisecond), nil
}

// turnAxis 给出该轮时间轴:每个 step 相对轴起点的毫秒偏移,以及轴总时长。
// 轴跨度取本轮全部请求(含旁路步):旁路请求真实占用墙钟时间,隐藏它会得出
// 错误的时间结构(design.md D2)。时间戳不可解析时 ok 为 false,界面此时不画轴,
// 不用零偏移冒充「同时开始」。
func (an *sessionAnalysis) turnAxis(seqs []int64, indexOf map[int64]int) (map[int64]int64, int64, bool) {
	type span struct {
		seq   int64
		start time.Time
		end   time.Time
	}
	spans := make([]span, 0, len(seqs))
	for _, seq := range seqs {
		i, ok := indexOf[seq]
		if !ok {
			continue
		}
		start, err := time.Parse(time.RFC3339Nano, an.steps[i].Ts)
		if err != nil {
			return nil, 0, false
		}
		spans = append(spans, span{
			seq:   seq,
			start: start,
			end:   start.Add(time.Duration(an.steps[i].LatencyMs) * time.Millisecond),
		})
	}
	if len(spans) == 0 {
		return nil, 0, false
	}
	from, to := spans[0].start, spans[0].end
	for _, s := range spans {
		if s.start.Before(from) {
			from = s.start
		}
		if s.end.After(to) {
			to = s.end
		}
	}
	offsets := make(map[int64]int64, len(spans))
	for _, s := range spans {
		offsets[s.seq] = s.start.Sub(from).Milliseconds()
	}
	total := to.Sub(from).Milliseconds()
	if total < 0 {
		return nil, 0, false
	}
	return offsets, total, true
}

// turnStory 给出一轮的一行叙述:只重述已有判定与实测值,不引入新语义
// (design.md D7)。段序固定:首步分类 → 行数变化 → 首个细分标记 → 失败计数。
// 工具调用数属渐进填充层,由界面在 digest 到达后追加,故不在此处生成。
func (an *sessionAnalysis) turnStory(seqs []int64, indexOf map[int64]int) []map[string]any {
	firstIdx := -1
	added, removed, failures := 0, 0, 0
	var markSeq, failSeq int64
	mark := ""
	for _, seq := range seqs {
		i, ok := indexOf[seq]
		if !ok {
			continue
		}
		// 旁路步不进叙述:轮级统计只取主路步,与压缩/重试/失败计数一致。
		if an.sideSteps[seq] {
			continue
		}
		if firstIdx < 0 {
			firstIdx = i
		}
		delta := an.deltas[i]
		added += delta.Added
		removed += delta.Removed
		if mark == "" {
			mark, markSeq = fineMark(delta, seq)
		}
		if failed(an.steps[i]) {
			failures++
			if failSeq == 0 {
				failSeq = seq
			}
		}
	}
	parts := make([]map[string]any, 0, 4)
	if firstIdx >= 0 {
		parts = append(parts, storyPart(kindStoryLabel(an.deltas[firstIdx].Kind), an.steps[firstIdx].Seq))
	}
	parts = append(parts, storyPart(lineChangeText(added, removed), 0))
	if mark != "" {
		parts = append(parts, storyPart(mark, markSeq))
	}
	if failures > 0 {
		parts = append(parts, storyPart(fmt.Sprintf("%d 步失败", failures), failSeq))
	}
	return parts
}

// storyPart 组装叙述的一段;seq 为 0 表示该段没有可跳转的步。
func storyPart(text string, seq int64) map[string]any {
	part := map[string]any{"text": text}
	if seq != 0 {
		part["seq"] = seq
	}
	return part
}

// kindStoryLabel 给分类取叙述用词:与步徽标同一套词,不另造说法。
func kindStoryLabel(kind trace.DeltaKind) string {
	switch kind {
	case trace.DeltaNew:
		return "会话首步"
	case trace.DeltaAdded:
		return "追加"
	case trace.DeltaRetry:
		return "重复"
	case trace.DeltaFork:
		return "分叉"
	case trace.DeltaRewrite:
		return "改写"
	case trace.DeltaChain:
		return "无重合"
	}
	return string(kind)
}

// lineChangeText 描述行数变化:两侧都有时并列给出,单侧时只说单侧。
func lineChangeText(added, removed int) string {
	switch {
	case added > 0 && removed > 0:
		return fmt.Sprintf("移除 %d 行 · 新增 %d 行", removed, added)
	case added > 0:
		return fmt.Sprintf("新增 %d 行", added)
	case removed > 0:
		return fmt.Sprintf("移除 %d 行", removed)
	}
	return "无行数变化"
}

// fineMark 取该步的细分标记:一轮只取首个,避免叙述堆砌同类信息。
func fineMark(delta trace.Delta, seq int64) (string, int64) {
	switch {
	case delta.Summarized:
		return fmt.Sprintf("压缩 %d→%d 行", delta.PrevLines, delta.CurLines), seq
	case delta.Cleared:
		return "上下文清零", seq
	case delta.TailUpdate:
		return "末行更新", seq
	}
	return "", 0
}

// kindDistribution 统计各分类的步数与细分标记数量。
// 分类分项之和恒等于步数:每个 step 恰好属于一个分类。细分是分类内部的形态,
// 单独计数、不并入分项(design.md D8)。
func (an *sessionAnalysis) kindDistribution() map[string]any {
	byKind := make(map[trace.DeltaKind]int, 6)
	compressed, cleared, tailUpdate := 0, 0, 0
	for _, delta := range an.deltas {
		byKind[delta.Kind]++
		if delta.Summarized {
			compressed++
		}
		if delta.Cleared {
			cleared++
		}
		if delta.TailUpdate {
			tailUpdate++
		}
	}
	// 顺序固定:界面按位置配色,不依赖 map 遍历顺序。
	order := []trace.DeltaKind{
		trace.DeltaNew, trace.DeltaAdded, trace.DeltaRetry,
		trace.DeltaFork, trace.DeltaRewrite, trace.DeltaChain,
	}
	counts := make([]map[string]any, 0, len(order))
	for _, kind := range order {
		counts = append(counts, map[string]any{"kind": string(kind), "steps": byKind[kind]})
	}
	return map[string]any{
		"counts": counts,
		"marks": map[string]any{
			"compressed":  compressed,
			"cleared":     cleared,
			"tail_update": tailUpdate,
		},
		"steps": len(an.deltas),
	}
}

// stepNote 给出该步的判定理由。旁路步的说法单独写:链首的通用文案是
// 「会话首条快照,无前序可比」,那是主路首步的说法,套到旁路首步上会牛头不对马嘴。
func (an *sessionAnalysis) stepNote(i int) string {
	delta := an.deltas[i]
	n, isSide := an.sideChainOf[an.steps[i].Seq]
	if !isSide {
		return delta.Note()
	}
	if delta.Kind == trace.DeltaNew {
		return fmt.Sprintf("旁路链 #%d 的首次请求:客户端自动发出,与主路的消息序列不相交", n)
	}
	return fmt.Sprintf("旁路链 #%d 的延续:%s", n, delta.Note())
}

// stepSkeletonJSON 组装单步骨架:只含指纹级字段、压缩标记与判定理由。
func (an *sessionAnalysis) stepSkeletonJSON(i int) map[string]any {
	step := an.steps[i]
	delta := an.deltas[i]
	out := map[string]any{
		"seq":   step.Seq,
		"ts":    step.Ts,
		"model": step.RequestedModel,
		// 协议决定用量列的口径:Anthropic 的 in 不含缓存,OpenAI 兼容的 in 含缓存命中。
		// 视图据此标注,读者才不会把 in + cache 当成总输入(admin-console 规范)。
		"protocol":    step.Protocol,
		"status_code": step.StatusCode,
		"latency_ms":  step.LatencyMs,
		"stream":      step.Stream != 0,
		"usage_in":    step.UsageIn,
		"usage_out":   step.UsageOut,
		"usage_cache": step.UsageCache,
		"delta": map[string]any{
			"kind":       string(delta.Kind),
			"added":      delta.Added,
			"removed":    delta.Removed,
			"kept":       delta.Kept,
			"first_diff": delta.FirstDiff,
			"note":       an.stepNote(i),
		},
		"retry": map[string]any{"n": delta.RetryN, "of": delta.RetryOf},
	}
	if step.ErrorKind != nil && *step.ErrorKind != "" {
		out["error_kind"] = *step.ErrorKind
	}
	// 旁路步:内联在轮次里但带标记,由界面弱化呈现。
	if an.sideSteps[step.Seq] {
		out["side"] = true
		if n, ok := an.sideChainOf[step.Seq]; ok {
			out["side_chain"] = n
		}
	}
	if delta.Summarized {
		// from_seq 取同链上一步:会话内前一步可能是旁路请求。
		fromSeq := int64(0)
		if prev, ok := an.prevIdxOf[i]; ok {
			fromSeq = an.steps[prev].Seq
		}
		out["compress"] = map[string]any{
			"before_lines": delta.PrevLines,
			"after_lines":  delta.CurLines,
			"from_seq":     fromSeq,
		}
	}
	if delta.Cleared {
		out["cleared"] = true
	}
	// 末行更新:方向由字节数给出,界面据此选文案(扩展 / 收缩 / 改写)。
	if delta.TailUpdate {
		out["tail_update"] = true
		// 行长度列缺失时两侧为 -1:此时不给方向,界面退回中性文案。
		if delta.TailBefore >= 0 && delta.TailAfter >= 0 {
			out["tail_before"] = delta.TailBefore
			out["tail_after"] = delta.TailAfter
		}
	}
	// 分歧行数:骨架层即可显示「分叉 N 行」,无需加载两侧内容。
	if n := an.divergedLines(i); n > 0 {
		out["diff_lines"] = n
		deltaJSON := out["delta"].(map[string]any)
		deltaJSON["first_diff"] = delta.PrefixLen
	}
	return out
}

// digestJSON 组装渐进填充数据:每轮标题与工具聚合。
func (an *sessionAnalysis) digestJSON() map[string]any {
	byTurn := make([]map[string]any, 0, len(an.turns))
	for _, turn := range an.turns {
		calls, byName, title := 0, map[string]int{}, ""
		for _, seq := range turn.StepSeqs {
			i, ok := an.indexOfSeq(seq)
			if !ok {
				continue
			}
			// 旁路步不计入轮次标题与工具统计:标题应取用户输入,不是监控请求的转录。
			if an.sideSteps[seq] {
				continue
			}
			if title == "" {
				title = turnTitle(an.added[i])
			}
			for _, call := range an.tools {
				if call.IssuedSeq == seq {
					calls++
					byName[call.Name]++
				}
			}
		}
		byTurn = append(byTurn, map[string]any{
			"turn": turn.Index, "title": title, "calls": calls, "by_name": byName,
		})
	}
	totals := map[string]int{"calls": len(an.tools), "ok": 0, "err": 0, "unseen": 0}
	for _, call := range an.tools {
		switch call.Status {
		case trace.ToolOK:
			totals["ok"]++
		case trace.ToolError:
			totals["err"]++
		case trace.ToolUnseen:
			totals["unseen"]++
		}
	}
	return map[string]any{
		"by_turn": byTurn,
		"by_step": an.stepToolCalls(),
		"totals":  totals,
		"tools":   an.toolAggregates(),
	}
}

// stepToolCalls 汇总每个 step 发起了哪些工具调用:step 行据此显示工具标签
// (与轮次行的工具数同源,都是渐进填充)。
func (an *sessionAnalysis) stepToolCalls() []map[string]any {
	type stepTools struct {
		seq    int64
		calls  int
		byName map[string]int
	}
	var order []int64
	bySeq := map[int64]*stepTools{}
	for _, call := range an.tools {
		rec, ok := bySeq[call.IssuedSeq]
		if !ok {
			rec = &stepTools{seq: call.IssuedSeq, byName: map[string]int{}}
			bySeq[call.IssuedSeq] = rec
			order = append(order, call.IssuedSeq)
		}
		rec.calls++
		rec.byName[call.Name]++
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]map[string]any, 0, len(order))
	for _, seq := range order {
		rec := bySeq[seq]
		out = append(out, map[string]any{
			"seq": rec.seq, "calls": rec.calls, "by_name": rec.byName,
		})
	}
	return out
}

// stepDetailJSON 组装单步详情:新增(或全量)messages、继承行数、tool 明细与压缩对照。
func (an *sessionAnalysis) stepDetailJSON(i int, full bool, ctxLines int) map[string]any {
	step := an.steps[i]
	delta := an.deltas[i]
	out := map[string]any{
		"seq":       step.Seq,
		"ts":        step.Ts,
		"delta":     map[string]any{"kind": string(delta.Kind), "note": delta.Note()},
		"inherited": delta.PrefixLen,
		"tools":     an.toolsOfSeq(step.Seq),
		"messages":  an.added[i],
	}
	if full {
		if msgs, err := an.allMessages(i); err == nil {
			out["messages"] = msgs
		}
	}
	if delta.Summarized {
		out["compress"] = an.compressJSON(i)
	}
	// 差异块:同链相邻两步之间真正变动的部分(纯追加/相同时没有)。
	if hunks := an.diffJSON(i, ctxLines); hunks != nil {
		out["diff"] = hunks
		out["diff_lines"] = an.divergedLines(i)
		out["ctx_lines"] = ctxLines
	}
	return out
}

// compressJSON 组装摘要压缩的前后对照:压缩前序列、压缩后序列与三段计数。
func (an *sessionAnalysis) compressJSON(i int) map[string]any {
	delta := an.deltas[i]
	out := map[string]any{
		"before_lines": delta.PrevLines,
		"after_lines":  delta.CurLines,
		"kept":         delta.Kept,
		"removed":      delta.Removed,
		"added":        delta.Added,
		"after":        an.added[i],
	}
	// 压缩前序列:解析**同链上一步**快照的全部行(压缩对照是全量解析的路径之一)。
	// 必须用同链前驱——会话内前一步可能是旁路请求,拿它当"压缩前"毫无意义。
	if prev, ok := an.prevIdxOf[i]; ok {
		out["from_seq"] = an.steps[prev].Seq
		if msgs, err := an.allMessages(prev); err == nil {
			out["before"] = msgs
		}
	}
	return out
}

// diffMaxHunkLines 是差异块返回的行数上限:超出时截断。
// 一次改写可能涉及上百行,全量返回会让详情接口的响应体过大。
const diffMaxHunkLines = 80

// divergedLines 返回该步的**分歧**行数(不加载消息,骨架用)。
//
// 只统计改动与删除:纯追加的块不计入——那属于「追加」分类,其内容已由
// 「本轮新增消息」呈现,若也算作分歧会让徽标把追加误标为分叉。
func (an *sessionAnalysis) divergedLines(i int) int {
	total := 0
	for _, h := range an.diffHunks(i) {
		if h.Kind == trace.DiffInsert {
			continue
		}
		total += max(h.PrevCount, h.CurCount)
	}
	return total
}

// diffHunks 返回该步与**同链上一步**的差异块(缓存内计算一次)。
// 必须用同链前驱:会话内相邻的两步可能分属不同链(主路步的前一步常是旁路请求),
// 按会话下标取前一步会拿旁路请求当比较对象,得出一堆无关的差异。
func (an *sessionAnalysis) diffHunks(i int) []trace.DiffHunk {
	prev, ok := an.prevIdxOf[i]
	if !ok {
		return nil
	}
	return trace.DiffHunks(an.lines[prev], an.lines[i])
}

// diffJSON 组装差异块:每块给出两侧的行号范围、字节数与该处的实际消息。
//
// 用 LCS 求差异块而非逐位置对齐:消息被插入或删除后,其后所有行的位置都会
// 偏移,逐位置对齐会报出一大片无关的"不同";LCS 只报真正变动的部分,
// 删除与新增也各自成块。指纹只能说明「这些行变了」,长度只能说明「变长还是
// 变短」——两者都不足以断言压缩或改写,因此只把事实交给界面。
func (an *sessionAnalysis) diffJSON(i, ctxLines int) []map[string]any {
	hunks := an.diffHunks(i)
	if len(hunks) == 0 {
		return nil
	}
	// 纯追加不返回差异块:其内容已由「本轮新增消息」呈现,重复展示只会造成
	// 徽标把「追加」误标为「分叉」。
	if an.divergedLines(i) == 0 {
		return nil
	}
	prevIdx := an.prevIdxOf[i]
	prevSnap, curSnap := an.lines[prevIdx], an.lines[i]
	prevMsgs, _ := an.allMessages(prevIdx)
	curMsgs, _ := an.allMessages(i)

	out := make([]map[string]any, 0, len(hunks))
	shown := 0
	for k, h := range hunks {
		if shown >= diffMaxHunkLines {
			break
		}
		before, after := diffContextBounds(hunks, k, ctxLines, prevSnap.Lines, curSnap.Lines)
		row := map[string]any{
			// 块头按统一 diff 的标准语义给出:范围**含上下文行**。
			"header": diffHeader(h, before, after),
			"kind":   string(h.Kind),
			// 以下四个字段是**变更本身**的范围(不含上下文),供界面标注用。
			"prev_start": h.PrevStart, "prev_count": h.PrevCount,
			"cur_start": h.CurStart, "cur_count": h.CurCount,
		}
		// 与上一块之间未变更的行数:界面据此显示「… 未变更 N 行 …」。
		if k > 0 {
			p := hunks[k-1]
			row["unchanged_before"] = h.PrevStart - (p.PrevStart + p.PrevCount)
		}
		if rows := diffContextBefore(h, before, curSnap, curMsgs); len(rows) > 0 {
			row["ctx_before"] = rows
		}
		if rows := diffContextAfter(h, after, curSnap, curMsgs); len(rows) > 0 {
			row["ctx_after"] = rows
		}
		if prevRows := diffSideRows(h.PrevStart, h.PrevCount, prevSnap, prevMsgs); prevRows != nil {
			row["prev"] = prevRows
			row["prev_bytes"] = sumRowBytes(prevRows)
		}
		if curRows := diffSideRows(h.CurStart, h.CurCount, curSnap, curMsgs); curRows != nil {
			row["cur"] = curRows
			row["cur_bytes"] = sumRowBytes(curRows)
		}
		out = append(out, row)
		shown += max(h.PrevCount, h.CurCount)
	}
	return out
}

// 上下文行数:默认与 git 一致取 3,界面可请求更多(展开时)。
const (
	diffContextDefault = 3
	diffContextMax     = 200
)

// diffContextBounds 计算该块前后各能取多少行上下文:受序列两端与相邻块的范围限制,
// 不侵入相邻块——标准 diff 也不会让两个块的上下文重叠。
func diffContextBounds(hunks []trace.DiffHunk, k, ctxLines, prevLines, curLines int) (before, after int) {
	h := hunks[k]
	before = ctxLines
	after = ctxLines
	// 前置:不能越过序列开头。
	before = min(before, h.PrevStart, h.CurStart)
	// 后置:不能越过序列末尾。
	after = min(after, prevLines-(h.PrevStart+h.PrevCount), curLines-(h.CurStart+h.CurCount))
	// 不侵入相邻块。
	if k > 0 {
		p := hunks[k-1]
		before = min(before,
			h.PrevStart-(p.PrevStart+p.PrevCount),
			h.CurStart-(p.CurStart+p.CurCount))
	}
	if k+1 < len(hunks) {
		n := hunks[k+1]
		after = min(after,
			n.PrevStart-(h.PrevStart+h.PrevCount),
			n.CurStart-(h.CurStart+h.CurCount))
	}
	return max(before, 0), max(after, 0)
}

// diffHeader 拼出标准的块头 `@@ -旧侧范围 +新侧范围 @@`,范围含上下文行。
// 单行时按标准省略计数(写作 `-32` 而非 `-32,1`);零行时给出插入点(写作 `-32,0`)。
func diffHeader(h trace.DiffHunk, before, after int) string {
	side := func(start, count int) string {
		ctxStart := start - before + 1
		total := count + before + after
		switch total {
		case 1:
			return fmt.Sprintf("%d", ctxStart)
		case 0:
			return fmt.Sprintf("%d,0", max(ctxStart-1, 0))
		default:
			return fmt.Sprintf("%d,%d", ctxStart, total)
		}
	}
	return fmt.Sprintf("@@ -%s +%s @@", side(h.PrevStart, h.PrevCount), side(h.CurStart, h.CurCount))
}

// diffContextBefore 取变更前 count 行上下文,按时间顺序返回。
// 内容取自新侧(LCS 保证两侧相同),同时给出两侧行号。
func diffContextBefore(h trace.DiffHunk, count int, snap trace.SnapshotLines,
	msgs []trace.MainlineMessage,
) []map[string]any {
	if count <= 0 {
		return nil
	}
	rows := make([]map[string]any, 0, count)
	for j := count; j >= 1; j-- {
		rows = append(rows, diffContextRow(h.PrevStart-j, h.CurStart-j, snap, msgs))
	}
	return rows
}

// diffContextAfter 取变更后 count 行上下文,按时间顺序返回。
func diffContextAfter(h trace.DiffHunk, count int, snap trace.SnapshotLines,
	msgs []trace.MainlineMessage,
) []map[string]any {
	if count <= 0 {
		return nil
	}
	rows := make([]map[string]any, 0, count)
	for j := 0; j < count; j++ {
		rows = append(rows, diffContextRow(h.PrevStart+h.PrevCount+j, h.CurStart+h.CurCount+j, snap, msgs))
	}
	return rows
}

// diffContextRow 组装一行上下文:两侧行号、字节数与消息内容。
func diffContextRow(prevLine, curLine int, snap trace.SnapshotLines, msgs []trace.MainlineMessage) map[string]any {
	row := map[string]any{
		"prev_line": prevLine, "cur_line": curLine,
		"bytes": trace.LineSize(snap.LineSizes, curLine),
	}
	if curLine < len(msgs) {
		row["message"] = msgs[curLine]
	}
	return row
}

// diffSideRows 取差异块某一侧的消息与各自字节数;该侧不涉及此块时返回 nil。
func diffSideRows(start, count int, snap trace.SnapshotLines, msgs []trace.MainlineMessage) []map[string]any {
	if count <= 0 {
		return nil
	}
	rows := make([]map[string]any, 0, count)
	for line := start; line < start+count; line++ {
		row := map[string]any{"line": line, "bytes": trace.LineSize(snap.LineSizes, line)}
		if line < len(msgs) {
			row["message"] = msgs[line]
		}
		rows = append(rows, row)
	}
	return rows
}

// sumRowBytes 合计差异块一侧的字节数。
func sumRowBytes(rows []map[string]any) int {
	total := 0
	for _, row := range rows {
		if n, ok := row["bytes"].(int); ok && n > 0 {
			total += n
		}
	}
	return total
}

// toolAggregates 按工具名聚合:调用数、状态分布、近似间隔与发起轮次。
func (an *sessionAnalysis) toolAggregates() []map[string]any {
	type agg struct {
		calls, ok, err, unseen int
		gaps                   []int64
		turns                  map[int]bool
	}
	byName := map[string]*agg{}
	turnOfSeq := an.turnIndexBySeq()
	for _, call := range an.tools {
		rec := byName[call.Name]
		if rec == nil {
			rec = &agg{turns: map[int]bool{}}
			byName[call.Name] = rec
		}
		rec.calls++
		switch call.Status {
		case trace.ToolOK:
			rec.ok++
		case trace.ToolError:
			rec.err++
		case trace.ToolUnseen:
			rec.unseen++
		}
		if turn, ok := turnOfSeq[call.IssuedSeq]; ok {
			rec.turns[turn] = true
		}
		// 间隔归属:该调用之后那一步的间隔;结果未见的调用没有可归属的执行窗口,不计间隔。
		if call.Status != trace.ToolUnseen {
			if gap, ok := an.gapAfter(call.IssuedSeq); ok {
				rec.gaps = append(rec.gaps, gap)
			}
		}
	}
	out := make([]map[string]any, 0, len(byName))
	for name, rec := range byName {
		turns := make([]int, 0, len(rec.turns))
		for turn := range rec.turns {
			turns = append(turns, turn)
		}
		sort.Ints(turns)
		out = append(out, map[string]any{
			"name": name, "source": toolSource(name),
			"calls": rec.calls, "ok": rec.ok, "err": rec.err, "unseen": rec.unseen,
			"gaps_ms": rec.gaps, "turns": turns,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ci, cj := out[i]["calls"].(int), out[j]["calls"].(int)
		if ci != cj {
			return ci > cj
		}
		return out[i]["name"].(string) < out[j]["name"].(string)
	})
	return out
}

// toolsOfSeq 返回发起于该 step 的调用明细。
func (an *sessionAnalysis) toolsOfSeq(seq int64) []map[string]any {
	out := make([]map[string]any, 0)
	for _, call := range an.tools {
		if call.IssuedSeq != seq {
			continue
		}
		out = append(out, map[string]any{
			"tool_call_id": call.ID,
			"name":         call.Name, "status": string(call.Status),
			"input": call.Input, "result": call.Result,
			"issued_seq": call.IssuedSeq, "result_seq": call.ResultSeq,
		})
	}
	return out
}

// turnIndexBySeq 返回 step 序号 → 轮次序号。
func (an *sessionAnalysis) turnIndexBySeq() map[int64]int {
	out := make(map[int64]int, len(an.steps))
	for _, turn := range an.turns {
		for _, seq := range turn.StepSeqs {
			out[seq] = turn.Index
		}
	}
	return out
}

// gapAfter 返回该 step 之后那一步的间隔(毫秒);无后继时返回 false。
func (an *sessionAnalysis) gapAfter(seq int64) (int64, bool) {
	for i := 0; i+1 < len(an.steps); i++ {
		if an.steps[i].Seq == seq {
			return gapMs(an.steps[i], an.steps[i+1]), true
		}
	}
	return 0, false
}

// allMessages 解压并解析指定 step 的全部消息(压缩对照与全量切换用)。
func (an *sessionAnalysis) allMessages(i int) ([]trace.MainlineMessage, error) {
	ndjson, err := trace.Decompress(an.steps[i].NdjsonGzip)
	if err != nil {
		return nil, err
	}
	return trace.ParseMessages(ndjson)
}

// gapMs 返回相邻两步之间的间隔:后一步开始 − 前一步结束。
// 含客户端本地执行与人的思考时间,只是近似(admin-console 规范要求标明口径)。
func gapMs(prev, cur db.ListSessionStepsRow) int64 {
	prevStart, err1 := time.Parse(time.RFC3339Nano, prev.Ts)
	curStart, err2 := time.Parse(time.RFC3339Nano, cur.Ts)
	if err1 != nil || err2 != nil {
		return 0
	}
	gap := curStart.Sub(prevStart).Milliseconds() - prev.LatencyMs
	if gap < 0 {
		return 0
	}
	return gap
}

// turnTitle 取该轮新增消息中第一条 user 文本的片段;取不到时为空。
func turnTitle(messages []trace.MainlineMessage) string {
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		for _, block := range msg.Blocks {
			text, ok := block["text"].(string)
			if !ok || text == "" {
				continue
			}
			return truncateRunes(strings.TrimSpace(text), turnTitleMaxRunes)
		}
	}
	return ""
}

// toolSource 按工具名前缀判断来源。
func toolSource(name string) string {
	if strings.HasPrefix(name, "mcp__") {
		return "MCP"
	}
	return "内置"
}

// failed 判断该步是否失败(状态码非 2xx 或带网关侧归因)。
func failed(step db.ListSessionStepsRow) bool {
	return step.StatusCode >= 400 || (step.ErrorKind != nil && *step.ErrorKind != "")
}

// derefInt 解引用可空计数(空视为 0)。
func derefInt(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// truncateRunes 按字符数截断,超出部分以省略号收尾。
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// usageSplitJSON 把一组累加值转成骨架里的拆分对象。
func usageSplitJSON(m map[string]int64) map[string]any {
	return map[string]any{
		"steps":       m["steps"],
		"usage_in":    m["in"],
		"usage_out":   m["out"],
		"usage_cache": m["cache"],
	}
}
