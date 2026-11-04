package admin

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"awatchdog/pkg/trace"
)

// TestRegression_OneShotFirstStepKeepsMainChain 覆盖回归根因:会话首步是一次性
// 自动请求(生成标题)时,主路统计必须落在主对话链上,标题与监控都算旁路。
//
// REGRESSION: 首步为一次性请求导致主链误判,主对话被标成旁路。
func TestRegression_OneShotFirstStepKeepsMainChain(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	titleSys := trace.MainlineMessage{Role: "system", Blocks: []trace.MainlineBlock{{"type": "text", "text": "你是会话标题生成器"}}}
	mainSys := trace.MainlineMessage{Role: "system", Blocks: []trace.MainlineBlock{{"type": "text", "text": "你是主对话助手"}}}
	sideSys := trace.MainlineMessage{Role: "system", Blocks: []trace.MainlineBlock{{"type": "text", "text": "你是安全监控"}}}
	naming := trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": "给这个会话起个标题"}}}
	task := trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": "帮我看看登录接口"}}}
	answer := trace.MainlineMessage{Role: "assistant", Blocks: []trace.MainlineBlock{{"type": "text", "text": "先看路由"}}}
	ask := trace.MainlineMessage{Role: "user", Blocks: []trace.MainlineBlock{{"type": "text", "text": "检查这一步"}}}

	// 标题请求先到,只出现一次;主对话与监控交错重复出现。
	writeStep(t, store, "2026-10-13T10:00:00Z", 200, 100, []trace.MainlineMessage{titleSys, naming})
	writeStep(t, store, "2026-10-13T10:00:01Z", 200, 100, []trace.MainlineMessage{mainSys, task})
	writeStep(t, store, "2026-10-13T10:00:02Z", 200, 100, []trace.MainlineMessage{sideSys, ask})
	writeStep(t, store, "2026-10-13T10:00:03Z", 200, 100, []trace.MainlineMessage{mainSys, task, answer})
	writeStep(t, store, "2026-10-13T10:00:04Z", 200, 100, []trace.MainlineMessage{sideSys, ask})
	writeStep(t, store, "2026-10-13T10:00:05Z", 200, 100, []trace.MainlineMessage{mainSys, task, answer, task})

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID, "", cookie, false)
	var body traceJSON
	decode(t, resp, &body)

	chains, _ := body["chains"].(map[string]any)
	if chains["main"] != float64(1) || chains["side"] != float64(2) {
		t.Fatalf("链汇总 = %v, want 主链 1 旁路 2", chains)
	}
	mainSplit, _ := body["main"].(map[string]any)
	if mainSplit["steps"] != float64(3) {
		t.Errorf("主路步数 = %v, want 3(主对话链)", mainSplit["steps"])
	}
	sideSplit, _ := body["side"].(map[string]any)
	if sideSplit["steps"] != float64(3) {
		t.Errorf("旁路步数 = %v, want 3(标题 1 + 监控 2)", sideSplit["steps"])
	}

	// 旁路编号从 1 起连续,且不依赖主链位于下标 0。
	seen := map[float64]bool{}
	mainFirstSeq := float64(0)
	mainFirstNote := ""
	storyFirst := ""
	turns, _ := body["turns"].([]any)
	for _, rawTurn := range turns {
		turn, _ := rawTurn.(map[string]any)
		if storyFirst == "" {
			if story, ok := turn["story"].([]any); ok && len(story) > 0 {
				part, _ := story[0].(map[string]any)
				storyFirst, _ = part["text"].(string)
			}
		}
		steps, _ := turn["steps"].([]any)
		for _, rawStep := range steps {
			step, _ := rawStep.(map[string]any)
			delta, _ := step["delta"].(map[string]any)
			if step["side"] == true || delta["kind"] != "new" {
				continue
			}
			mainFirstSeq, _ = step["seq"].(float64)
			mainFirstNote, _ = delta["note"].(string)
		}
		for _, rawStep := range steps {
			step, _ := rawStep.(map[string]any)
			if step["side"] != true {
				continue
			}
			n, _ := step["side_chain"].(float64)
			seen[n] = true
		}
	}
	nums := make([]int, 0, len(seen))
	for n := range seen {
		nums = append(nums, int(n))
	}
	sort.Ints(nums)
	want := []int{1, 2}
	if len(nums) != len(want) {
		t.Fatalf("旁路编号 = %v, want %v", nums, want)
	}
	for i := range want {
		if nums[i] != want[i] {
			t.Fatalf("旁路编号 = %v, want %v", nums, want)
		}
	}
	if !strings.Contains(mainFirstNote, "主链首步") {
		t.Errorf("主链首步 note = %q, want 含「主链首步」", mainFirstNote)
	}
	if storyFirst != "首步" {
		t.Errorf("首轮 story 首段 = %q, want 首步", storyFirst)
	}
	if mainFirstSeq == 0 {
		t.Fatal("未找到主链首步")
	}
	resp = do(t, srv.Client(), http.MethodGet,
		fmt.Sprintf("%s/console/api/traces/%s/%d", srv.URL, testSessionID, int64(mainFirstSeq)),
		"", cookie, false)
	var detail map[string]any
	decode(t, resp, &detail)
	detailDelta, _ := detail["delta"].(map[string]any)
	detailNote, _ := detailDelta["note"].(string)
	if !strings.Contains(detailNote, "主链首步") {
		t.Errorf("主链首步详情 note = %q, want 含「主链首步」", detailNote)
	}
}

func mainChainMsg(role, text string) trace.MainlineMessage {
	return trace.MainlineMessage{Role: role, Blocks: []trace.MainlineBlock{{"type": "text", "text": text}}}
}

// TestAdmin_SideChainNumbersStayContinuous 覆盖三条旁路链的编号:
// 主链不在下标 0 时,旁路编号仍从 1 起连续。
func TestAdmin_SideChainNumbersStayContinuous(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	titleSys := mainChainMsg("system", "你是会话标题生成器")
	mainSys := mainChainMsg("system", "你是主对话助手")
	monitorSys := mainChainMsg("system", "你是安全监控")
	agentSys := mainChainMsg("system", "你是子 agent")
	naming := mainChainMsg("user", "给这个会话起个标题")
	task := mainChainMsg("user", "帮我看看登录接口")
	answer := mainChainMsg("assistant", "先看路由")
	monitor := mainChainMsg("user", "检查这一步")
	agent := mainChainMsg("user", "整理测试清单")

	// 主链 [1,4,7];三条旁路链:标题、监控、子 agent。
	writeStep(t, store, "2026-10-13T10:01:00Z", 200, 100, []trace.MainlineMessage{titleSys, naming})
	writeStep(t, store, "2026-10-13T10:01:01Z", 200, 100, []trace.MainlineMessage{mainSys, task})
	writeStep(t, store, "2026-10-13T10:01:02Z", 200, 100, []trace.MainlineMessage{monitorSys, monitor})
	writeStep(t, store, "2026-10-13T10:01:03Z", 200, 100, []trace.MainlineMessage{agentSys, agent})
	writeStep(t, store, "2026-10-13T10:01:04Z", 200, 100, []trace.MainlineMessage{mainSys, task, answer})
	writeStep(t, store, "2026-10-13T10:01:05Z", 200, 100, []trace.MainlineMessage{monitorSys, monitor})
	writeStep(t, store, "2026-10-13T10:01:06Z", 200, 100, []trace.MainlineMessage{agentSys, agent})
	writeStep(t, store, "2026-10-13T10:01:07Z", 200, 100, []trace.MainlineMessage{mainSys, task, answer, task})

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces/"+testSessionID, "", cookie, false)
	var body traceJSON
	decode(t, resp, &body)

	chains, _ := body["chains"].(map[string]any)
	if chains["main"] != float64(1) || chains["side"] != float64(3) {
		t.Fatalf("链汇总 = %v, want 主链 1 旁路 3", chains)
	}
	mainSplit, _ := body["main"].(map[string]any)
	if mainSplit["steps"] != float64(3) {
		t.Errorf("主路步数 = %v, want 3", mainSplit["steps"])
	}
	sideSplit, _ := body["side"].(map[string]any)
	if sideSplit["steps"] != float64(5) {
		t.Errorf("旁路步数 = %v, want 5", sideSplit["steps"])
	}

	seen := map[float64]bool{}
	turns, _ := body["turns"].([]any)
	for _, rawTurn := range turns {
		turn, _ := rawTurn.(map[string]any)
		steps, _ := turn["steps"].([]any)
		for _, rawStep := range steps {
			step, _ := rawStep.(map[string]any)
			if step["side"] != true {
				continue
			}
			n, _ := step["side_chain"].(float64)
			seen[n] = true
		}
	}
	nums := make([]int, 0, len(seen))
	for n := range seen {
		nums = append(nums, int(n))
	}
	sort.Ints(nums)
	want := []int{1, 2, 3}
	if len(nums) != len(want) {
		t.Fatalf("旁路编号 = %v, want %v", nums, want)
	}
	for i := range want {
		if nums[i] != want[i] {
			t.Fatalf("旁路编号 = %v, want %v", nums, want)
		}
	}
}
