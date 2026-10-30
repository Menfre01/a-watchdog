package main

import (
	"context"
	"strings"
	"testing"
)

// TestExpandLoopback 覆盖地址展开:loopback 展开为双栈,其余原样。
func TestExpandLoopback(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"IPv4 loopback", "127.0.0.1:8399", []string{"127.0.0.1:8399", "[::1]:8399"}},
		{"localhost", "localhost:8399", []string{"127.0.0.1:8399", "[::1]:8399"}},
		{"IPv6 loopback", "[::1]:8399", []string{"127.0.0.1:8399", "[::1]:8399"}},
		{"全接口不动", "0.0.0.0:8399", []string{"0.0.0.0:8399"}},
		{"具名主机不动", "192.168.1.5:8399", []string{"192.168.1.5:8399"}},
		{"无端口原样", "bad-addr", []string{"bad-addr"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandLoopback(tc.in)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("expandLoopback(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestListenLoopbackAndConsoleURL 覆盖真实监听:至少一侧可用,入口 URL 形态正确。
func TestListenLoopbackAndConsoleURL(t *testing.T) {
	listeners, err := listenLoopback(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listenLoopback() error = %v", err)
	}
	t.Cleanup(func() {
		for _, ln := range listeners {
			_ = ln.Close()
		}
	})

	url := consoleURL(listeners[0].Addr())
	if !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.HasSuffix(url, "/console/") {
		t.Errorf("consoleURL() = %q", url)
	}
}

// TestListenLoopbackInvalidAddr 覆盖非法地址:报错而非静默成功。
func TestListenLoopbackInvalidAddr(t *testing.T) {
	if _, err := listenLoopback(context.Background(), "127.0.0.1:not-a-port"); err == nil {
		t.Fatal("非法端口未报错")
	}
}
