package trace

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

const (
	// lineHashBytes 是行指纹的长度(sha256 前 8 字节):单会话碰撞概率可忽略,
	// 且定长切片便于比对(design.md D1)。
	lineHashBytes = 8
	// lineSizeBytes 是行长度记录的长度(uint32 大端)。
	lineSizeBytes = 4
	// maxLineBytes 是单行长度上限:超过即视为异常输入,避免长度字段溢出。
	maxLineBytes = 1 << 26 // 64 MiB
)

// volatileBlockKeys 是每轮都可能变化的注解字段:客户端用 cache_control 标记缓存断点,
// 断点位置随会话滚动。它们必须原样留在快照里(保真),但不得参与指纹与行长度,
// 否则每一步的前缀都会断在标记那一行,分类全部退化为「分叉」(真实流量采样所得)。
var volatileBlockKeys = map[string]bool{"cache_control": true}

// Snapshot 是一份可落库的快照:ndjson 明文与两列派生序列。
type Snapshot struct {
	NDJSON     []byte // 一行一条 canonical JSON,每行以 \n 结尾(含最后一行)
	LineHashes []byte // 每行 sha256 前 8 字节,按行序拼接
	LineSizes  []byte // 每行字节数(uint32 大端),按行序拼接
	Lines      int    // 行数
}

// Serialize 把主干消息序列化成快照形态:一行一条 canonical JSON,
// 并派生行指纹与行长度。两者都是与分类规则无关的确定性事实,
// 比对与增量分类只读它们,不触发 JSON 解析(design.md D1/D5)。
func Serialize(messages []MainlineMessage) (Snapshot, error) {
	var buf bytes.Buffer
	hashes := make([]byte, 0, len(messages)*lineHashBytes)
	sizes := make([]byte, 0, len(messages)*lineSizeBytes)
	for _, msg := range messages {
		// encoding/json 对结构体按字段序、对 map 按键序输出,天然 canonical。
		line, err := json.Marshal(msg)
		if err != nil {
			return Snapshot{}, fmt.Errorf("trace: 序列化消息: %w", err)
		}
		if len(line) > maxLineBytes {
			return Snapshot{}, fmt.Errorf("trace: 单行 %d 字节超出上限 %d", len(line), maxLineBytes)
		}
		// 比对输入剔除易变注解:内容与角色不变即视为同一行。
		stable, err := stableLineBytes(msg)
		if err != nil {
			return Snapshot{}, fmt.Errorf("trace: 序列化比对行: %w", err)
		}
		sum := sha256.Sum256(stable)
		hashes = append(hashes, sum[:lineHashBytes]...)
		var size [lineSizeBytes]byte
		// #nosec G115 -- 剔除字段只会让长度更小,已由上方校验兜底。
		binary.BigEndian.PutUint32(size[:], uint32(len(stable)))
		sizes = append(sizes, size[:]...)
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return Snapshot{
		NDJSON:     buf.Bytes(),
		LineHashes: hashes,
		LineSizes:  sizes,
		Lines:      len(messages),
	}, nil
}

// stableLineBytes 返回仅用于比对的行字节:剔除每轮都会滚动的注解字段。
func stableLineBytes(msg MainlineMessage) ([]byte, error) {
	blocks := make([]MainlineBlock, 0, len(msg.Blocks))
	for _, block := range msg.Blocks {
		stable := make(MainlineBlock, len(block))
		for key, value := range block {
			if volatileBlockKeys[key] {
				continue
			}
			stable[key] = value
		}
		blocks = append(blocks, stable)
	}
	return json.Marshal(MainlineMessage{Role: msg.Role, Blocks: blocks})
}

// ParseLines 把快照 ndjson 拆成行:忽略尾随空行(切分规则与 Serialize 共用,
// 两侧不一致会让指纹序列错位,见 design.md D1)。
func ParseLines(ndjson []byte) [][]byte {
	raw := bytes.Split(ndjson, []byte("\n"))
	// 末行后必有换行,切分结果末尾会多一个空元素;连续空行一并忽略。
	for len(raw) > 0 && len(raw[len(raw)-1]) == 0 {
		raw = raw[:len(raw)-1]
	}
	return raw
}

// LineHash 返回指纹序列中第 i 行的 8 字节指纹;越界返回 nil。
func LineHash(hashes []byte, i int) []byte {
	start := i * lineHashBytes
	if i < 0 || start+lineHashBytes > len(hashes) {
		return nil
	}
	return hashes[start : start+lineHashBytes]
}

// LineSize 返回长度序列中第 i 行的字节数;越界返回 -1。
func LineSize(sizes []byte, i int) int {
	start := i * lineSizeBytes
	if i < 0 || start+lineSizeBytes > len(sizes) {
		return -1
	}
	return int(binary.BigEndian.Uint32(sizes[start : start+lineSizeBytes]))
}

// CommonPrefixLen 返回两串指纹序列的公共前缀长度(按行比较,不解析 JSON)。
func CommonPrefixLen(a, b []byte) int {
	n := 0
	for {
		ha, hb := LineHash(a, n), LineHash(b, n)
		if ha == nil || hb == nil || !bytes.Equal(ha, hb) {
			return n
		}
		n++
	}
}
