package admin

import (
	"context"
	"net/http"
	"testing"
	"time"

	"awatchdog/pkg/config/db"
)

// TestAdmin_RequestMetrics 覆盖请求指标:计数、错误率与分位数。
func TestAdmin_RequestMetrics(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	latencies := []int64{100, 200, 300, 400}
	reqs := make([]db.InsertRequestParams, 0, len(latencies))
	for i, ms := range latencies {
		status := int64(200)
		if i == 3 {
			status = 500
		}
		reqs = append(reqs, db.InsertRequestParams{
			Ts: now, Protocol: "anthropic-messages", StatusCode: status,
			LatencyMs: ms, Stream: 1,
		})
	}
	if _, err := store.InsertRequests(context.Background(), reqs); err != nil {
		t.Fatalf("InsertRequests() error = %v", err)
	}

	resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/metrics?window=all", "", cookie, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取指标 = %d, want 200", resp.StatusCode)
	}
	var out map[string]any
	decode(t, resp, &out)
	if out["count"] != float64(4) || out["errors"] != float64(1) {
		t.Fatalf("计数 = %v / %v", out["count"], out["errors"])
	}
	if out["p50_ms"] != float64(200) || out["p95_ms"] != float64(400) {
		t.Fatalf("分位 = p50 %v, p95 %v", out["p50_ms"], out["p95_ms"])
	}
	if out["error_rate"] != 0.25 {
		t.Fatalf("错误率 = %v, want 0.25", out["error_rate"])
	}
}

// TestPercentile 覆盖分位数的边界。
func TestPercentile(t *testing.T) {
	cases := []struct {
		name string
		in   []int64
		p    float64
		want int64
	}{
		{"空样本", nil, 0.95, 0},
		{"单样本", []int64{42}, 0.95, 42},
		{"四样本中位", []int64{100, 200, 300, 400}, 0.5, 200},
		{"四样本 p95", []int64{100, 200, 300, 400}, 0.95, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := percentile(c.in, c.p); got != c.want {
				t.Fatalf("percentile(%v, %v) = %d, want %d", c.in, c.p, got, c.want)
			}
		})
	}
}
