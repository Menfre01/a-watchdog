package trace

import (
	"strconv"
	"strings"
	"testing"
)

// linesOf 用给定的行文本构造比对输入:每行是一条只含 text 块的消息。
func linesOf(t *testing.T, texts ...string) SnapshotLines {
	t.Helper()
	msgs := make([]MainlineMessage, 0, len(texts))
	for _, text := range texts {
		msgs = append(msgs, MainlineMessage{
			Role:   "user",
			Blocks: []MainlineBlock{{"type": "text", "text": text}},
		})
	}
	snap, err := Serialize(msgs)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	return snap.LinesInfo()
}

// filled 返回 n 行等长文本(用于制造"行数不变、长度骤降"的内容压缩形态)。
func filled(prefix string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, prefix+strings.Repeat("x", 200))
	}
	return out
}

// TestCompareTable 覆盖分类主路径与三段区间。
func TestCompareTable(t *testing.T) {
	cases := []struct {
		name        string
		prev, cur   []string
		wantKind    DeltaKind
		wantPrefix  int
		wantKept    int
		wantRemoved int
		wantAdded   int
	}{
		{"纯追加", []string{"a", "b"}, []string{"a", "b", "c"}, DeltaAdded, 2, 2, 0, 1},
		{"完全相同", []string{"a", "b"}, []string{"a", "b"}, DeltaRetry, 2, 2, 0, 0},
		{"尾部替换", []string{"a", "b", "c", "d"}, []string{"a", "b", "X", "Y"}, DeltaFork, 2, 2, 2, 2},
		{"尾部删除", []string{"a", "b", "c", "d"}, []string{"a", "b"}, DeltaFork, 2, 2, 2, 0},
		{"历史替换", []string{"a", "b", "c", "d"}, []string{"S", "c", "d"}, DeltaRewrite, 0, 2, 2, 1},
		{"无共同前缀无共享尾部", []string{"a", "b"}, []string{"X", "Y"}, DeltaChain, 0, 0, 2, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Compare(linesOf(t, tc.prev...), linesOf(t, tc.cur...))
			if got.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.PrefixLen != tc.wantPrefix {
				t.Errorf("PrefixLen = %d, want %d", got.PrefixLen, tc.wantPrefix)
			}
			if got.Kept != tc.wantKept || got.Removed != tc.wantRemoved || got.Added != tc.wantAdded {
				t.Errorf("三段区间 = 保留 %d/移除 %d/新增 %d, want %d/%d/%d",
					got.Kept, got.Removed, got.Added, tc.wantKept, tc.wantRemoved, tc.wantAdded)
			}
			if got.FirstDiff != got.PrefixLen {
				t.Errorf("FirstDiff = %d, want %d", got.FirstDiff, got.PrefixLen)
			}
		})
	}
}

// TestCompareSummarized 覆盖摘要压缩:改写形态且行数骤降。
func TestCompareSummarized(t *testing.T) {
	prev := linesOf(t, "a", "b", "c", "d", "e", "f", "g", "h", "i", "j")
	cur := linesOf(t, "SUMMARY", "i", "j") // 行数 3 < 10×0.6,且保留尾部 2 行
	got := Compare(prev, cur)
	if got.Kind != DeltaRewrite {
		t.Fatalf("Kind = %q, want rewrite", got.Kind)
	}
	if !got.Summarized {
		t.Errorf("未标记摘要压缩: %+v", got)
	}
	if got.SharedSuffix != 2 || got.Kept != 2 {
		t.Errorf("共享尾部 = %d / 保留 = %d, want 2/2", got.SharedSuffix, got.Kept)
	}
	// 行数未骤降的改写不标记。
	mild := Compare(prev, linesOf(t, "S", "d", "e", "f", "g", "h", "i", "j"))
	if mild.Summarized {
		t.Errorf("行数未骤降却标记摘要压缩: %+v", mild)
	}
}

// TestCompareRecordsDivergedSizes 覆盖分叉的长度事实:首个分歧行在两侧的字节数。
// 指纹只能说明「这一行变了」,长度只能说明「变长还是变短」——两者都不足以
// 断言压缩或改写,因此只记录事实供界面展示对照。
func TestCompareRecordsDivergedSizes(t *testing.T) {
	long := []string{"head"}
	long = append(long, filled("payload", 3)...)
	prev := linesOf(t, long...)

	short := []string{"head", "[Old tool result content cleared]", "b", "c"}
	cur := linesOf(t, short...)

	got := Compare(prev, cur)
	if got.Kind != DeltaFork {
		t.Fatalf("Kind = %q, want fork", got.Kind)
	}
	if got.FirstDiff != 1 {
		t.Fatalf("首个分歧行 = %d, want 1", got.FirstDiff)
	}
	// 该行在两侧的字节数:原行很长,新行是短占位文本。
	if got.PrevDiffSize <= 0 || got.CurDiffSize <= 0 {
		t.Fatalf("分歧行长度未记录: %d → %d", got.PrevDiffSize, got.CurDiffSize)
	}
	if got.CurDiffSize >= got.PrevDiffSize {
		t.Errorf("长度方向不符: %d → %d", got.PrevDiffSize, got.CurDiffSize)
	}
}

// TestCompareNoDivergedOnPureAppend 覆盖纯追加:没有分歧行,不记录长度。
func TestCompareNoDivergedOnPureAppend(t *testing.T) {
	prev := linesOf(t, "a", "b")
	cur := linesOf(t, "a", "b", "c")
	got := Compare(prev, cur)
	if got.Kind != DeltaAdded {
		t.Fatalf("Kind = %q, want added", got.Kind)
	}
	if got.PrevDiffSize != 0 || got.CurDiffSize != 0 {
		t.Errorf("纯追加不应记录分歧长度: %d → %d", got.PrevDiffSize, got.CurDiffSize)
	}
}

// TestCompareTailUpdate 覆盖末行更新:只剩末行不同、行数不变,方向不限。
//
// 判据不能限定方向。真实流量里旁路请求会重写自己的转录行,变长与变短都出现;
// 只认变长会把变短的变化误标成笼统的「分叉」。
func TestCompareTailUpdate(t *testing.T) {
	base := []string{"sys", "固定内容", "转录-中等长度"}

	cases := []struct {
		name     string
		cur      []string
		wantTail bool
		wantWord string // 理由里应出现的方向词
	}{
		{"末行变长", []string{"sys", "固定内容", "转录-变长了很多很多内容"}, true, "扩到"},
		{"末行变短", []string{"sys", "固定内容", "短"}, true, "缩到"},
		// 等长改写仍属末行更新,只是方向为「不变」——单独在 TestCompareTailUpdateEqualSize 覆盖。
		{"倒数第二行也变", []string{"sys", "改了", "转录-中等长度"}, false, ""},
		{"行数变化", []string{"sys", "固定内容", "转录-中等长度", "新增行"}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Compare(linesOf(t, base...), linesOf(t, tc.cur...))
			if tc.name == "行数变化" {
				// 行数增加且只有末行不同 → 归「追加」,不是末行更新。
				if got.Kind != DeltaAdded {
					t.Fatalf("Kind = %q, want added", got.Kind)
				}
				if got.TailUpdate {
					t.Errorf("追加形态不应标记末行更新: %+v", got)
				}
				return
			}
			if got.Kind != DeltaFork {
				t.Fatalf("Kind = %q, want fork", got.Kind)
			}
			if got.TailUpdate != tc.wantTail {
				t.Fatalf("TailUpdate = %v, want %v(%+v)", got.TailUpdate, tc.wantTail, got)
			}
			if !tc.wantTail {
				return
			}
			// 理由给出方向与长度对比,便于判断变了多少。
			note := got.Note()
			if !strings.Contains(note, "末行内容从") || !strings.Contains(note, tc.wantWord) {
				t.Errorf("判定理由 = %q, want 含 %q", note, tc.wantWord)
			}
			if got.TailBefore <= 0 || got.TailAfter <= 0 {
				t.Errorf("长度对比 = %d → %d, want 两侧都有值", got.TailBefore, got.TailAfter)
			}
		})
	}
}

// TestCompareTailUpdateEqualSize 覆盖末行字节数不变的改写:
// 长度无变化时理由不该编造方向,只说明被改写。
func TestCompareTailUpdateEqualSize(t *testing.T) {
	prev := linesOf(t, "sys", "固定", "AAAA")
	cur := linesOf(t, "sys", "固定", "BBBB")
	got := Compare(prev, cur)
	if !got.TailUpdate {
		t.Fatalf("未标记末行更新: %+v", got)
	}
	if got.TailBefore != got.TailAfter {
		t.Fatalf("长度应相同: %d vs %d", got.TailBefore, got.TailAfter)
	}
	if note := got.Note(); !strings.Contains(note, "字节数不变") {
		t.Errorf("判定理由 = %q, want 含「字节数不变」", note)
	}
}

// TestCompareCleared 覆盖上下文清零:无重合且几乎清空。
func TestCompareCleared(t *testing.T) {
	prev := linesOf(t, "a", "b", "c", "d", "e")
	got := Compare(prev, linesOf(t, "全新任务"))
	if got.Kind != DeltaChain {
		t.Fatalf("Kind = %q, want chain", got.Kind)
	}
	if !got.Cleared {
		t.Errorf("未标记上下文清零: %+v", got)
	}
	// 无重合但行数较多时不标记清零。
	big := Compare(prev, linesOf(t, "x1", "x2", "x3", "x4", "x5"))
	if big.Cleared {
		t.Errorf("行数较多却标记清零: %+v", big)
	}
}

// TestAnalyzeRetryGroups 覆盖重试组:非重复起始步 + 连续重复步构成一组。
func TestAnalyzeRetryGroups(t *testing.T) {
	snaps := []SnapshotLines{
		linesOf(t, "u1"),                         // 0:首步
		linesOf(t, "u1", "a1"),                   // 1:追加
		linesOf(t, "u1", "a1", "u2"),             // 2:追加(重试组的起始尝试)
		linesOf(t, "u1", "a1", "u2"),             // 3:重复
		linesOf(t, "u1", "a1", "u2"),             // 4:重复
		linesOf(t, "u1", "a1", "u2", "a2"),       // 5:追加(组结束)
		linesOf(t, "u1", "a1", "u2", "a2", "u3"), // 6:追加
	}
	deltas := Analyze(snaps)
	if len(deltas) != len(snaps) {
		t.Fatalf("增量数 = %d, want %d", len(deltas), len(snaps))
	}
	if deltas[0].Kind != DeltaNew {
		t.Errorf("首步 = %q, want new", deltas[0].Kind)
	}
	// 组 = [2,3,4]:尝试 1/3, 2/3, 3/3。
	wantOf := map[int]int{2: 3, 3: 3, 4: 3, 5: 1, 1: 1}
	for idx, want := range wantOf {
		if deltas[idx].RetryOf != want {
			t.Errorf("step %d 的组大小 = %d, want %d", idx, deltas[idx].RetryOf, want)
		}
	}
	for idx, wantN := range map[int]int{2: 1, 3: 2, 4: 3} {
		if deltas[idx].RetryN != wantN {
			t.Errorf("step %d 的组内序号 = %d, want %d", idx, deltas[idx].RetryN, wantN)
		}
	}
}

// TestAnalyzeEmpty 覆盖空序列。
func TestAnalyzeEmpty(t *testing.T) {
	if got := Analyze(nil); got != nil {
		t.Errorf("空序列 = %v, want nil", got)
	}
}

// TestCompareEmptySequences 覆盖空快照:空 vs 有内容。
func TestCompareEmptySequences(t *testing.T) {
	empty := SnapshotLines{}
	got := Compare(empty, linesOf(t, "a"))
	if got.Kind != DeltaAdded {
		t.Errorf("空→有内容 = %q, want added(全部新增)", got.Kind)
	}
	if again := Compare(empty, empty); again.Kind != DeltaRetry {
		t.Errorf("空→空 = %q, want retry", again.Kind)
	}
}

// TestSharedSuffixScanLimit 覆盖超长序列放弃尾部匹配:宁可按无重合处理,
// 也不做 O(n²) 的硬算(判据只影响分类徽标)。
func TestSharedSuffixScanLimit(t *testing.T) {
	long := make([]string, maxSuffixScanLines+1)
	for i := range long {
		long[i] = "line-" + strconv.Itoa(i)
	}
	prev := linesOf(t, long...)
	// 两侧都超过扫描上限:此时放弃匹配,而不是做 O(n²) 的硬算。
	cur := linesOf(t, append([]string{"SUMMARY"}, long...)...)
	got := Compare(prev, cur)
	if got.SharedSuffix != 0 || got.Kind != DeltaChain {
		t.Errorf("超长序列应放弃尾部匹配: %+v", got)
	}
}

// TestCompareLastLineRefreshIsAdded 覆盖「末行刷新 + 尾部追加」:
// 客户端每轮更新末尾注入内容(如剩余 token 计数),不应退化为分叉。
func TestCompareLastLineRefreshIsAdded(t *testing.T) {
	prev := linesOf(t, "a", "b", "<total_tokens>100</total_tokens>")
	cur := linesOf(t, "a", "b", "<total_tokens>99</total_tokens>", "c")
	got := Compare(prev, cur)
	if got.Kind != DeltaAdded {
		t.Fatalf("Kind = %q, want added", got.Kind)
	}
	if got.Kept != 2 || got.Added != 2 {
		t.Errorf("三段区间 = 保留 %d/新增 %d, want 2/2", got.Kept, got.Added)
	}

	// 行数未增加的末行刷新归分叉,并标记为末行更新(方向由字节数给出)。
	edited := linesOf(t, "a", "b", "X")
	if again := Compare(prev, edited); again.Kind != DeltaFork {
		t.Errorf("行数未增加 = %q, want fork", again.Kind)
	} else if !again.TailUpdate {
		t.Errorf("行数未增加的末行刷新未标记末行更新: %+v", again)
	}
}

// TestCompareTailUpdateWithoutSizes 覆盖行长度列缺失:
// 分类仍成立,但理由只陈述「末行被更新」,不编造方向、不印出哨兵值 -1。
func TestCompareTailUpdateWithoutSizes(t *testing.T) {
	prev := linesOf(t, "sys", "固定", "AAAA")
	cur := linesOf(t, "sys", "固定", "BBBB")
	// 抹掉长度列,模拟只落指纹的旧数据。
	prev.LineSizes, cur.LineSizes = nil, nil
	got := Compare(prev, cur)
	if !got.TailUpdate {
		t.Fatalf("未标记末行更新: %+v", got)
	}
	note := got.Note()
	if !strings.Contains(note, "行长度不可比") {
		t.Errorf("判定理由 = %q, want 含「行长度不可比」", note)
	}
	if strings.Contains(note, "-1") || strings.Contains(note, "字节数不变") {
		t.Errorf("判定理由编造了方向: %q", note)
	}
}
