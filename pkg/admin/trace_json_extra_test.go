package admin

import (
	"strings"
	"testing"

	"awatchdog/pkg/config/db"
	"awatchdog/pkg/trace"
)

// jsonTestMsg 造一条用户文本消息(测试夹具)。
func jsonTestMsg(text string) trace.MainlineMessage {
	return trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": text}}}
}

// jsonGzip 把消息序列化成快照并压缩(测试夹具)。
func jsonGzip(t *testing.T, msgs []trace.MainlineMessage) ([]byte, trace.SnapshotLines) {
	t.Helper()
	snap, err := trace.Serialize(msgs)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	gz, err := trace.Compress(snap.NDJSON)
	if err != nil {
		t.Fatalf("Compress() error = %v", err)
	}
	return gz, trace.SnapshotLinesOf(snap.LineHashes, snap.LineSizes)
}

// TestCompressJSON 覆盖压缩对照:前后序列、三段计数与同链前驱。
func TestCompressJSON(t *testing.T) {
	gz0, _ := jsonGzip(t, []trace.MainlineMessage{jsonTestMsg("a"), jsonTestMsg("b")})
	an := &sessionAnalysis{
		steps: []db.ListSessionStepsRow{
			{Seq: 1, NdjsonGzip: gz0},
			{Seq: 2},
		},
		deltas: []trace.Delta{
			{},
			{Kind: trace.DeltaRewrite, Summarized: true, PrevLines: 2, CurLines: 1, Kept: 1, Removed: 1, Added: 1},
		},
		added:     [][]trace.MainlineMessage{nil, {jsonTestMsg("摘要")}},
		prevIdxOf: map[int]int{1: 0},
	}
	got := an.compressJSON(1)
	if got["before_lines"] != 2 || got["after_lines"] != 1 || got["kept"] != 1 || got["removed"] != 1 || got["added"] != 1 {
		t.Errorf("三段计数 = %#v", got)
	}
	if got["from_seq"] != int64(1) {
		t.Errorf("from_seq = %v, want 1", got["from_seq"])
	}
	before, _ := got["before"].([]trace.MainlineMessage)
	if len(before) != 2 || before[1].Blocks[0]["text"] != "b" {
		t.Errorf("before = %#v, want 2 条", got["before"])
	}
	after, _ := got["after"].([]trace.MainlineMessage)
	if len(after) != 1 {
		t.Errorf("after = %#v", got["after"])
	}

	// 没有同链前驱:不给 from_seq 与 before。
	noPrev := &sessionAnalysis{
		steps:  []db.ListSessionStepsRow{{Seq: 1}},
		deltas: []trace.Delta{{}},
		added:  [][]trace.MainlineMessage{{}},
	}
	got = noPrev.compressJSON(0)
	if _, has := got["from_seq"]; has {
		t.Errorf("无前驱却给了 from_seq: %#v", got)
	}
	if _, has := got["before"]; has {
		t.Errorf("无前驱却给了 before: %#v", got)
	}

	// 前驱快照损坏:只省略 before,其余字段照旧。
	badPrev := &sessionAnalysis{
		steps:     []db.ListSessionStepsRow{{Seq: 1, NdjsonGzip: []byte("bad")}, {Seq: 2}},
		deltas:    []trace.Delta{{}, {Kind: trace.DeltaRewrite, Summarized: true}},
		added:     [][]trace.MainlineMessage{{}, {}},
		prevIdxOf: map[int]int{1: 0},
	}
	got = badPrev.compressJSON(1)
	if got["from_seq"] != int64(1) {
		t.Errorf("损坏前驱的 from_seq = %v, want 1", got["from_seq"])
	}
	if _, has := got["before"]; has {
		t.Errorf("损坏前驱不应给 before: %#v", got)
	}
}

// TestStepSkeletonJSONBranches 覆盖单步骨架的细分字段。
func TestStepSkeletonJSONBranches(t *testing.T) {
	kind := "auth"
	t.Run("失败归因", func(t *testing.T) {
		an := &sessionAnalysis{
			steps:     []db.ListSessionStepsRow{{Seq: 1, StatusCode: 401, ErrorKind: &kind}},
			deltas:    []trace.Delta{{Kind: trace.DeltaNew}},
			sideSteps: map[int64]bool{},
		}
		out := an.stepSkeletonJSON(0)
		if out["error_kind"] != "auth" {
			t.Errorf("error_kind = %v, want auth", out["error_kind"])
		}
	})

	t.Run("压缩且同链前驱", func(t *testing.T) {
		an := &sessionAnalysis{
			steps: []db.ListSessionStepsRow{{Seq: 1}, {Seq: 2}},
			deltas: []trace.Delta{{}, {Kind: trace.DeltaRewrite, Summarized: true,
				PrevLines: 8, CurLines: 3}},
			lines:     []trace.SnapshotLines{{Lines: 8}, {Lines: 3}},
			sideSteps: map[int64]bool{},
			prevIdxOf: map[int]int{1: 0},
		}
		out := an.stepSkeletonJSON(1)
		compress, _ := out["compress"].(map[string]any)
		if compress["before_lines"] != 8 || compress["after_lines"] != 3 || compress["from_seq"] != int64(1) {
			t.Errorf("compress = %#v", compress)
		}
	})

	t.Run("压缩但无前驱", func(t *testing.T) {
		an := &sessionAnalysis{
			steps:     []db.ListSessionStepsRow{{Seq: 1}},
			deltas:    []trace.Delta{{Kind: trace.DeltaRewrite, Summarized: true}},
			sideSteps: map[int64]bool{},
		}
		out := an.stepSkeletonJSON(0)
		compress, _ := out["compress"].(map[string]any)
		if compress["from_seq"] != int64(0) {
			t.Errorf("无前驱 from_seq = %v, want 0", compress["from_seq"])
		}
	})

	t.Run("上下文清零", func(t *testing.T) {
		an := &sessionAnalysis{
			steps:     []db.ListSessionStepsRow{{Seq: 1}},
			deltas:    []trace.Delta{{Kind: trace.DeltaChain, Cleared: true}},
			sideSteps: map[int64]bool{},
		}
		if out := an.stepSkeletonJSON(0); out["cleared"] != true {
			t.Errorf("cleared = %v, want true", out["cleared"])
		}
	})

	t.Run("末行更新给出方向", func(t *testing.T) {
		an := &sessionAnalysis{
			steps:     []db.ListSessionStepsRow{{Seq: 1}},
			deltas:    []trace.Delta{{Kind: trace.DeltaFork, TailUpdate: true, TailBefore: 10, TailAfter: 20}},
			sideSteps: map[int64]bool{},
		}
		out := an.stepSkeletonJSON(0)
		if out["tail_update"] != true || out["tail_before"] != 10 || out["tail_after"] != 20 {
			t.Errorf("tail 字段 = %#v", out)
		}
	})

	t.Run("末行长度不可比", func(t *testing.T) {
		an := &sessionAnalysis{
			steps:     []db.ListSessionStepsRow{{Seq: 1}},
			deltas:    []trace.Delta{{Kind: trace.DeltaFork, TailUpdate: true, TailBefore: -1, TailAfter: -1}},
			sideSteps: map[int64]bool{},
		}
		out := an.stepSkeletonJSON(0)
		if out["tail_update"] != true {
			t.Errorf("tail_update = %v, want true", out["tail_update"])
		}
		if _, has := out["tail_before"]; has {
			t.Errorf("长度缺失却给出 tail_before: %#v", out)
		}
	})

	t.Run("旁路步", func(t *testing.T) {
		an := &sessionAnalysis{
			steps:       []db.ListSessionStepsRow{{Seq: 1}},
			deltas:      []trace.Delta{{Kind: trace.DeltaNew}},
			sideSteps:   map[int64]bool{1: true},
			sideChainOf: map[int64]int{1: 2},
		}
		out := an.stepSkeletonJSON(0)
		if out["side"] != true || out["side_chain"] != 2 {
			t.Errorf("旁路字段 = %#v", out)
		}
	})

	t.Run("分歧行数", func(t *testing.T) {
		prevGz, prevLines := jsonGzip(t, []trace.MainlineMessage{jsonTestMsg("a"), jsonTestMsg("b")})
		curGz, curLines := jsonGzip(t, []trace.MainlineMessage{jsonTestMsg("a"), jsonTestMsg("X")})
		_ = curGz
		an := &sessionAnalysis{
			steps:     []db.ListSessionStepsRow{{Seq: 1, NdjsonGzip: prevGz}, {Seq: 2}},
			lines:     []trace.SnapshotLines{prevLines, curLines},
			deltas:    []trace.Delta{{Kind: trace.DeltaNew}, {Kind: trace.DeltaFork, PrefixLen: 1}},
			sideSteps: map[int64]bool{},
			prevIdxOf: map[int]int{1: 0},
			added:     [][]trace.MainlineMessage{nil, nil},
		}
		out := an.stepSkeletonJSON(1)
		if out["diff_lines"] != 1 {
			t.Errorf("diff_lines = %v, want 1", out["diff_lines"])
		}
		delta := out["delta"].(map[string]any)
		if delta["first_diff"] != 1 {
			t.Errorf("first_diff = %v, want 1", delta["first_diff"])
		}
	})
}

// TestDigestJSON 覆盖渐进填充:轮次标题、工具计数、状态分布、间隔与排序。
func TestDigestJSON(t *testing.T) {
	an := &sessionAnalysis{
		steps: []db.ListSessionStepsRow{
			{Seq: 1, Ts: "2026-10-13T10:00:00Z", LatencyMs: 1000},
			{Seq: 2, Ts: "2026-10-13T10:00:05Z", LatencyMs: 1000},
			{Seq: 3, Ts: "2026-10-13T10:00:10Z", LatencyMs: 1000},
		},
		turns: []trace.Turn{
			{Index: 1, StepSeqs: []int64{1, 2, 99}},
			{Index: 2, StepSeqs: []int64{3}},
		},
		sideSteps: map[int64]bool{2: true},
		added: [][]trace.MainlineMessage{
			{jsonTestMsg("第一个问题")},
			{jsonTestMsg("旁路")},
			{jsonTestMsg("第二个问题")},
		},
		tools: []trace.ToolCall{
			{ID: "1", Name: "Zeta", Status: trace.ToolError, IssuedSeq: 1},
			{ID: "2", Name: "Alpha", Status: trace.ToolUnseen, IssuedSeq: 1},
			{ID: "3", Name: "Alpha", Status: trace.ToolOK, IssuedSeq: 3},
			{ID: "4", Name: "Alpha", Status: trace.ToolOK, IssuedSeq: 3},
			{ID: "5", Name: "mcp__github__x", Status: trace.ToolOK, IssuedSeq: 3},
			{ID: "6", Name: "Bash", Status: trace.ToolOK, IssuedSeq: 3},
		},
	}
	got := an.digestJSON()

	byTurn, _ := got["by_turn"].([]map[string]any)
	if len(byTurn) != 2 {
		t.Fatalf("by_turn = %#v, want 2 轮", byTurn)
	}
	if byTurn[0]["title"] != "第一个问题" || byTurn[0]["calls"] != 2 {
		t.Errorf("首轮 = %#v", byTurn[0])
	}
	// 旁路步不参与标题与工具计数。
	if byTurn[1]["title"] != "第二个问题" || byTurn[1]["calls"] != 4 {
		t.Errorf("次轮 = %#v", byTurn[1])
	}

	totals, _ := got["totals"].(map[string]int)
	if totals["calls"] != 6 || totals["ok"] != 4 || totals["err"] != 1 || totals["unseen"] != 1 {
		t.Errorf("totals = %#v", totals)
	}

	byStep, _ := got["by_step"].([]map[string]any)
	if len(byStep) != 2 || byStep[0]["seq"] != int64(1) || byStep[1]["seq"] != int64(3) {
		t.Fatalf("by_step = %#v, want 按 seq 升序", byStep)
	}
	if byStep[0]["calls"] != 2 || byStep[1]["calls"] != 4 {
		t.Errorf("by_step 计数 = %#v", byStep)
	}

	tools, _ := got["tools"].([]map[string]any)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool["name"].(string))
	}
	// 调用数降序,同数按名字升序。
	want := []string{"Alpha", "Bash", "Zeta", "mcp__github__x"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("工具排序 = %v, want %v", names, want)
	}
	alpha := tools[0]
	if alpha["calls"] != 3 || alpha["ok"] != 2 || alpha["unseen"] != 1 || alpha["err"] != 0 {
		t.Errorf("Alpha 聚合 = %#v", alpha)
	}
	if turns, _ := alpha["turns"].([]int); len(turns) != 2 || turns[0] != 1 || turns[1] != 2 {
		t.Errorf("Alpha 轮次 = %#v, want [1 2]", alpha["turns"])
	}
	// Alpha 的调用只有未见结果(不计间隔)与末步(无后继),故无间隔。
	if gaps, _ := alpha["gaps_ms"].([]int64); len(gaps) != 0 {
		t.Errorf("Alpha 间隔 = %#v, want 0 条", alpha["gaps_ms"])
	}
	zeta := tools[2]
	if gaps, _ := zeta["gaps_ms"].([]int64); len(gaps) != 1 || gaps[0] != 4000 {
		t.Errorf("Zeta 间隔 = %#v, want [4000]", zeta["gaps_ms"])
	}
	if tools[3]["source"] != "MCP" {
		t.Errorf("MCP 来源 = %v", tools[3]["source"])
	}
}

// TestStepDetailJSON 覆盖单步详情:全量切换、压缩对照与全量解析失败回退。
func TestStepDetailJSON(t *testing.T) {
	gz0, lines0 := jsonGzip(t, []trace.MainlineMessage{jsonTestMsg("a"), jsonTestMsg("b")})
	gz1, lines1 := jsonGzip(t, []trace.MainlineMessage{jsonTestMsg("a"), jsonTestMsg("b"), jsonTestMsg("c")})
	an := &sessionAnalysis{
		steps: []db.ListSessionStepsRow{
			{Seq: 1, NdjsonGzip: gz0},
			{Seq: 2, NdjsonGzip: gz1},
		},
		lines:     []trace.SnapshotLines{lines0, lines1},
		deltas:    []trace.Delta{{Kind: trace.DeltaNew}, {Kind: trace.DeltaAdded, PrefixLen: 2}},
		added:     [][]trace.MainlineMessage{nil, {jsonTestMsg("c")}},
		prevIdxOf: map[int]int{1: 0},
		tools:     []trace.ToolCall{{ID: "t", Name: "Read", Status: trace.ToolOK, IssuedSeq: 1}},
	}
	got := an.stepDetailJSON(1, true, 3)
	msgs, _ := got["messages"].([]trace.MainlineMessage)
	if len(msgs) != 3 {
		t.Errorf("full=1 消息数 = %d, want 3(全量)", len(msgs))
	}
	if _, has := got["compress"]; has {
		t.Errorf("非压缩步不应有 compress: %#v", got)
	}

	// 压缩步:summary 详情带 compress 对照。
	an.deltas[1] = trace.Delta{Kind: trace.DeltaRewrite, Summarized: true, PrevLines: 2, CurLines: 3}
	an.deltas[0] = trace.Delta{Kind: trace.DeltaNew}
	got = an.stepDetailJSON(1, false, 3)
	if _, has := got["compress"]; !has {
		t.Errorf("压缩步缺 compress: %#v", got)
	}

	// 全量解析失败:回退到新增消息,不报错。
	an.steps[1].NdjsonGzip = []byte("bad")
	got = an.stepDetailJSON(1, true, 3)
	fallback, _ := got["messages"].([]trace.MainlineMessage)
	if len(fallback) != 1 || fallback[0].Blocks[0]["text"] != "c" {
		t.Errorf("解析失败回退 = %#v, want 新增消息", got["messages"])
	}
}

// TestKindStoryLabel 覆盖分类叙述用词,含未知分类回退。
func TestKindStoryLabel(t *testing.T) {
	cases := []struct {
		kind     trace.DeltaKind
		firstIdx int
		want     string
	}{
		{trace.DeltaNew, 0, "会话首步"},
		{trace.DeltaNew, 3, "首步"},
		{trace.DeltaAdded, 0, "追加"},
		{trace.DeltaRetry, 0, "重复"},
		{trace.DeltaFork, 0, "分叉"},
		{trace.DeltaRewrite, 0, "改写"},
		{trace.DeltaChain, 0, "无重合"},
		{trace.DeltaKind("weird"), 0, "weird"},
	}
	for _, tc := range cases {
		if got := kindStoryLabel(tc.kind, tc.firstIdx); got != tc.want {
			t.Errorf("kindStoryLabel(%q, %d) = %q, want %q", tc.kind, tc.firstIdx, got, tc.want)
		}
	}
}

// TestLineChangeText 覆盖行数变化的四种文案。
func TestLineChangeText(t *testing.T) {
	cases := []struct {
		added, removed int
		want           string
	}{
		{2, 3, "移除 3 行 · 新增 2 行"},
		{2, 0, "新增 2 行"},
		{0, 3, "移除 3 行"},
		{0, 0, "无行数变化"},
	}
	for _, tc := range cases {
		if got := lineChangeText(tc.added, tc.removed); got != tc.want {
			t.Errorf("lineChangeText(%d,%d) = %q, want %q", tc.added, tc.removed, got, tc.want)
		}
	}
}

// TestFineMark 覆盖细分标记:压缩、清零、末行更新与无标记。
func TestFineMark(t *testing.T) {
	cases := []struct {
		name  string
		delta trace.Delta
		want  string
	}{
		{"压缩", trace.Delta{Summarized: true, PrevLines: 5, CurLines: 2}, "压缩 5→2 行"},
		{"清零", trace.Delta{Cleared: true}, "上下文清零"},
		{"末行更新", trace.Delta{TailUpdate: true}, "末行更新"},
		{"无", trace.Delta{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, seq := fineMark(tc.delta, 7)
			if got != tc.want {
				t.Errorf("fineMark = %q, want %q", got, tc.want)
			}
			if tc.want != "" && seq != 7 {
				t.Errorf("seq = %d, want 7", seq)
			}
			if tc.want == "" && seq != 0 {
				t.Errorf("无标记 seq = %d, want 0", seq)
			}
		})
	}
}

// TestTurnTitleBranches 覆盖标题提取:非字符串块、空文本与空白裁剪。
func TestTurnTitleBranches(t *testing.T) {
	msgs := []trace.MainlineMessage{
		{Role: "user", Blocks: []trace.MainlineBlock{{"text": 42}, {"text": ""}, {"text": "  题目  "}}},
	}
	if got := turnTitle(msgs); got != "题目" {
		t.Errorf("turnTitle = %q, want 题目", got)
	}
	if got := turnTitle([]trace.MainlineMessage{{Role: "assistant", Blocks: []trace.MainlineBlock{{"text": "忽略"}}}}); got != "" {
		t.Errorf("无 user 消息 = %q, want 空", got)
	}
}

// TestGapAfterMissing 覆盖无后继与无匹配步序号。
func TestGapAfterMissing(t *testing.T) {
	an := &sessionAnalysis{steps: []db.ListSessionStepsRow{{Seq: 1}, {Seq: 2}}}
	if _, ok := an.gapAfter(99); ok {
		t.Error("未知序号不应给出间隔")
	}
	if _, ok := an.gapAfter(2); ok {
		t.Error("末步不应给出间隔")
	}
}

// TestAllMessagesBadGzip 覆盖快照解压失败。
func TestAllMessagesBadGzip(t *testing.T) {
	an := &sessionAnalysis{steps: []db.ListSessionStepsRow{{NdjsonGzip: []byte("bad")}}}
	if _, err := an.allMessages(0); err == nil {
		t.Fatal("损坏快照应返回错误")
	}
}

// TestGapMsInvalidCurrent 覆盖后一步时间戳非法。
func TestGapMsInvalidCurrent(t *testing.T) {
	prev := db.ListSessionStepsRow{Ts: "2026-10-13T09:00:00Z", LatencyMs: 1000}
	cur := db.ListSessionStepsRow{Ts: "不是时间"}
	if got := gapMs(prev, cur); got != 0 {
		t.Errorf("非法当前时间 = %d, want 0", got)
	}
}

// TestDiffHeaderZeroCount 覆盖零行块的插入点写法。
func TestDiffHeaderZeroCount(t *testing.T) {
	got := diffHeader(trace.DiffHunk{}, 0, 0)
	if got != "@@ -0,0 +0,0 @@" {
		t.Errorf("diffHeader(零行) = %q, want @@ -0,0 +0,0 @@", got)
	}
}

// TestSumRowBytesSkipsNonInt 覆盖字节数只累加正整数。
func TestSumRowBytesSkipsNonInt(t *testing.T) {
	rows := []map[string]any{
		{"bytes": 5},
		{"bytes": -3},
		{"bytes": "x"},
		{},
	}
	if got := sumRowBytes(rows); got != 5 {
		t.Errorf("sumRowBytes = %d, want 5", got)
	}
}

// TestTurnAxisUnknownSeq 覆盖未知步序号被跳过。
func TestTurnAxisUnknownSeq(t *testing.T) {
	an := &sessionAnalysis{steps: []db.ListSessionStepsRow{{Seq: 1, Ts: "2026-10-13T10:00:00Z", LatencyMs: 1000}}}
	offsets, total, ok := an.turnAxis([]int64{1, 99}, indexSeq(an.steps))
	if !ok || total != 1000 || offsets[1] != 0 {
		t.Errorf("turnAxis = %v,%d,%v", offsets, total, ok)
	}
}

// TestTurnAxisNegativeTotal 覆盖总时长为负时轴不可用。
func TestTurnAxisNegativeTotal(t *testing.T) {
	an := &sessionAnalysis{steps: []db.ListSessionStepsRow{
		{Seq: 1, Ts: "2026-10-13T10:00:01Z", LatencyMs: -5000},
	}}
	if _, _, ok := an.turnAxis([]int64{1}, indexSeq(an.steps)); ok {
		t.Error("负总时长时轴应不可用")
	}
}

// TestTurnStoryUnknownSeq 覆盖叙述跳过未知步序号。
func TestTurnStoryUnknownSeq(t *testing.T) {
	an := &sessionAnalysis{
		steps:     []db.ListSessionStepsRow{{Seq: 1, Ts: "2026-10-13T10:00:00Z", StatusCode: 200}},
		deltas:    []trace.Delta{{Kind: trace.DeltaNew, Added: 1}},
		sideSteps: map[int64]bool{},
	}
	parts := an.turnStory([]int64{1, 99}, indexSeq(an.steps))
	if len(parts) != 2 || parts[0]["text"] != "会话首步" {
		t.Errorf("story = %#v", parts)
	}
}

// TestHarnessBySeqPrevInvalid 覆盖前一步时间戳非法时跳过。
func TestHarnessBySeqPrevInvalid(t *testing.T) {
	an := &sessionAnalysis{
		steps: []db.ListSessionStepsRow{
			{Seq: 1, Ts: "不是时间", LatencyMs: 1000},
			{Seq: 2, Ts: "2026-10-13T10:00:05Z", LatencyMs: 500},
		},
		prevIdxOf: map[int]int{1: 0},
	}
	if got := an.harnessBySeq(); len(got) != 0 {
		t.Errorf("前一步时间戳非法应无 harness: %v", got)
	}
}

// TestSkeletonJSONUnknownSeqAndSummarized 覆盖骨架跳过未知步、统计压缩次数。
func TestSkeletonJSONUnknownSeqAndSummarized(t *testing.T) {
	an := &sessionAnalysis{
		sessionID: "1:s", source: "explicit", note: "n",
		firstSeen: "2026-10-13T10:00:00Z", lastSeen: "2026-10-13T10:00:05Z",
		steps: []db.ListSessionStepsRow{
			{Seq: 1, Ts: "2026-10-13T10:00:00Z", LatencyMs: 1000},
			{Seq: 2, Ts: "2026-10-13T10:00:05Z", LatencyMs: 1000},
		},
		deltas: []trace.Delta{
			{Kind: trace.DeltaNew, Added: 1},
			{Kind: trace.DeltaRewrite, Summarized: true, PrevLines: 5, CurLines: 2, PrefixLen: 1, Added: 1},
		},
		lines:       []trace.SnapshotLines{{Lines: 1}, {Lines: 1}},
		turns:       []trace.Turn{{Index: 1, StepSeqs: []int64{1, 2, 99}}},
		sideSteps:   map[int64]bool{},
		sideChainOf: map[int64]int{},
		prevIdxOf:   map[int]int{1: 0},
		added:       [][]trace.MainlineMessage{nil, nil},
	}
	out := an.skeletonJSON()
	turns, _ := out["turns"].([]map[string]any)
	if len(turns) != 1 {
		t.Fatalf("turns = %#v", out["turns"])
	}
	steps, _ := turns[0]["steps"].([]map[string]any)
	if len(steps) != 2 {
		t.Errorf("未知步应被跳过: %d 步", len(steps))
	}
	if turns[0]["compresses"] != 1 {
		t.Errorf("压缩计数 = %v, want 1", turns[0]["compresses"])
	}
	// 第 2 步带压缩对照。
	compress, _ := steps[1]["compress"].(map[string]any)
	if compress == nil || compress["from_seq"] != int64(1) {
		t.Errorf("第 2 步 compress = %#v", steps[1]["compress"])
	}
}

// TestHarnessBySeqCurrentInvalid 覆盖后继时间戳非法时跳过。
func TestHarnessBySeqCurrentInvalid(t *testing.T) {
	an := &sessionAnalysis{
		steps: []db.ListSessionStepsRow{
			{Seq: 1, Ts: "2026-10-13T10:00:00Z", LatencyMs: 1000},
			{Seq: 2, Ts: "不是时间", LatencyMs: 500},
		},
		prevIdxOf: map[int]int{1: 0},
	}
	if got := an.harnessBySeq(); len(got) != 0 {
		t.Errorf("后继时间戳非法应无 harness: %v", got)
	}
}

// TestGapMsNegative 覆盖后一步早于前一步结束:间隔归零。
func TestGapMsNegative(t *testing.T) {
	prev := db.ListSessionStepsRow{Ts: "2026-10-13T10:00:10Z", LatencyMs: 5000}
	cur := db.ListSessionStepsRow{Ts: "2026-10-13T10:00:11Z"}
	if got := gapMs(prev, cur); got != 0 {
		t.Errorf("倒挂间隔 = %d, want 0", got)
	}
}
