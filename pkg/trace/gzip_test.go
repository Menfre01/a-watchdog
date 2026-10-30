package trace

import (
	"bytes"
	"testing"
)

// TestCompressRoundTrip 覆盖压缩往返:内容与长度都还原。
func TestCompressRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"空输入", nil},
		{"短文本", []byte(`{"a":1}`)},
		{"可压缩文本", bytes.Repeat([]byte(`{"role":"user","content":"hi"}`), 100)},
		{"二进制", []byte{0x00, 0xff, 0x10, 0x80}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compressed, err := Compress(tc.in)
			if err != nil {
				t.Fatalf("Compress() error = %v", err)
			}
			got, err := Decompress(compressed)
			if err != nil {
				t.Fatalf("Decompress() error = %v", err)
			}
			if !bytes.Equal(got, tc.in) {
				t.Fatalf("往返不一致: got %d 字节, want %d 字节", len(got), len(tc.in))
			}
		})
	}
}

// TestCompressShrinksRepeatedJSON 覆盖压缩效果:重复 JSON 显著变小。
func TestCompressShrinksRepeatedJSON(t *testing.T) {
	in := bytes.Repeat([]byte(`{"role":"user","content":"hello world"}`), 1000)
	compressed, err := Compress(in)
	if err != nil {
		t.Fatalf("Compress() error = %v", err)
	}
	if len(compressed) >= len(in)/4 {
		t.Errorf("压缩后 %d 字节, want < %d", len(compressed), len(in)/4)
	}
}

// TestDecompressInvalid 覆盖非法输入:报错而非 panic。
func TestDecompressInvalid(t *testing.T) {
	if _, err := Decompress([]byte("not gzip")); err == nil {
		t.Fatal("Decompress(非法输入) error = nil, want error")
	}
}

// TestDecompressRejectsOversized 覆盖解压上限:超限报错,不撑爆内存。
func TestDecompressRejectsOversized(t *testing.T) {
	if testing.Short() {
		t.Skip("需要分配数百 MB,短模式跳过")
	}
	oversized := bytes.Repeat([]byte("a"), maxDecompressedBytes+1024)
	compressed, err := Compress(oversized)
	if err != nil {
		t.Fatalf("Compress() error = %v", err)
	}
	if _, err := Decompress(compressed); err == nil {
		t.Fatal("超限输入未报错")
	}
}
