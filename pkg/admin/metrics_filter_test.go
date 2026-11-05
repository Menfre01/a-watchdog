package admin

import (
	"context"
	"net/http"
	"testing"

	"awatchdog/pkg/config"
	"awatchdog/pkg/config/db"
)

func insertMetricsRequest(t *testing.T, store *config.Store, keyID int64, latency int64, status int64) int64 {
	t.Helper()
	ids, err := store.InsertRequests(context.Background(), []db.InsertRequestParams{{
		Ts:            "2026-10-13T09:00:00Z",
		ClientKeyID:   &keyID,
		ClientKeyName: "test-key",
		Protocol:      "anthropic-messages",
		StatusCode:    status,
		LatencyMs:     latency,
	}})
	if err != nil {
		t.Fatalf("InsertRequests() error = %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("InsertRequests() 返回 %d 个 id, want 1", len(ids))
	}
	return ids[0]
}

func insertMetricsSnapshot(t *testing.T, store *config.Store, requestID int64, sessionID string) {
	t.Helper()
	err := store.InsertSnapshot(context.Background(), db.InsertSnapshotParams{
		RequestID:     requestID,
		SessionID:     sessionID,
		SessionSource: "explicit",
		SessionNote:   "explicit: test",
		Seq:           1,
		Ts:            "2026-10-13T09:00:00Z",
		NdjsonGzip:    []byte{0x1f, 0x8b, 0x08},
		LineHashes:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
		LineSizes:     []byte{0, 0, 0, 5},
	})
	if err != nil {
		t.Fatalf("InsertSnapshot() error = %v", err)
	}
}

// TestAdmin_RequestMetricsFilters 覆盖 key 与 session 过滤,
// 并确认无快照请求只在 key 过滤中保留。
func TestAdmin_RequestMetricsFilters(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)

	keyA, keyB := int64(1), int64(2)
	a1 := insertMetricsRequest(t, store, keyA, 10, 200)
	a2 := insertMetricsRequest(t, store, keyA, 20, 200)
	b1 := insertMetricsRequest(t, store, keyB, 30, 200)
	_ = insertMetricsRequest(t, store, keyA, 40, 500) // 无快照
	insertMetricsSnapshot(t, store, a1, "1:alpha-1")
	insertMetricsSnapshot(t, store, a2, "1:alpha-2")
	insertMetricsSnapshot(t, store, b1, "2:beta-1")

	cases := []struct {
		name      string
		query     string
		wantCount float64
		wantP95   float64
		wantErr   float64
	}{
		{"全部", "window=all", 4, 40, 1},
		{"仅 key", "window=all&key_id=1", 3, 40, 1},
		{"仅 session", "window=all&q=alpha", 2, 20, 0},
		{"key 与 session 叠加", "window=all&key_id=1&q=alpha", 2, 20, 0},
		{"key 不匹配", "window=all&key_id=2&q=alpha", 0, 0, 0},
		{"另一 key", "window=all&key_id=2&q=beta", 1, 30, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out map[string]any
			resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/metrics?"+tc.query, "", cookie, false)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("读取指标 = %d, want 200", resp.StatusCode)
			}
			decode(t, resp, &out)
			if out["count"] != tc.wantCount {
				t.Errorf("count = %v, want %v", out["count"], tc.wantCount)
			}
			if out["p95_ms"] != tc.wantP95 {
				t.Errorf("p95 = %v, want %v", out["p95_ms"], tc.wantP95)
			}
			if out["errors"] != tc.wantErr {
				t.Errorf("errors = %v, want %v", out["errors"], tc.wantErr)
			}
		})
	}
}

func TestAdmin_RequestMetricsInvalidKey(t *testing.T) {
	_, _, srv := newTestAdmin(t)
	cookie := login(t, srv)
	for _, key := range []string{"abc", "0", "-1"} {
		resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/metrics?window=all&key_id="+key, "", cookie, false)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("key_id=%s = %d, want 400", key, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

func TestMetricsKeyID(t *testing.T) {
	if got, err := metricsKeyID(""); err != nil || got != nil {
		t.Errorf("metricsKeyID(空) = %v, %v; want nil, nil", got, err)
	}
	for _, raw := range []string{"abc", "0", "-1"} {
		if got, err := metricsKeyID(raw); err == nil {
			t.Errorf("metricsKeyID(%q) = %v, nil; want error", raw, got)
		}
	}
	id, err := metricsKeyID("7")
	if err != nil || id == nil || *id != 7 {
		t.Fatalf("metricsKeyID(7) = %v, %v", id, err)
	}
}

// TestAdmin_MetricsAndListKeyCountAgree 防止列表按 session_id 前缀、
// 指标按 client_key_id 的过滤谓词出现漂移。
func TestAdmin_MetricsAndListKeyCountAgree(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	keyA, keyB := int64(1), int64(2)
	a1 := insertMetricsRequest(t, store, keyA, 10, 200)
	a2 := insertMetricsRequest(t, store, keyA, 20, 200)
	b1 := insertMetricsRequest(t, store, keyB, 30, 200)
	insertMetricsSnapshot(t, store, a1, "1:alpha-1")
	insertMetricsSnapshot(t, store, a2, "1:alpha-2")
	insertMetricsSnapshot(t, store, b1, "2:beta-1")

	var list []map[string]any
	decode(t, do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/traces?key_id=1", "", cookie, false), &list)
	var steps float64
	for _, row := range list {
		n, ok := row["steps"].(float64)
		if !ok {
			t.Fatalf("steps 类型 = %T, want float64", row["steps"])
		}
		steps += n
	}
	if steps != 2 {
		t.Fatalf("列表 key 过滤 steps = %v, want 2", steps)
	}

	var metrics map[string]any
	decode(t, do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/metrics?window=all&key_id=1", "", cookie, false), &metrics)
	if metrics["count"] != steps {
		t.Fatalf("指标 count = %v, 列表 steps = %v", metrics["count"], steps)
	}
}

func insertMetricsErrorRequest(t *testing.T, store *config.Store, keyID int64, latency int64, status int64, kind string) int64 {
	t.Helper()
	p := db.InsertRequestParams{
		Ts:            "2026-10-13T09:00:00Z",
		ClientKeyID:   &keyID,
		ClientKeyName: "test-key",
		Protocol:      "anthropic-messages",
		StatusCode:    status,
		LatencyMs:     latency,
	}
	if kind != "" {
		p.ErrorKind = &kind
	}
	ids, err := store.InsertRequests(context.Background(), []db.InsertRequestParams{p})
	if err != nil {
		t.Fatalf("InsertRequests() error = %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("InsertRequests() 返回 %d 个 id, want 1", len(ids))
	}
	return ids[0]
}

// TestAdmin_RequestMetricsBreakdown 覆盖错误构成与成功请求延迟。
func TestAdmin_RequestMetricsBreakdown(t *testing.T) {
	_, store, srv := newTestAdmin(t)
	cookie := login(t, srv)
	keyA, keyB := int64(1), int64(2)
	a1 := insertMetricsRequest(t, store, keyA, 10, 200)
	a2 := insertMetricsErrorRequest(t, store, keyA, 20, 404, "model_not_found")
	_ = insertMetricsErrorRequest(t, store, keyB, 30, 500, "")
	_ = insertMetricsErrorRequest(t, store, keyB, 40, 200, "stream_interrupted")
	_ = insertMetricsRequest(t, store, keyA, 50, 200) // 无快照
	insertMetricsSnapshot(t, store, a1, "1:alpha")
	insertMetricsSnapshot(t, store, a2, "1:alpha")

	cases := []struct {
		name        string
		query       string
		wantCount   int
		wantErrors  int
		want4xx     int
		want5xx     int
		wantKinds   int
		wantSuccess int
		wantP95     int
	}{
		{"全部", "window=all", 5, 3, 1, 1, 3, 2, 50},
		{"仅 key", "window=all&key_id=1", 3, 1, 1, 0, 1, 2, 50},
		{"仅 session", "window=all&q=alpha", 2, 1, 1, 0, 1, 1, 10},
		{"无成功请求", "window=all&key_id=2", 2, 2, 0, 1, 2, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out struct {
				Count      int `json:"count"`
				Errors     int `json:"errors"`
				Errors4xx  int `json:"errors_4xx"`
				Errors5xx  int `json:"errors_5xx"`
				ErrorKinds []struct {
					Kind  string `json:"kind"`
					Count int    `json:"count"`
				} `json:"error_kinds"`
				SuccessCount int `json:"success_count"`
				SuccessP95Ms int `json:"success_p95_ms"`
				SuccessP99Ms int `json:"success_p99_ms"`
			}
			resp := do(t, srv.Client(), http.MethodGet, srv.URL+"/console/api/metrics?"+tc.query, "", cookie, false)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("读取指标 = %d, want 200", resp.StatusCode)
			}
			decode(t, resp, &out)
			if out.Count != tc.wantCount || out.Errors != tc.wantErrors {
				t.Errorf("count/errors = %d/%d, want %d/%d", out.Count, out.Errors, tc.wantCount, tc.wantErrors)
			}
			if out.Errors4xx != tc.want4xx || out.Errors5xx != tc.want5xx {
				t.Errorf("4xx/5xx = %d/%d, want %d/%d", out.Errors4xx, out.Errors5xx, tc.want4xx, tc.want5xx)
			}
			if len(out.ErrorKinds) != tc.wantKinds {
				t.Errorf("错误原因条数 = %d, want %d: %#v", len(out.ErrorKinds), tc.wantKinds, out.ErrorKinds)
			}
			if out.SuccessCount != tc.wantSuccess {
				t.Errorf("success_count = %d, want %d", out.SuccessCount, tc.wantSuccess)
			}
			if out.SuccessP95Ms != tc.wantP95 || out.SuccessP99Ms != tc.wantP95 {
				t.Errorf("成功分位 = p95 %d / p99 %d, want %d/%d", out.SuccessP95Ms, out.SuccessP99Ms, tc.wantP95, tc.wantP95)
			}
			var kindTotal int
			for _, kind := range out.ErrorKinds {
				kindTotal += kind.Count
			}
			if kindTotal != out.Errors {
				t.Errorf("错误原因合计 = %d, want %d", kindTotal, out.Errors)
			}
			if out.SuccessCount+out.Errors != out.Count {
				t.Errorf("成功数 + 错误数 = %d, want count %d", out.SuccessCount+out.Errors, out.Count)
			}
		})
	}
}
