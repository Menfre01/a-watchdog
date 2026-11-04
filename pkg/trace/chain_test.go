package trace

import (
	"strconv"
	"strings"
	"testing"
)

// chainShape 把链归属结果压成可比较的形状:`主链=[0,1,3] 旁路=[2,4]`。
func chainShape(chains []SessionChain) string {
	parts := make([]string, 0, len(chains))
	for _, c := range chains {
		label := "主链"
		if c.Kind == ChainSide {
			label = "旁路"
		}
		idx := make([]string, 0, len(c.Steps))
		for _, i := range c.Steps {
			idx = append(idx, strconv.Itoa(i))
		}
		parts = append(parts, label+"=["+strings.Join(idx, ",")+"]")
	}
	return strings.Join(parts, " ")
}

// chainOf 构造一条链的快照:给定首条消息指纹与行文本(测试夹具)。
func chainOf(t *testing.T, sysFP string, texts ...string) SnapshotLines {
	t.Helper()
	s := linesOf(t, texts...)
	s.SystemFP = sysFP
	return s
}

// TestAnalyzeSessionByFingerprint 覆盖唯一的判据:首条消息指纹相同即同链。
// 主对话 / 安全监控 / 子 agent 的指纹各不相同,交错出现也各自成链。
func TestAnalyzeSessionByFingerprint(t *testing.T) {
	steps := []SnapshotLines{
		chainOf(t, "main", "sys", "u1"),
		chainOf(t, "monitor", "sec-sys", "t1"),
		chainOf(t, "main", "sys", "u1", "a1"),
		chainOf(t, "agent", "agent-sys", "p1"),
		chainOf(t, "monitor", "sec-sys", "t2"),
		chainOf(t, "main", "sys", "u1", "a1", "u2"),
		chainOf(t, "agent", "agent-sys", "p2"),
	}
	chains := AnalyzeSession(steps)
	if got, want := chainShape(chains), "主链=[0,2,5] 旁路=[1,4] 旁路=[3,6]"; got != want {
		t.Fatalf("链归属 = %s, want %s", got, want)
	}
}

// TestAnalyzeSessionDoesNotCompareChainLength 覆盖主链门槛:主链只有 2 步,
// 更长的 agent 链仍是旁路——门槛只看持续性,不比较步数。
func TestAnalyzeSessionDoesNotCompareChainLength(t *testing.T) {
	steps := []SnapshotLines{
		chainOf(t, "main", "sys", "u1"),                          // 首步
		chainOf(t, "agent", "agent-sys", "p1", "p2", "p3", "p4"), // 更长
		chainOf(t, "main", "sys", "u1", "a1"),
	}
	chains := AnalyzeSession(steps)
	if chains[0].Kind != ChainMain || len(chains[0].Steps) != 2 {
		t.Fatalf("首条链 = %+v, want 主链", chains[0])
	}
	if chains[1].Kind != ChainSide {
		t.Errorf("第二条链类型 = %q, want side", chains[1].Kind)
	}
}

// TestAnalyzeSessionCrossChainNotRetry 覆盖重复组按链统计:
// 不同链上的相同内容不算重复。
func TestAnalyzeSessionCrossChainNotRetry(t *testing.T) {
	same := []string{"sys", "same"}
	steps := []SnapshotLines{
		chainOf(t, "a", same...),
		chainOf(t, "b", same...), // 内容相同但指纹不同:另一条链的首步,不是重复
	}
	chains := AnalyzeSession(steps)
	if len(chains) != 2 {
		t.Fatalf("链数 = %d, want 2", len(chains))
	}
	if chains[1].Deltas[0].Kind == DeltaRetry {
		t.Errorf("跨链的相同内容被当成重复: %+v", chains[1].Deltas[0])
	}
}

// TestAnalyzeSessionEmptyFingerprintDegrades 覆盖空指纹:视为同类,退化为单链。
func TestAnalyzeSessionEmptyFingerprintDegrades(t *testing.T) {
	steps := []SnapshotLines{
		linesOf(t, "u1"),
		linesOf(t, "u1", "a1"),
	}
	chains := AnalyzeSession(steps)
	if got, want := chainShape(chains), "主链=[0,1]"; got != want {
		t.Fatalf("链归属 = %s, want %s(无指纹时退化为单链)", got, want)
	}
}

// TestAnalyzeSessionEmpty 覆盖空输入。
func TestAnalyzeSessionEmpty(t *testing.T) {
	if got := AnalyzeSession(nil); got != nil {
		t.Errorf("空输入 = %+v, want nil", got)
	}
}

// TestFirstDelta 覆盖链首步的增量构造。
func TestFirstDelta(t *testing.T) {
	d := firstDelta(linesOf(t, "a", "b"))
	if d.Kind != DeltaNew || d.Added != 2 || d.CurLines != 2 {
		t.Errorf("首步增量 = %+v", d)
	}
	if empty := firstDelta(SnapshotLines{}); empty.Kind != DeltaChain || empty.Added != 0 {
		t.Errorf("空快照首步 = %+v", empty)
	}
}

// chainOf2 构造带两级指纹的快照(测试夹具)。
func chainOf2(t *testing.T, sysFP, userFP string, texts ...string) SnapshotLines {
	t.Helper()
	s := linesOf(t, texts...)
	s.SystemFP = sysFP
	s.UserFP = userFP
	return s
}

// TestAnalyzeSessionUserFingerprintSplitsInstances 覆盖二级判据:
// 同一 system 指纹、不同首条 user 前缀 = 不同的并行实例。
// 实测形态:3 个 subagent 共用同一套 system prompt,但任务描述不同。
func TestAnalyzeSessionUserFingerprintSplitsInstances(t *testing.T) {
	steps := []SnapshotLines{
		chainOf2(t, "main", "u-hi", "sys", "u1"),
		chainOf2(t, "agent", "task-A", "agent-sys", "a1"),
		chainOf2(t, "agent", "task-B", "agent-sys", "b1"),
		chainOf2(t, "main", "u-hi", "sys", "u1", "a1"),
		chainOf2(t, "agent", "task-A", "agent-sys", "a2"),
		chainOf2(t, "agent", "task-B", "agent-sys", "b2"),
	}
	chains := AnalyzeSession(steps)
	if got, want := chainShape(chains), "主链=[0,3] 旁路=[1,4] 旁路=[2,5]"; got != want {
		t.Fatalf("链归属 = %s, want %s", got, want)
	}
}

// TestAnalyzeSessionMissingUserFingerprintFallsBack 覆盖缺 user 指纹:
// 只用 system 指纹判定,不因缺一级而分裂。
func TestAnalyzeSessionMissingUserFingerprintFallsBack(t *testing.T) {
	steps := []SnapshotLines{
		chainOf(t, "main", "sys", "u1"),
		chainOf(t, "main", "sys", "u1", "a1"),
	}
	// chainOf 只设 SystemFP,UserFP 为空 → 退回 system 判据
	chains := AnalyzeSession(steps)
	if got, want := chainShape(chains), "主链=[0,1]"; got != want {
		t.Fatalf("链归属 = %s, want %s", got, want)
	}
}

// TestAnalyzeSessionSideChainNotCleared 覆盖「上下文清零」的作用域:
// 该语义只对主对话成立(/clear 是用户对主对话的操作)。旁路请求本身往往
// 就是 3 行左右,跨 turn 时前缀不共享会恰好命中清零判据,属误报。
func TestAnalyzeSessionSideChainNotCleared(t *testing.T) {
	steps := []SnapshotLines{
		chainOf(t, "main", "sys", "u1", "a1"),
		chainOf(t, "monitor", "sec-sys", "t1"),
		chainOf(t, "monitor", "sec-sys", "t2"), // 与上一步无共同前缀,且 3 行
		chainOf(t, "main", "sys", "u1", "a1", "u2"),
	}
	chains := AnalyzeSession(steps)
	side := chains[1]
	if len(side.Steps) != 2 {
		t.Fatalf("旁路链步数 = %d, want 2", len(side.Steps))
	}
	for k, d := range side.Deltas {
		if d.Cleared {
			t.Errorf("旁路链第 %d 步被标为上下文清零: %+v", k, d)
		}
	}
}

// TestAnalyzeSessionMainChainCanBeCleared 覆盖主链的清零仍生效。
func TestAnalyzeSessionMainChainCanBeCleared(t *testing.T) {
	steps := []SnapshotLines{
		chainOf(t, "main", "sys", "u1", "a1", "u2", "a2"),
		chainOf(t, "main", "clean", "全新内容"), // 前缀不共享且 1 行
	}
	chains := AnalyzeSession(steps)
	// 主链只有一步:第二条是独立的链(指纹不同),故此处验证的是首链的判据仍可用。
	if chains[0].Kind != ChainMain {
		t.Fatalf("首条链类型 = %q", chains[0].Kind)
	}
	// 清零判据本身仍生效(直接验证 Compare)。
	if d := Compare(linesOf(t, "a", "b", "c"), linesOf(t, "x")); !d.Cleared {
		t.Errorf("Compare 的清零判据失效: %+v", d)
	}
}

// TestAnalyzeSessionNoSystemKeepsSingleChain 覆盖无 system 消息的客户端:
// 它不发 system prompt,首条 user 消息每轮都变——若据此拆分,每一步都会成为
// 独立的链,整个会话被切成碎片。缺少类型指纹时应退化为单链。
func TestAnalyzeSessionNoSystemKeepsSingleChain(t *testing.T) {
	steps := []SnapshotLines{
		{LineHashes: linesOf(t, "第一问").LineHashes, UserFP: "u1", Lines: 1},
		{LineHashes: linesOf(t, "第二问").LineHashes, UserFP: "u2", Lines: 1},
		{LineHashes: linesOf(t, "第三问").LineHashes, UserFP: "u3", Lines: 1},
	}
	chains := AnalyzeSession(steps)
	if got, want := chainShape(chains), "主链=[0,1,2]"; got != want {
		t.Fatalf("链归属 = %s, want %s(无 system 指纹时不得按 user 前缀拆分)", got, want)
	}
}
