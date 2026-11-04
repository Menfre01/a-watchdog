package trace

import (
	"bytes"
	"fmt"
)

// DeltaKind 是相邻 step 的增量分类(design.md D5)。
type DeltaKind string

const (
	DeltaNew     DeltaKind = "new"     // 链首步:无前序
	DeltaAdded   DeltaKind = "added"   // 前缀延伸:只在尾部追加
	DeltaRetry   DeltaKind = "retry"   // 与上一步完全相同(客户端重发)
	DeltaFork    DeltaKind = "fork"    // 前缀相同,尾部被替换或删除
	DeltaRewrite DeltaKind = "rewrite" // 无共同前缀但有共享尾部(历史被替换)
	DeltaChain   DeltaKind = "chain"   // 无重合:与同链上一步既无共同前缀也无共享尾部
)

const (
	// summarizeRatio 是摘要压缩的判据:改写形态下新序列短于旧序列的这个比例。
	summarizeRatio = 0.6
	// clearMaxLines 是上下文清零的判据:无重合且行数不超过此值。
	clearMaxLines = 3
	// minSharedSuffix 是共享尾部的最小长度:单行相同不足以判定延续。
	minSharedSuffix = 2
	// maxSuffixScanLines 是共享尾部扫描的行数上限:超过时放弃尾部匹配。
	// 该判据只影响分类徽标,不值得为极端长序列付出 O(n²) 的比较代价。
	maxSuffixScanLines = 2000
)

// SnapshotLines 是一份快照的比对输入:行指纹、行长度与行数。
// 比对只读它,不触碰 ndjson(design.md D5)。
type SnapshotLines struct {
	LineHashes []byte
	LineSizes  []byte
	Lines      int
	// SystemFP 是首条 system 消息的稳定指纹,用作链归属的硬边界。
	// 实测:子 agent 从主对话分叉,消息前缀与主对话高度相似、上下文长度也相当,
	// 仅靠前缀/长度判据会把它与主对话混为一谈;各请求类型的 system 互不相同
	// (主对话 / 安全监控 / 生成标题 / 子 agent 各一种),是可靠的区分信号。
	SystemFP string
	// UserFP 是首条 user 消息文本前缀的指纹,用于在同一请求类型内区分实例。
	// 实测:3 个并行子 agent 共用同一套 system prompt(system 指纹相同),
	// 但各自的任务描述不同——按此前缀可精确分成 3 条链。
	UserFP string
}

// LinesInfo 返回快照的比对输入。
func (s Snapshot) LinesInfo() SnapshotLines {
	return SnapshotLines{LineHashes: s.LineHashes, LineSizes: s.LineSizes, Lines: s.Lines}
}

// SnapshotLinesOf 从落库的派生列还原比对输入:行数由指纹序列长度推得,
// 无需解压或解析 ndjson。视图层据此只读指纹即可完成分类与分组。
func SnapshotLinesOf(lineHashes, lineSizes []byte) SnapshotLines {
	return SnapshotLines{
		LineHashes: lineHashes,
		LineSizes:  lineSizes,
		Lines:      len(lineHashes) / lineHashBytes,
	}
}

// Delta 是一次相邻比对的结果:分类、三段区间与细分标记。
type Delta struct {
	Kind DeltaKind
	// PrevLines / CurLines 是前后快照的行数。
	PrevLines int
	CurLines  int
	// PrefixLen 是公共前缀行数;FirstDiff 是首个分歧位置(= PrefixLen)。
	PrefixLen int
	FirstDiff int
	// SharedSuffix 是共享尾部行数(仅改写形态使用)。
	SharedSuffix int
	// Kept / Removed / Added 是三段行数:保留(前缀 + 共享尾部)、移除、新增。
	Kept    int
	Removed int
	Added   int
	// Summarized 标记摘要压缩:改写形态且行数骤降。
	Summarized bool
	// PrevDiffLine / PrevDiffSize / CurDiffSize 描述首个分歧行的形态:
	// 该行在上一步与本步的字节数。指纹只能说明"这一行变了",长度能说明
	// "变长还是变短"——不从中推断压缩或改写等语义,只把事实交给界面展示。
	PrevDiffSize int
	CurDiffSize  int
	// Cleared 标记上下文清零:无重合且几乎清空。
	Cleared bool
	// TailUpdate 标记「只剩末行不同、行数不变」:末行内容被就地更新,方向不限。
	// 常见于客户端重发末尾注入内容(如剩余 token 计数)与旁路请求重写自己的
	// 转录行。旁路请求会重建转录,把先前的行写短,故变长与变短都要认。
	// 指纹只能说明「这行变了」,方向靠 TailBefore / TailAfter 给出。
	TailUpdate bool
	// TailBefore / TailAfter 是末行的字节数(仅 TailUpdate 时填充),用于给出方向。
	// 行长度列缺失时为 -1,此时不给方向。
	TailBefore int
	TailAfter  int
	// RetryN / RetryOf 是重试组内序号与组大小(由 Analyze 填充;非重试组为 1/1)。
	RetryN  int
	RetryOf int
}

// Note 给出该分类的判定理由:写明依据的特征;无法进一步区分时如实说明,
// 不给出过度确定的结论(对应 trajectory-analysis 规范)。
func (d Delta) Note() string {
	switch d.Kind {
	case DeltaNew:
		return "会话首条快照,无前序可比"
	case DeltaRetry:
		// 判据是内容完全相同:只陈述事实,不推断是重发还是并行的独立调用。
		if d.RetryOf > 1 {
			return fmt.Sprintf("与上一步内容完全相同(第 %d/%d 次相同请求)", d.RetryN, d.RetryOf)
		}
		return "与上一步内容完全相同"
	case DeltaAdded:
		return fmt.Sprintf("前缀延伸:继承 %d 行 + 新增 %d 行", d.Kept, d.Added)
	case DeltaFork:
		if d.TailUpdate {
			// 行长度列缺失时 LineSize 返回 -1:此时只能陈述「末行被更新」,
			// 不编造方向,也不印出 -1B。
			if d.TailBefore < 0 || d.TailAfter < 0 {
				return fmt.Sprintf("前缀 %d 行相同;末行内容被更新(行长度不可比)", d.PrefixLen)
			}
			switch {
			case d.TailAfter > d.TailBefore:
				return fmt.Sprintf("前缀 %d 行相同;末行内容从 %s 扩到 %s(常见于刷新末尾注入内容或转录增长)",
					d.PrefixLen, humanBytes(d.TailBefore), humanBytes(d.TailAfter))
			case d.TailAfter < d.TailBefore:
				return fmt.Sprintf("前缀 %d 行相同;末行内容从 %s 缩到 %s(常见于重写末尾注入内容或转录被重建)",
					d.PrefixLen, humanBytes(d.TailBefore), humanBytes(d.TailAfter))
			default:
				return fmt.Sprintf("前缀 %d 行相同;末行内容被改写(字节数不变,%s)",
					d.PrefixLen, humanBytes(d.TailAfter))
			}
		}
		// 多行变化:替换是「删 n 加 m」的成对操作,说「n 行被替换为 m 行」即可;
		// 说成「n 行被更新、m 行新增」会把同一处替换双计,读起来像涉及 n+m 行。
		if d.Removed > 0 && d.Added > 0 {
			return fmt.Sprintf("共同前缀 %d 行相同;%d 行被替换为 %d 行(常见于客户端刷新注入内容)",
				d.PrefixLen, d.Removed, d.Added)
		}
		return fmt.Sprintf("共同前缀 %d 行相同,尾部被替换或删除(新输入与改写无法进一步区分)", d.PrefixLen)
	case DeltaRewrite:
		if d.Summarized {
			return fmt.Sprintf("无共同前缀;旧序列末尾 %d 行原样保留,行数骤降 → 摘要压缩", d.SharedSuffix)
		}
		return fmt.Sprintf("无共同前缀;旧序列末尾 %d 行原样保留 → 历史被替换", d.SharedSuffix)
	case DeltaChain:
		if d.Cleared {
			return "无共同前缀也无共享尾部,且行数不超过 3 → 上下文清零"
		}
		// 只陈述事实:这一步与同链上一步零重合。是否属于同一条链由链归属判定,
		// 故不称「新链」——那个词会让读者把它读成链数(admin-console 规范)。
		return "与同链上一步无共同前缀也无共享尾部 → 无重合"
	}
	return ""
}

// Compare 比较相邻两份快照的派生列,给出增量分类与三段区间。不解析 JSON。
func Compare(prev, cur SnapshotLines) Delta {
	d := Delta{
		PrevLines: prev.Lines,
		CurLines:  cur.Lines,
		RetryN:    1,
		RetryOf:   1,
	}
	d.PrefixLen = CommonPrefixLen(prev.LineHashes, cur.LineHashes)
	d.FirstDiff = d.PrefixLen

	switch {
	case d.PrefixLen == prev.Lines && cur.Lines == prev.Lines:
		d.Kind = DeltaRetry
	case d.PrefixLen+1 >= prev.Lines && cur.Lines > prev.Lines:
		// 除末行外全部相同且总行数增加:视为追加。真实客户端每轮都会刷新末尾的注入
		// 内容(如剩余 token 计数),形式上表现为「末行被替换 + 尾部追加」。
		d.Kind = DeltaAdded
	case d.PrefixLen > 0:
		// 前缀相同但更短(尾部删除),或前缀仅部分相同(尾部替换):都归为分叉,
		// 首个分歧位置即 PrefixLen。
		d.Kind = DeltaFork
	default:
		// 无共同前缀:再看旧序列末尾是否被保留。
		if suffix := sharedSuffix(prev, cur); suffix >= minSharedSuffix {
			d.Kind = DeltaRewrite
			d.SharedSuffix = suffix
		} else {
			d.Kind = DeltaChain
		}
	}

	switch d.Kind {
	case DeltaRetry:
		d.Kept = prev.Lines
	case DeltaAdded:
		d.Kept = d.PrefixLen
		d.Added = cur.Lines - d.PrefixLen
	case DeltaChain:
		d.Removed = prev.Lines
		d.Added = cur.Lines
	case DeltaRewrite:
		d.Kept = d.SharedSuffix
		d.Removed = prev.Lines - d.SharedSuffix
		d.Added = cur.Lines - d.SharedSuffix
	case DeltaFork:
		d.Kept = d.PrefixLen
		d.Removed = prev.Lines - d.PrefixLen
		d.Added = cur.Lines - d.PrefixLen
	}

	// 细分标记:摘要压缩与上下文清零。
	if d.Kind == DeltaRewrite && float64(cur.Lines) < float64(prev.Lines)*summarizeRatio {
		d.Summarized = true
	}
	// 记录首个分歧行在两边的字节数:只陈述事实,不据此推断压缩或改写。
	if d.FirstDiff < prev.Lines && d.FirstDiff < cur.Lines {
		d.PrevDiffSize = LineSize(prev.LineSizes, d.FirstDiff)
		d.CurDiffSize = LineSize(cur.LineSizes, d.FirstDiff)
	}
	if d.Kind == DeltaChain && cur.Lines <= clearMaxLines {
		d.Cleared = true
	}
	// 末行更新:只剩末行不同、行数不变。客户端重发末尾注入内容(token 计数)、
	// 旁路请求重写自己的转录行都落在这个形态上。
	// 不限定方向——变长与变短都是"这一行被就地更新",方向由字节数给出:
	// 只认变长会把「转录被重建」这类变短的变化误标成笼统的「分叉」。
	if d.Kind == DeltaFork && d.PrefixLen+1 >= prev.Lines && cur.Lines == prev.Lines {
		last := cur.Lines - 1
		if last >= 0 {
			d.TailUpdate = true
			d.TailBefore, d.TailAfter = LineSize(prev.LineSizes, last), LineSize(cur.LineSizes, last)
		}
	}
	return d
}

// Analyze 对一串快照(按会话序号)做相邻比对:首步为 DeltaNew,其余逐步比较,
// 并标注连续重复组成的重试组。
func Analyze(snaps []SnapshotLines) []Delta {
	if len(snaps) == 0 {
		return nil
	}
	out := make([]Delta, len(snaps))
	out[0] = Delta{
		Kind:      DeltaNew,
		CurLines:  snaps[0].Lines,
		FirstDiff: 0,
		Added:     snaps[0].Lines,
		RetryN:    1,
		RetryOf:   1,
	}
	for i := 1; i < len(snaps); i++ {
		out[i] = Compare(snaps[i-1], snaps[i])
	}
	markRetryGroups(out)
	return out
}

// markRetryGroups 标注重试组:一个非重复的起始步加上紧随其后的连续重复步构成一组,
// 组内序号即尝试次序(重试 n/N,design.md D5)。
func markRetryGroups(deltas []Delta) {
	for i := 1; i < len(deltas); i++ {
		if deltas[i].Kind != DeltaRetry {
			continue
		}
		// 组的起点是紧邻的前一步:它是该组的首次尝试(外层循环按组推进,
		// 上一步必然不是重复)。
		start := i - 1
		end := i
		for end < len(deltas) && deltas[end].Kind == DeltaRetry {
			end++
		}
		total := end - start
		for k := start; k < end; k++ {
			deltas[k].RetryN = k - start + 1
			deltas[k].RetryOf = total
		}
		i = end - 1
	}
}

// sharedSuffix 返回旧序列末尾能在新序列中连续找到的最大行数(k ≥ minSharedSuffix 才算命中)。
// 判据只约束旧序列的末尾:头部与中段被整体替换不影响判定(design.md D4/D5)。
func sharedSuffix(prev, cur SnapshotLines) int {
	maxK := min(prev.Lines, cur.Lines)
	if maxK > maxSuffixScanLines {
		return 0
	}
	for k := maxK; k >= minSharedSuffix; k-- {
		if containsRun(cur, prev, prev.Lines-k, k) {
			return k
		}
	}
	return 0
}

// containsRun 判断 needle 中从 start 起的 k 行是否在新序列中连续出现。
func containsRun(cur, needle SnapshotLines, start, k int) bool {
	for offset := range cur.Lines - k + 1 {
		matched := true
		for i := 0; i < k; i++ {
			if !bytes.Equal(LineHash(cur.LineHashes, offset+i), LineHash(needle.LineHashes, start+i)) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// humanBytes 把字节数写成可读形式(用于判定理由里的长度对比)。
func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
