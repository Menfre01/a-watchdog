package trace

import "testing"

// sysMsg 构造一条 system 消息(测试夹具)。
func sysMsg(text string) MainlineMessage {
	return MainlineMessage{Role: "system", Blocks: []MainlineBlock{{"type": "text", "text": text}}}
}

// TestSystemFingerprintIgnoresVersion 覆盖版本号抖动:
// 计费头里的 cc_version 每个进程可能不同,不剔除会让重启后的主链断裂。
func TestSystemFingerprintIgnoresVersion(t *testing.T) {
	a := SystemFingerprint([]MainlineMessage{
		sysMsg("x-anthropic-billing-header: cc_version=2.1.300.ca9; cc_entrypoint=cli;\nYou are Claude Code"),
	})
	b := SystemFingerprint([]MainlineMessage{
		sysMsg("x-anthropic-billing-header: cc_version=2.1.300.d7e; cc_entrypoint=cli;\nYou are Claude Code"),
	})
	if a == "" || a != b {
		t.Fatalf("版本号不同但指纹应相同: %q vs %q", a, b)
	}
	// 非版本部分的差异仍应反映在指纹上。
	c := SystemFingerprint([]MainlineMessage{
		sysMsg("x-anthropic-billing-header: cc_version=2.1.300.ca9; cc_entrypoint=cli;\nYou are a security monitor"),
	})
	if a == c {
		t.Errorf("正文不同却指纹相同: %q", a)
	}
}

// TestSystemFingerprintIgnoresDeepConfigDrift 覆盖深处的配置漂移:
// 客户端把实时配置嵌在 system 深处(实测同一类监控在第 9 万字符处分叉),
// 取前缀即可忽略这类与请求类型无关的差异。
func TestSystemFingerprintIgnoresDeepConfigDrift(t *testing.T) {
	head := "You are a security monitor for autonomous AI coding agents."
	long := make([]byte, 0, 6000)
	for len(long) < 5000 {
		long = append(long, 'x')
	}
	a := SystemFingerprint([]MainlineMessage{sysMsg(head + string(long) + "- User Deny Rules: none")})
	b := SystemFingerprint([]MainlineMessage{sysMsg(head + string(long) + "- User Deny Rules: `Artifact publish`")})
	if a != b {
		t.Fatalf("前缀相同、深处配置不同,指纹应相同: %q vs %q", a, b)
	}
}

// TestUserFingerprint 覆盖实例指纹:取首条 user 的文本前缀。
func TestUserFingerprint(t *testing.T) {
	long := ""
	for len(long) < 300 {
		long += "x"
	}
	a := UserFingerprint([]MainlineMessage{
		{Role: "user", Blocks: []MainlineBlock{{"type": "text", "text": "在仓库 /tmp 做一次探索" + long}}},
	})
	b := UserFingerprint([]MainlineMessage{
		{Role: "user", Blocks: []MainlineBlock{{"type": "text", "text": "在仓库 /tmp 做一次探索" + long + "尾部不同"}}},
	})
	if a == "" || a != b {
		t.Errorf("前缀相同应同指纹: %q vs %q", a, b)
	}
	c := UserFingerprint([]MainlineMessage{
		{Role: "user", Blocks: []MainlineBlock{{"type": "text", "text": "做另一件事" + long}}},
	})
	if a == c {
		t.Errorf("前缀不同却指纹相同: %q", a)
	}
}

// TestUserFingerprintEdges 覆盖边界:无 user、首条 user 无文本、空块。
func TestUserFingerprintEdges(t *testing.T) {
	if got := UserFingerprint(nil); got != "" {
		t.Errorf("空消息 = %q, want 空", got)
	}
	if got := UserFingerprint([]MainlineMessage{sysMsg("sys")}); got != "" {
		t.Errorf("无 user 消息 = %q, want 空", got)
	}
	// 首条 user 只有工具结果:不向后找,返回空交由 system 指纹判定。
	got := UserFingerprint([]MainlineMessage{
		{Role: "user", Blocks: []MainlineBlock{{"type": "tool_result", "tool_use_id": "t1", "content": "x"}}},
		{Role: "user", Blocks: []MainlineBlock{{"type": "text", "text": "后面的用户输入"}}},
	})
	if got != "" {
		t.Errorf("首条 user 无文本时应返回空: %q", got)
	}
	// 空白文本不算。
	blank := UserFingerprint([]MainlineMessage{
		{Role: "user", Blocks: []MainlineBlock{{"type": "text", "text": "   \n  "}}},
	})
	if blank != "" {
		t.Errorf("空白文本 = %q, want 空", blank)
	}
}
