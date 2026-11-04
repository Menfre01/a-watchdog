package trace

import "testing"

// TestRegression_OneShotFirstStepIsNotMain 覆盖回归根因:会话首步可能是一次性
// 自动请求(Claude Code 的「生成会话标题」)。旧判据「含会话首步者为主链」会把
// 这条 1 步的链当成主链,真正的主对话反而落入旁路。
//
// REGRESSION: 首步为一次性请求时主链误判。判据改为「至少两个非重复步的链中最早者」。
func TestRegression_OneShotFirstStepIsNotMain(t *testing.T) {
	steps := []SnapshotLines{
		chainOf(t, "title", "title-sys", "naming"), // 一次性,但时间最早
		chainOf(t, "main", "sys", "u1"),
		chainOf(t, "monitor", "sec-sys", "t1"),
		chainOf(t, "main", "sys", "u1", "a1"),
		chainOf(t, "monitor", "sec-sys", "t2"),
		chainOf(t, "main", "sys", "u1", "a1", "u2"),
	}
	chains := AnalyzeSession(steps)
	if got, want := chainShape(chains), "旁路=[0] 主链=[1,3,5] 旁路=[2,4]"; got != want {
		t.Fatalf("链归属 = %s, want %s", got, want)
	}
}

// TestAnalyzeSessionAllOneShotFallsBackToFirstStep 覆盖退化边界:会话内没有
// 至少两个非重复步的链时,退回「含会话首步者为主链」。
func TestAnalyzeSessionAllOneShotFallsBackToFirstStep(t *testing.T) {
	steps := []SnapshotLines{
		chainOf(t, "title", "title-sys", "naming"),
		chainOf(t, "main", "sys", "u1"),
		chainOf(t, "monitor", "sec-sys", "t1"),
	}
	chains := AnalyzeSession(steps)
	if got, want := chainShape(chains), "主链=[0] 旁路=[1] 旁路=[2]"; got != want {
		t.Fatalf("链归属 = %s, want %s", got, want)
	}
}

// TestRegression_OneShotRetryDoesNotBecomeMain 覆盖一次性请求被客户端重试的形态:
// 重试只重复内容,不构成持续性,标题链仍为旁路。
//
// REGRESSION: 重试步被计入「出现两次」门槛,标题链夺取主链。
func TestRegression_OneShotRetryDoesNotBecomeMain(t *testing.T) {
	steps := []SnapshotLines{
		chainOf(t, "title", "title-sys", "naming"),
		chainOf(t, "title", "title-sys", "naming"),
		chainOf(t, "main", "sys", "u1"),
		chainOf(t, "main", "sys", "u1", "a1"),
	}
	chains := AnalyzeSession(steps)
	if got, want := chainShape(chains), "旁路=[0,1] 主链=[2,3]"; got != want {
		t.Fatalf("链归属 = %s, want %s", got, want)
	}
}
