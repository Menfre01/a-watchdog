package trace

// ChainKind 标识会话内的消息链类型。
type ChainKind string

const (
	// ChainMain 是主链:至少有两个非重复步的链中最早的一条。
	// 没有这样的链时,主链是含会话首步的链。
	ChainMain ChainKind = "main"
	// ChainSide 是旁路链:首条消息指纹与主链不同的请求。
	ChainSide ChainKind = "side"
)

// SessionChain 是会话内的一条消息链。
// 同类请求会反复出现,所以链的成员不必在时间上连续——中间可能插着别的链的步。
type SessionChain struct {
	Kind   ChainKind
	Steps  []int   // 链内各步在会话中的下标(升序)
	Deltas []Delta // 与 Steps 等长:各步相对「本链上一步」的比较结果
}

// AnalyzeSession 做链归属,判据是两级的**首条消息指纹**:
//
//  1. system 指纹 = 请求类型(主对话 / 安全监控 / 子 agent / 生成标题),
//     剔除易变注解后同类型稳定、跨类型互不相同;
//  2. user 前缀指纹 = 同类型内的实例(3 个并行子 agent 共用 system prompt,
//     靠各自的任务描述区分)。
//
// 两侧都相同才归入同一条链。任一侧缺指纹(无 system 消息、首条 user 无文本)
// 时退回上一级判据。判据只用消息本身,不解析客户端特有的标记、不跟踪工具调用
// 关系,任何协议都适用。
//
// 主链是至少有两个非重复步的链中时间最早的一条。重复步(客户端重发)
// 不构成持续性,不计入门槛。一次性链不参与主链竞争:客户端会在会话开头
// 发出「生成标题」等一次性请求,它可能成为时间上的首步,但主对话必然在其后
// 反复出现。全部链都不足两个非重复步时退回含会话首步者。
//
// 「至少两个非重复步」只作门槛,不比较步数大小:步数与上下文长度都不是主链
// 判据(实测:27 步的监控链超过 15 步的对话链;45 行的子 agent 链超过 42 行
// 的主对话链)。
func AnalyzeSession(lines []SnapshotLines) []SessionChain {
	if len(lines) == 0 {
		return nil
	}
	var chains []SessionChain
	for i := range lines {
		matched := -1
		for j := range chains {
			last := lines[chains[j].Steps[len(chains[j].Steps)-1]]
			if sameChain(last, lines[i]) {
				matched = j
				break
			}
		}
		if matched < 0 {
			chains = append(chains, SessionChain{
				Steps:  []int{i},
				Deltas: []Delta{firstDelta(lines[i])},
			})
			continue
		}
		last := lines[chains[matched].Steps[len(chains[matched].Steps)-1]]
		chains[matched].Steps = append(chains[matched].Steps, i)
		chains[matched].Deltas = append(chains[matched].Deltas, Compare(last, lines[i]))
	}
	// 主链:至少两个非重复步的链中最早的一条。链的切片顺序即各链首次出现顺序,
	// 故第一个满足门槛的链就是主链。没有这样的链时退回链 0(含会话首步者)。
	mainIdx := 0
	for j := range chains {
		if persistentSteps(chains[j]) >= 2 {
			mainIdx = j
			break
		}
	}
	chains[mainIdx].Kind = ChainMain
	for j := range chains {
		if j != mainIdx {
			chains[j].Kind = ChainSide
		}
	}
	// 重复组按链统计:跨链的相同内容不算重复。
	for j := range chains {
		markRetryGroups(chains[j].Deltas)
		// 旁路链不标「上下文清零」:该语义只对主对话成立(/clear 是用户对主对话
		// 的操作)。旁路请求每次都是独立的 system + 输入,没有"上下文"可言;
		// 而它们本身往往就是 3 行左右,恰好命中该判据(实测:监控链跨 turn 时
		// 前缀不共享,被误标为上下文清零)。判据用 Kind,不假设主链位于下标 0。
		if chains[j].Kind == ChainSide {
			for k := range chains[j].Deltas {
				chains[j].Deltas[k].Cleared = false
			}
		}
	}
	return chains
}

// persistentSteps 返回链中非重复步的数量。重试只重复已有内容,不构成持续性证据。
func persistentSteps(chain SessionChain) int {
	n := 0
	for _, delta := range chain.Deltas {
		if delta.Kind != DeltaRetry {
			n++
		}
	}
	return n
}

// sameChain 判断两份快照是否属于同一条链:两级指纹都相同。
//
// 一级指纹(system)缺失时不拆分:它在判「请求类型」,缺了就无从判断,而二级
// 指纹只用于「同类型内的实例」。缺类型而用实例判据会误分——真实反例:不发送
// system 消息的客户端,其首条 user 消息每轮都变,若据此拆分则每一步都自成一条链。
// 宁可不拆(退化为单链),也不把一个会话切成碎片。
//
// 二级指纹缺失(首条 user 无文本)时同样跳过该级,只用 system 判定。
func sameChain(a, b SnapshotLines) bool {
	if a.SystemFP == "" || b.SystemFP == "" {
		return true
	}
	if a.SystemFP != b.SystemFP {
		return false
	}
	if a.UserFP != "" && b.UserFP != "" && a.UserFP != b.UserFP {
		return false
	}
	return true
}

// firstDelta 返回一条链首步的增量:整体视为新增,分类为该链的起点。
func firstDelta(lines SnapshotLines) Delta {
	kind := DeltaNew
	if lines.Lines == 0 {
		kind = DeltaChain
	}
	return Delta{
		Kind:     kind,
		CurLines: lines.Lines,
		Added:    lines.Lines,
		RetryN:   1,
		RetryOf:  1,
	}
}
