package trace

import (
	"strconv"
	"strings"
	"testing"
)

// hunkShape 把 hunks 压成可比较的形状:`replace@2:1→1 delete@5:2→0` 之类。
func hunkShape(hunks []DiffHunk) string {
	parts := make([]string, 0, len(hunks))
	for _, h := range hunks {
		parts = append(parts, string(h.Kind)+"@"+
			strconv.Itoa(h.PrevStart)+":"+strconv.Itoa(h.PrevCount)+"→"+
			strconv.Itoa(h.CurStart)+":"+strconv.Itoa(h.CurCount))
	}
	return strings.Join(parts, " ")
}

// diffOf 构造两侧快照并求差异块(测试夹具)。
func diffOf(t *testing.T, prev, cur []string) []DiffHunk {
	t.Helper()
	return DiffHunks(linesOf(t, prev...), linesOf(t, cur...))
}

// TestDiffHunksSame 覆盖完全相同时无差异块。
func TestDiffHunksSame(t *testing.T) {
	if got := diffOf(t, []string{"a", "b", "c"}, []string{"a", "b", "c"}); got != nil {
		t.Errorf("相同序列 = %+v, want nil", got)
	}
}

// TestDiffHunksPureInsert 覆盖纯插入:整段都是新增。
func TestDiffHunksPureInsert(t *testing.T) {
	got := diffOf(t, []string{"a"}, []string{"a", "b", "c"})
	if want := "insert@1:0→1:2"; hunkShape(got) != want {
		t.Errorf("hunks = %s, want %s", hunkShape(got), want)
	}
}

// TestDiffHunksPureDelete 覆盖纯删除:整段都被移除(旧消息被删)。
func TestDiffHunksPureDelete(t *testing.T) {
	got := diffOf(t, []string{"a", "b", "c"}, []string{"a"})
	if want := "delete@1:2→1:0"; hunkShape(got) != want {
		t.Errorf("hunks = %s, want %s", hunkShape(got), want)
	}
}

// TestDiffHunksSingleReplace 覆盖单行替换。
func TestDiffHunksSingleReplace(t *testing.T) {
	got := diffOf(t, []string{"a", "b", "c"}, []string{"a", "X", "c"})
	if want := "replace@1:1→1:1"; hunkShape(got) != want {
		t.Errorf("hunks = %s, want %s", hunkShape(got), want)
	}
}

// TestDiffHunksMultiLineReplace 覆盖多行替换:连续改动应合并为一块。
func TestDiffHunksMultiLineReplace(t *testing.T) {
	got := diffOf(t, []string{"a", "b", "c", "d", "e"}, []string{"a", "X", "Y", "e"})
	// 中间删 b,c,d(3 行)换成 X,Y(2 行) → 一块替换。
	if want := "replace@1:3→1:2"; hunkShape(got) != want {
		t.Errorf("hunks = %s, want %s", hunkShape(got), want)
	}
}

// TestDiffHunksInsertInMiddle 覆盖中间插入:前后相同行应被跳过,只报插入块。
func TestDiffHunksInsertInMiddle(t *testing.T) {
	got := diffOf(t, []string{"a", "b", "c"}, []string{"a", "NEW", "b", "c"})
	if want := "insert@1:0→1:1"; hunkShape(got) != want {
		t.Errorf("hunks = %s, want %s", hunkShape(got), want)
	}
}

// TestDiffHunksDeleteInMiddle 覆盖中间删除:被删的消息必须出现在 hunks 里。
func TestDiffHunksDeleteInMiddle(t *testing.T) {
	got := diffOf(t, []string{"a", "GONE", "b", "c"}, []string{"a", "b", "c"})
	if want := "delete@1:1→1:0"; hunkShape(got) != want {
		t.Errorf("hunks = %s, want %s", hunkShape(got), want)
	}
}

// TestDiffHunksMultipleBlocks 覆盖多处独立差异:应分成多个块。
func TestDiffHunksMultipleBlocks(t *testing.T) {
	prev := []string{"a", "old1", "b", "c", "d", "e", "old2", "f"}
	cur := []string{"a", "new1", "b", "c", "e", "f"}
	got := diffOf(t, prev, cur)
	// 第一处:old1→new1 替换;第二处:d 被删除、old2 被删除(与 d 合并)。
	shape := hunkShape(got)
	if !strings.HasPrefix(shape, "replace@1:1→1:1") {
		t.Errorf("首块 = %s, want 以 replace@1:1→1:1 开头", shape)
	}
	if len(got) < 2 {
		t.Fatalf("应分出多块,实际 %d 块: %s", len(got), shape)
	}
}

// TestDiffHunksSymmetricDeleteInsert 覆盖"删除若干行 + 新增若干行"的相邻形态:
// 应合并为替换块(表达"这几行换成了那几行"),而不是两个独立块。
func TestDiffHunksSymmetricDeleteInsert(t *testing.T) {
	got := diffOf(t, []string{"a", "b", "c"}, []string{"a", "X", "Y", "Z"})
	shape := hunkShape(got)
	if !strings.HasPrefix(shape, "replace@1:") {
		t.Errorf("hunks = %s, want 合并为 replace 起始", shape)
	}
	if len(got) != 1 {
		t.Errorf("两块相邻应合并,实际 %d 块: %s", len(got), shape)
	}
}

// TestDiffHunksNoOverlapWithCommonEdges 覆盖公共前后缀被裁掉:
// 只有中间不同的部分进入 hunks,行号仍是完整上下文中的位置。
func TestDiffHunksNoOverlapWithCommonEdges(t *testing.T) {
	prev := []string{"p1", "p2", "p3", "diff", "s1", "s2", "s3"}
	cur := []string{"p1", "p2", "p3", "DIFF", "s1", "s2", "s3"}
	got := diffOf(t, prev, cur)
	if want := "replace@3:1→3:1"; hunkShape(got) != want {
		t.Errorf("hunks = %s, want %s", hunkShape(got), want)
	}
}

// TestDiffHunksEmptySides 覆盖空序列。
func TestDiffHunksEmptySides(t *testing.T) {
	if got := DiffHunks(SnapshotLines{}, SnapshotLines{}); got != nil {
		t.Errorf("空→空 = %+v, want nil", got)
	}
	// 空 → 有内容:整段新增。
	got := DiffHunks(SnapshotLines{}, linesOf(t, "a", "b"))
	if want := "insert@0:0→0:2"; hunkShape(got) != want {
		t.Errorf("空→有内容 = %s, want %s", hunkShape(got), want)
	}
}

// TestDiffHunksLargeFallback 覆盖规模超限:退回单块替换,仍如实标注整段不同。
func TestDiffHunksLargeFallback(t *testing.T) {
	n := 2200 // n×n > 400 万格,触发上限
	prev := make([]string, n)
	cur := make([]string, n)
	for i := range prev {
		prev[i] = "p" + strconv.Itoa(i)
		cur[i] = "c" + strconv.Itoa(i)
	}
	got := DiffHunks(linesOf(t, prev...), linesOf(t, cur...))
	if len(got) != 1 || got[0].Kind != DiffReplace {
		t.Fatalf("超限应退回单块替换: %d 块", len(got))
	}
	if got[0].PrevCount != n || got[0].CurCount != n {
		t.Errorf("回退块的覆盖范围 = %d→%d, want %d→%d",
			got[0].PrevCount, got[0].CurCount, n, n)
	}
}
