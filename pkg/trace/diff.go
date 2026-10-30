package trace

import "bytes"

// DiffKind 是一处差异的类型。
type DiffKind string

const (
	DiffReplace DiffKind = "replace" // 两侧都有:该处的消息被改动
	DiffDelete  DiffKind = "delete"  // 只有旧侧:该处的消息被删除
	DiffInsert  DiffKind = "insert"  // 只有新侧:该处新增了消息
)

// DiffHunk 是一处差异:两侧各自的起始行与行数。
// 行号是**完整上下文中的位置**(0 基),与消息序号一致。
type DiffHunk struct {
	Kind DiffKind
	// PrevStart / PrevCount 是旧侧(上一步)的位置;插入时 Count 为 0。
	PrevStart int
	PrevCount int
	// CurStart / CurCount 是新侧(本步)的位置;删除时 Count 为 0。
	CurStart int
	CurCount int
}

// diffMaxCells 是 LCS 动态规划的规模上限(n×m)。超过时退回单块替换:
// 差异无法精确到行,但仍如实告知"这一段不同",不丢失可读性。
const diffMaxCells = 4 << 20 // 400 万格

// DiffHunks 计算两个快照之间的差异块(基于行指纹的 LCS)。
//
// 先裁掉公共前缀与公共后缀,只对中间部分做 LCS——差异通常集中在很小的一段,
// 裁剪后规模骤降。返回的 hunks 按行号升序,相邻同类的差异会合并。
// 两侧完全相同或纯追加时返回 nil(追加不算"分叉",由分类层表达)。
func DiffHunks(prev, cur SnapshotLines) []DiffHunk {
	prefix := CommonPrefixLen(prev.LineHashes, cur.LineHashes)
	suffix := commonSuffixLen(prev, cur, prefix)
	prevFrom, prevTo := prefix, prev.Lines-suffix
	curFrom, curTo := prefix, cur.Lines-suffix
	if prevFrom >= prevTo && curFrom >= curTo {
		return nil // 完全相同
	}
	if prevFrom >= prevTo {
		// 纯插入:整段都是新增。
		return []DiffHunk{{Kind: DiffInsert, PrevStart: prevFrom, CurStart: curFrom, CurCount: curTo - curFrom}}
	}
	if curFrom >= curTo {
		// 纯删除:整段都被移除。
		return []DiffHunk{{Kind: DiffDelete, PrevStart: prevFrom, PrevCount: prevTo - prevFrom, CurStart: curFrom}}
	}
	n, m := prevTo-prevFrom, curTo-curFrom
	if n*m > diffMaxCells {
		// 规模过大:退回单块替换,如实标注整段不同。
		return []DiffHunk{{
			Kind: DiffReplace, PrevStart: prevFrom, PrevCount: n, CurStart: curFrom, CurCount: m,
		}}
	}
	return lcsHunks(prev, cur, prevFrom, prevTo, curFrom, curTo)
}

// commonSuffixLen 返回两侧末尾逐行相同(指纹相等)的长度,不超过 prefix(避免重叠)。
func commonSuffixLen(prev, cur SnapshotLines, prefix int) int {
	maxN := min(prev.Lines, cur.Lines) - prefix
	n := 0
	for n < maxN {
		ph := LineHash(prev.LineHashes, prev.Lines-1-n)
		ch := LineHash(cur.LineHashes, cur.Lines-1-n)
		if ph == nil || ch == nil || !bytes.Equal(ph, ch) {
			break
		}
		n++
	}
	return n
}

// lcsHunks 对中间段做 LCS 并回溯出差异块。
func lcsHunks(prev, cur SnapshotLines, prevFrom, prevTo, curFrom, curTo int) []DiffHunk {
	n, m := prevTo-prevFrom, curTo-curFrom
	// dp[i][j] = prev[prevFrom+i:prevTo] 与 cur[curFrom+j:curTo] 的 LCS 长度。
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if bytes.Equal(LineHash(prev.LineHashes, prevFrom+i), LineHash(cur.LineHashes, curFrom+j)) {
				dp[i][j] = dp[i+1][j+1] + 1
				continue
			}
			if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	// 回溯:先收集逐步操作,再合并相邻同类。
	type step struct {
		kind    DiffKind
		prevIdx int
		curIdx  int
	}
	var steps []step
	for i, j := 0, 0; i < n || j < m; {
		switch {
		case i < n && j < m && bytes.Equal(LineHash(prev.LineHashes, prevFrom+i), LineHash(cur.LineHashes, curFrom+j)):
			i, j = i+1, j+1 // 相同行:跳过(不进 hunks)
		case j < m && (i == n || dp[i][j+1] > dp[i+1][j]):
			steps = append(steps, step{kind: DiffInsert, prevIdx: prevFrom + i, curIdx: curFrom + j})
			j++
		case i < n:
			steps = append(steps, step{kind: DiffDelete, prevIdx: prevFrom + i, curIdx: curFrom + j})
			i++
		default:
			j++
		}
	}

	hunks := make([]DiffHunk, 0, len(steps))
	for _, s := range steps {
		last := len(hunks) - 1
		if last >= 0 && mergeable(hunks[last], s.kind, s.prevIdx, s.curIdx) {
			hunks[last].PrevCount += prevCountOf(s.kind)
			hunks[last].CurCount += curCountOf(s.kind)
			continue
		}
		hunks = append(hunks, DiffHunk{
			Kind:      s.kind,
			PrevStart: s.prevIdx,
			PrevCount: prevCountOf(s.kind),
			CurStart:  s.curIdx,
			CurCount:  curCountOf(s.kind),
		})
	}
	// 相邻的删除与插入在同一个位置:合并为「替换」——它们表达的是"这几行被换成了那几行"。
	return coalesceReplace(hunks)
}

// mergeable 判断新的一步能否并入上一个 hunk:同类且位置连续。
func mergeable(h DiffHunk, kind DiffKind, prevIdx, curIdx int) bool {
	switch {
	case h.Kind == DiffDelete && kind == DiffDelete:
		return h.PrevStart+h.PrevCount == prevIdx
	case h.Kind == DiffInsert && kind == DiffInsert:
		return h.CurStart+h.CurCount == curIdx
	default:
		return false
	}
}

// prevCountOf / curCountOf 返回该操作在两侧各占几行。
func prevCountOf(kind DiffKind) int {
	if kind == DiffInsert {
		return 0
	}
	return 1
}

func curCountOf(kind DiffKind) int {
	if kind == DiffDelete {
		return 0
	}
	return 1
}

// coalesceReplace 把相邻的「删除 + 插入」合并为「替换」:
// 前者表达的"这里删了、那里加了"对查看者没有意义,合并后才是"这几行换成了那几行"。
func coalesceReplace(hunks []DiffHunk) []DiffHunk {
	out := make([]DiffHunk, 0, len(hunks))
	for _, h := range hunks {
		last := len(out) - 1
		if last >= 0 && out[last].Kind == DiffDelete && h.Kind == DiffInsert {
			out[last].Kind = DiffReplace
			out[last].CurStart = h.CurStart
			out[last].CurCount = h.CurCount
			continue
		}
		if last >= 0 && out[last].Kind == DiffInsert && h.Kind == DiffDelete {
			// 插入在前、删除在后:拆成替换时把两侧对齐。
			out[last].Kind = DiffReplace
			out[last].PrevStart = h.PrevStart
			out[last].PrevCount = h.PrevCount
			continue
		}
		out = append(out, h)
	}
	return out
}
