package trace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

const (
	// systemPrefixRunes 是参与指纹的 system 文本长度上限。
	// 客户端会把实时配置(权限拒绝规则等)嵌在 system 深处,实测同一类监控
	// 因配置变化在第 9 万字符处分叉、被拆成三条链;取前缀即可忽略这类
	// 与"请求类型"无关的漂移。
	systemPrefixRunes = 2000
	// volatileVersionPattern 匹配计费头里的版本号:同一客户端的各进程可能不同
	// (实测 ca9 / d5a / d7e / 874),不剔除会让重启后的主链断裂。
	volatileVersionPattern = `cc_version=[^;\s]*`
)

var volatileVersionRE = regexp.MustCompile(volatileVersionPattern)

// SystemFingerprint 计算首条 system 消息的稳定指纹,用作链归属的硬边界。
//
// 为什么需要它:子 agent 从主对话分叉,消息前缀与主对话高度相似、上下文长度
// 也相当,仅靠前缀/长度判据会把它与主对话混为一谈(实测:45 行的子 agent 链
// 击败 42 行的主对话链,导致界面把子 agent 当主路)。各请求类型的 system 互不
// 相同(主对话 / 安全监控 / 子 agent / 生成标题各一种),是可靠的区分信号。
//
// 剔除两类易变内容,使同一类型的各次调用得到同一指纹:
//   - 块级注解(cache_control 随客户端滚动位置)
//   - 配额计数行(`<total_tokens>…tokens left` 每轮都变)
//
// 无 system 消息时返回空串,调用方据此退回前缀判据。
func SystemFingerprint(messages []MainlineMessage) string {
	for _, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		blocks := make([]MainlineBlock, 0, len(msg.Blocks))
		for _, block := range msg.Blocks {
			stable := make(MainlineBlock, len(block))
			for key, value := range block {
				if volatileBlockKeys[key] {
					continue
				}
				stable[key] = value
			}
			if text, ok := stable["text"].(string); ok {
				stable["text"] = stripVolatileLines(text)
			}
			blocks = append(blocks, stable)
		}
		canon, err := json.Marshal(MainlineMessage{Role: msg.Role, Blocks: blocks})
		if err != nil {
			return ""
		}
		// 剔除每次进程可能不同的版本号,再取前若干字符:两者都与「请求类型」无关。
		stable := volatileVersionRE.ReplaceAllString(string(canon), "cc_version=?")
		runes := []rune(stable)
		if len(runes) > systemPrefixRunes {
			runes = runes[:systemPrefixRunes]
		}
		sum := sha256.Sum256([]byte(string(runes)))
		return hex.EncodeToString(sum[:lineHashBytes])
	}
	return ""
}

// userPrefixRunes 是实例指纹取用的首条 user 文本长度。
// 取前缀而非全文:监控请求的首条 user 里含随对话增长的转录,前缀稳定而后文变动。
const userPrefixRunes = 120

// UserFingerprint 计算首条 user 消息文本前缀的指纹,用于区分同一请求类型的
// 多个实例。实测:3 个并行子 agent 共用同一 system prompt,仅凭 system 指纹
// 会把它们合成一条 49 步的链;而它们的任务描述不同,按此前缀可精确分成 3 条。
//
// 返回空串表示首条 user 消息没有文本块(如纯工具结果),调用方据此只用
// system 指纹判定。
func UserFingerprint(messages []MainlineMessage) string {
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		for _, block := range msg.Blocks {
			text, ok := block["text"].(string)
			if !ok || strings.TrimSpace(text) == "" {
				continue
			}
			runes := []rune(text)
			if len(runes) > userPrefixRunes {
				runes = runes[:userPrefixRunes]
			}
			sum := sha256.Sum256([]byte(string(runes)))
			return hex.EncodeToString(sum[:lineHashBytes])
		}
		// 首条 user 消息没有可用文本:不向后找,交由 system 指纹单独判定。
		return ""
	}
	return ""
}

// stripVolatileLines 去掉每轮都会变化的配额计数行(`<total_tokens>…tokens left`)。
func stripVolatileLines(text string) string {
	if !strings.Contains(text, "<total_tokens>") {
		return text
	}
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "<total_tokens>") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
