package trace

import (
	"regexp"
	"slices"
	"sort"
	"strings"
)

// injectionPrefixes 是客户端注入内容的文本前缀:命中者不开启新轮次。
// 清单经真实流量采样校准(tasks 5.1),不靠推测。
var injectionPrefixes = []string{
	"<system-reminder>",
	"<command-name>",
	"<command-message>",
	"<command-args>",
	"<local-command-stdout>",
	"<local-command-stderr>",
	"Caveat:",
	// Claude Code 在用户离开后自动请求的复述提示,不是人的输入(真实流量采样所得)。
	"The user stepped away and is coming back.",
	// 子 agent 完成时由 harness 注入主对话的结果,不是人的输入:
	// 措辞里的「Another Claude session」正说明它来自另一个会话。
	// 它是发起该子 agent 那一轮的延续,不得开启新轮次(真实流量采样所得)。
	"Another Claude session sent a message:",
}

// injectedTagPattern 匹配形如 `<tag>` / `</tag>` 的开头。客户端注入内容普遍以
// 自定义标签包裹,硬编码列表会随客户端版本腐化(实测 `<command-message>` 漏网
// 会开出假轮次),故在列表之外补一条通用判据。
var injectedTagPattern = regexp.MustCompile(`^</?[a-z][a-z0-9-]*>`)

// StepForTurn 是轮次分组所需的单步输入。
type StepForTurn struct {
	Seq   int64
	Delta Delta
	Added []MainlineMessage // 该步相对上一步新增的消息
	// Side 标记旁路步(客户端的监控 / 子 agent 请求):内联在轮次里以保留时间顺序,
	// 但不参与轮次边界判定——旁路请求不是用户的对话轮次。
	Side bool
}

// Turn 是一个轮次:由一个或多个相邻 step 组成。
type Turn struct {
	Index    int     // 轮次序号(自 1 起)
	StepSeqs []int64 // 该轮的 step 序号(升序)
	Parallel bool    // 平行链(无重合形态):不与主链轮次混合
}

// GroupTurns 依据增量分类与新增消息推断轮次分组(design.md D5):
// 主路步决定轮次边界——追加或分叉且新增尾部含「人的新输入」时开新轮,
// 重试与压缩留在原轮,无法确定时不臆断;无重合的步单独成组并标记为平行链。
//
// 旁路步不参与边界判定,归入其**之后**最近的主路步所在轮:真实流量里监控请求
// 与被检查的操作常在同一秒发出(实测:用户第二问与一次监控同为 09:05:50),
// 按「前一个主路步」归属会把它们错放到上一轮。其后没有主路步时并入最后一轮。
func GroupTurns(steps []StepForTurn) []Turn {
	if len(steps) == 0 {
		return nil
	}
	turns := make([]Turn, 0, len(steps))
	turnOfSeq := make(map[int64]int, len(steps))

	// 第一遍:只在主路步上划分轮次。
	for _, step := range steps {
		if step.Side {
			continue
		}
		if len(turns) == 0 || startsNewTurn(step) {
			turns = append(turns, Turn{
				Index:    len(turns) + 1,
				Parallel: step.Delta.Kind == DeltaChain,
			})
		}
		turns[len(turns)-1].StepSeqs = append(turns[len(turns)-1].StepSeqs, step.Seq)
		turnOfSeq[step.Seq] = len(turns) - 1
	}

	if len(turns) == 0 {
		// 全是旁路步:并为一轮,避免生成空结果。
		all := make([]int64, 0, len(steps))
		for _, step := range steps {
			all = append(all, step.Seq)
		}
		return []Turn{{Index: 1, StepSeqs: all}}
	}

	// 第二遍:从后往前确定每个旁路步的目标轮——其后最近的主路步所在轮。
	target := len(turns) - 1
	sideSeq := make(map[int64]int, len(steps))
	for i := len(steps) - 1; i >= 0; i-- {
		if !steps[i].Side {
			target = turnOfSeq[steps[i].Seq]
			continue
		}
		sideSeq[steps[i].Seq] = target
	}
	for _, step := range steps {
		if !step.Side {
			continue
		}
		idx := sideSeq[step.Seq]
		turns[idx].StepSeqs = append(turns[idx].StepSeqs, step.Seq)
	}
	// 插入后按序号排序,保证轮内仍按时间序呈现。
	for i := range turns {
		seqs := turns[i].StepSeqs
		sort.Slice(seqs, func(a, b int) bool { return seqs[a] < seqs[b] })
	}
	return turns
}

// startsNewTurn 判断该 step 是否开启新轮次。
func startsNewTurn(step StepForTurn) bool {
	if step.Side {
		// 旁路步内联在所属轮次里,不开新轮。
		return false
	}
	switch step.Delta.Kind {
	case DeltaAdded, DeltaFork:
		// 追加与分叉都要看尾部:真实客户端每轮都会更新注入内容(如剩余 token 计数),
		// 使纯追加退化为分叉,但尾部仍可能是人的新输入(真实流量采样所得)。
		return containsHumanInput(step.Added)
	case DeltaChain:
		// 无重合的步与主链不相交:单独成组,不并入主链轮次。
		return true
	default:
		// 重试、压缩改写、分叉:留在原轮(无法确定是否为新诉求时不臆断)。
		return false
	}
}

// containsHumanInput 判断新增消息中是否有代表「人的新输入」的条目。
func containsHumanInput(messages []MainlineMessage) bool {
	return slices.ContainsFunc(messages, isHumanInput)
}

// isHumanInput 判断一条消息是否代表人的新输入:user 角色、含非空文本、
// 不含工具结果、且首段文本不命中注入判据。
func isHumanInput(msg MainlineMessage) bool {
	if msg.Role != "user" {
		return false
	}
	firstText := ""
	for _, block := range msg.Blocks {
		switch typeOf(map[string]any(block)) {
		case "tool_result":
			// 含工具结果的消息是迭代产物,不是新输入。
			return false
		case "text":
			text := stringField(block, "text")
			if strings.TrimSpace(text) == "" {
				continue
			}
			if firstText == "" {
				firstText = text
			}
		}
	}
	// 没有任何非空文本的 user 消息(如纯图片、空块)不构成可读的新输入。
	if firstText == "" {
		return false
	}
	return !isInjectedText(firstText)
}

// isInjectedText 判断文本是否来自客户端注入:命中前缀列表,或以自定义标签开头。
func isInjectedText(text string) bool {
	trimmed := strings.TrimSpace(text)
	for _, prefix := range injectionPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return injectedTagPattern.MatchString(trimmed)
}
