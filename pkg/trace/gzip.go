package trace

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

// maxDecompressedBytes 是单份快照解压后的上限:原始层由网关注入,但仍以固定上限兜底,
// 避免损坏或被篡改的数据在解压时撑爆内存。
const maxDecompressedBytes = 256 << 20 // 256 MiB

// Compress 以 gzip 压缩字节串:原始层与快照都以 gzip 存储(见 design.md D1)。
// 请求体多为 JSON 文本,BestSpeed 已能拿到大部分压缩收益且省 CPU。
func Compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, fmt.Errorf("trace: new gzip writer: %w", err)
	}
	if _, err := zw.Write(data); err != nil {
		return nil, fmt.Errorf("trace: gzip write: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("trace: gzip close: %w", err)
	}
	return buf.Bytes(), nil
}

// Decompress 解开 gzip 字节串。
func Decompress(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("trace: new gzip reader: %w", err)
	}
	// gzip.Reader 的 Close 不校验校验和、无实际错误路径,忽略其返回值。
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(io.LimitReader(zr, maxDecompressedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("trace: gzip read: %w", err)
	}
	if len(out) > maxDecompressedBytes {
		return nil, fmt.Errorf("trace: 解压后超过上限 %d 字节", maxDecompressedBytes)
	}
	return out, nil
}
