package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"ds-context/pkg/config/db"
	"ds-context/pkg/trace"
)

// ---- 可观测(轨迹) ----

// errSessionNotFound 表示目标会话不存在(没有任何快照)。
var errSessionNotFound = errors.New("admin: session not found")

// traceListLimit 是会话列表的默认与最大条数。
const (
	traceListDefaultLimit = 100
	traceListMaxLimit     = 1000
)

// sessionAnalysis 是一个会话的推断结果:会话列表之外的三个视图共用。
type sessionAnalysis struct {
	sessionID string
	source    string
	note      string
	firstSeen string
	lastSeen  string
	steps     []db.ListSessionStepsRow
	lines     []trace.SnapshotLines
	deltas    []trace.Delta
	turns     []trace.Turn
	// sideSteps 标记旁路的步序号:客户端的监控 / 子 agent 请求,与主链不相交。
	// 它们内联在轮次里(保留时间对应关系),但不参与轮次边界判定。
	sideSteps map[int64]bool
	// sideChainOf 是旁路步 → 所属旁路链的编号(1 起),用于区分不同调用方。
	sideChainOf map[int64]int
	// prevIdxOf 是步下标 → **同链**上一步的下标(链首不在其中)。
	// diff 与「新增区间」都必须基于同链上一步:会话内相邻的两步可能分属不同链
	// (主路步的前一步常是旁路请求),按会话下标取前一步会拿错比较对象。
	prevIdxOf map[int]int
	added     [][]trace.MainlineMessage
	tools     []trace.ToolCall
}

// analyzeSession 组装会话分析。分类与分组只读派生列(指纹与行长度);
// 新增消息按 [prefixLen, curLines) 区间解析,不解析历史行(design.md D5)。
// 结果按「会话 + 最后一轮 request_id」缓存:有新轮次时键变化,旧结果自然失效。
func (a *Admin) analyzeSession(ctx context.Context, sessionID string) (*sessionAnalysis, error) {
	steps, err := a.store.ListSessionSteps(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if len(steps) == 0 {
		return nil, errSessionNotFound
	}
	// 按时间戳排序后再做链归属:seq 是写入序,并发请求(如多个 background
	// subagent)下完成顺序与发起顺序不一致,按 seq 处理会把链拆散或错并
	// (实测:同一会话里 seq=23 的时间戳比 seq=22 早 9 秒)。
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Ts < steps[j].Ts })
	cacheKey := fmt.Sprintf("%s:%d", sessionID, steps[len(steps)-1].RequestID)
	if cached, ok := a.traces.Get(cacheKey); ok {
		return cached, nil
	}

	lines := make([]trace.SnapshotLines, len(steps))
	for i, step := range steps {
		lines[i] = trace.SnapshotLinesOf(step.LineHashes, step.LineSizes)
	}
	// 解压一次:既算 system 指纹(链归属的硬边界),也切出各步新增。
	// 指纹缺失(无 system 消息或快照损坏)时留空,归属退回前缀判据。
	allMsgs := make([][]trace.MainlineMessage, len(steps))
	for i, step := range steps {
		msgs, err := allMessagesOf(step.NdjsonGzip)
		if err != nil {
			// 快照损坏不阻断视图:该步按空处理。
			continue
		}
		allMsgs[i] = msgs
		// 两级指纹:system 定请求类型,首条 user 前缀定同类型内的实例
		// (3 个并行子 agent 共用 system prompt,靠任务描述区分)。
		lines[i].SystemFP = trace.SystemFingerprint(msgs)
		lines[i].UserFP = trace.UserFingerprint(msgs)
	}
	// 链归属:旁路请求会周期性插入同一会话,只比相邻步会把主链每步都误判成独立链。
	chains := trace.AnalyzeSession(lines)
	deltas := make([]trace.Delta, len(steps))
	for _, chain := range chains {
		for k, idx := range chain.Steps {
			deltas[idx] = chain.Deltas[k]
		}
	}
	added := make([][]trace.MainlineMessage, len(steps))
	for _, chain := range chains {
		for k, idx := range chain.Steps {
			d := chain.Deltas[k]
			msgs := sliceMessages(allMsgs[idx], d.PrefixLen, d.CurLines)
			if msgs == nil {
				// 空区间(重试步)返回空数组而非 nil:序列化成 [] 而不是 null,
				// 前端才能安全地对它取 length。
				msgs = []trace.MainlineMessage{}
			}
			added[idx] = msgs
		}
	}

	// 旁路步内联在轮次里:监控请求通常是针对某一步操作发生的,时间对应关系本身
	// 就是信息。它们只带标记,不参与轮次边界判定(design.md D5)。
	sideSteps := make(map[int64]bool)
	// sideChainOf 给每条旁路链编号:同一次会话里可能存在多种旁路调用方
	// (实测:Claude Code 的「生成标题」与「安全监控」是两条互不相交的链),
	// 编号让界面能把它们区分开。
	sideChainOf := make(map[int64]int, len(steps))
	for n, chain := range chains[1:] {
		for _, idx := range chain.Steps {
			sideSteps[steps[idx].Seq] = true
			sideChainOf[steps[idx].Seq] = n + 1
		}
	}
	// prevIdxOf 记录每条链内部的前驱关系:diff 与新增区间都得跟同链上一步比较。
	prevIdxOf := make(map[int]int, len(steps))
	for _, chain := range chains {
		for k := 1; k < len(chain.Steps); k++ {
			prevIdxOf[chain.Steps[k]] = chain.Steps[k-1]
		}
	}
	turnSteps := make([]trace.StepForTurn, 0, len(steps))
	for i, step := range steps {
		turnSteps = append(turnSteps, trace.StepForTurn{
			Seq:   step.Seq,
			Delta: deltas[i],
			Added: added[i],
			Side:  sideSteps[step.Seq],
		})
	}
	toolInput := make([]trace.StepMessages, len(steps))
	for i, step := range steps {
		toolInput[i] = trace.StepMessages{Seq: step.Seq, Messages: added[i]}
	}

	analysis := &sessionAnalysis{
		sessionID:   sessionID,
		source:      steps[0].SessionSource,
		note:        steps[0].SessionNote,
		firstSeen:   steps[0].Ts,
		lastSeen:    steps[len(steps)-1].Ts,
		steps:       steps,
		lines:       lines,
		deltas:      deltas,
		turns:       trace.GroupTurns(turnSteps),
		sideSteps:   sideSteps,
		sideChainOf: sideChainOf,
		prevIdxOf:   prevIdxOf,
		added:       added,
		tools:       trace.ExtractTools(toolInput),
	}
	analysis.attributeTools()
	a.traces.Put(cacheKey, analysis)
	return analysis, nil
}

// attributeTools 把工具从「首次出现的步」退到「生成该调用的步」。
// tool_use 会出现在下一步请求的历史里;不退回,明细与工具耗时会落到错误的步上。
func (an *sessionAnalysis) attributeTools() {
	index := make(map[int64]int, len(an.steps))
	for i, step := range an.steps {
		index[step.Seq] = i
	}
	for i := range an.tools {
		call := &an.tools[i]
		idx, ok := index[call.IssuedSeq]
		if !ok {
			continue
		}
		prev, ok := an.prevIdxOf[idx]
		if !ok {
			continue
		}
		call.IssuedSeq = an.steps[prev].Seq
	}
}

// allMessagesOf 解压并解析整段快照。调用方一次解压后既算 system 指纹,
// 也从全量里切出各步新增,避免为同一步解压两次。
func allMessagesOf(compressed []byte) ([]trace.MainlineMessage, error) {
	ndjson, err := trace.Decompress(compressed)
	if err != nil {
		return nil, err
	}
	return trace.ParseMessages(ndjson)
}

// sliceMessages 切出 [start, end) 区间的消息;越界按可用范围收敛。
func sliceMessages(msgs []trace.MainlineMessage, start, end int) []trace.MainlineMessage {
	if start < 0 {
		start = 0
	}
	if end < 0 || end > len(msgs) {
		end = len(msgs)
	}
	if start >= end {
		return nil
	}
	return msgs[start:end]
}

// indexOfSeq 返回指定序号的 step 下标。
func (an *sessionAnalysis) indexOfSeq(seq int64) (int, bool) {
	for i, step := range an.steps {
		if step.Seq == seq {
			return i, true
		}
	}
	return 0, false
}

// handleListTraces 返回会话列表,支持标识前缀 / 时间范围 / client key 过滤。
// 列表不呈现轮次数(推断成本随会话数放大,见 admin-console 规范)。
func (a *Admin) handleListTraces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// 两个过滤条件各自成立:key_id 选密钥,prefix 匹配客户端原始标识。
	rows, err := a.store.ListSessionSummaries(
		r.Context(), q.Get("key_id"), q.Get("q"), q.Get("from"), q.Get("to"), parseLimit(q.Get("limit")),
	)
	if err != nil {
		a.storeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"session_id":     row.SessionID,
			"session_source": row.SessionSource,
			"session_note":   row.SessionNote,
			"agent":          trace.AgentFromNote(row.SessionNote),
			"first_seen":     row.FirstSeen,
			"last_seen":      row.LastSeen,
			"steps":          row.Steps,
			"usage_in":       row.UsageIn,
			"usage_out":      row.UsageOut,
			"usage_cache":    row.UsageCache,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleTraceDetail 返回会话轨迹骨架:轮次与 step 的指纹级信息与判定理由。
func (a *Admin) handleTraceDetail(w http.ResponseWriter, r *http.Request) {
	analysis, err := a.analyzeSession(r.Context(), r.PathValue("session_id"))
	if err != nil {
		a.traceErr(w, err)
		return
	}
	skeleton := analysis.skeletonJSON()
	a.attachToolDurations(r.Context(), analysis, skeleton)
	writeJSON(w, http.StatusOK, skeleton)
}

// handleTraceDigest 返回渐进填充数据:每轮标题与工具聚合(需解析各步新增行)。
func (a *Admin) handleTraceDigest(w http.ResponseWriter, r *http.Request) {
	analysis, err := a.analyzeSession(r.Context(), r.PathValue("session_id"))
	if err != nil {
		a.traceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, analysis.digestJSON())
}

// handleTraceStep 返回单个 step 的详情:新增消息、继承行数、tool 明细与压缩对照。
// 默认只返回本轮新增;full=1 返回全量 messages。
func (a *Admin) handleTraceStep(w http.ResponseWriter, r *http.Request) {
	analysis, err := a.analyzeSession(r.Context(), r.PathValue("session_id"))
	if err != nil {
		a.traceErr(w, err)
		return
	}
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid seq")
		return
	}
	idx, ok := analysis.indexOfSeq(seq)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "step not found")
		return
	}
	full := r.URL.Query().Get("full") == "1"
	detail := analysis.stepDetailJSON(idx, full, parseCtxLines(r.URL.Query().Get("ctx")))
	detail["response"] = a.responseOf(r.Context(), analysis.steps[idx].RequestID)
	writeJSON(w, http.StatusOK, detail)
}

// parseCtxLines 解析差异块的上下文行数:默认 3(与 git 一致),
// 非法值取默认,上限封顶(展开时前端会请求更大的值)。
func parseCtxLines(raw string) int {
	if raw == "" {
		return diffContextDefault
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return diffContextDefault
	}
	return min(n, diffContextMax)
}

// traceErr 把分析错误映射为响应:会话不存在返回 404,其余按存储错误处理。
func (a *Admin) traceErr(w http.ResponseWriter, err error) {
	if errors.Is(err, errSessionNotFound) {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	a.storeErr(w, err)
}

// parseLimit 解析条数上限:非法或缺失时取默认值,上限做封顶。
func parseLimit(raw string) int64 {
	if raw == "" {
		return traceListDefaultLimit
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return traceListDefaultLimit
	}
	if n > traceListMaxLimit {
		return traceListMaxLimit
	}
	return n
}
