package admin

import (
	"errors"
	"net/http"
	"strconv"
	"time"
)

// metricsWindow 返回统计窗口的起点;all 表示不限时间。
func metricsWindow(raw string) string {
	now := time.Now().UTC()
	switch raw {
	case "1h":
		return now.Add(-time.Hour).Format(time.RFC3339Nano)
	case "7d":
		return now.Add(-7 * 24 * time.Hour).Format(time.RFC3339Nano)
	case "all":
		return ""
	default: // 24h 为默认窗口
		return now.Add(-24 * time.Hour).Format(time.RFC3339Nano)
	}
}

// percentile 取升序样本的第 p 百分位;样本为空时返回 0。
func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted))*p+0.999999) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

// metricsKeyID 解析可选的 key_id;空值表示全部密钥。
func metricsKeyID(raw string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return nil, errors.New("invalid key_id")
	}
	return &id, nil
}

// handleRequestMetrics 返回请求侧的汇总指标。
func (a *Admin) handleRequestMetrics(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	since := metricsWindow(window)
	keyID, err := metricsKeyID(r.URL.Query().Get("key_id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	sessionPrefix := r.URL.Query().Get("q")
	agg, err := a.store.AggregateRequestsSince(r.Context(), since, keyID, sessionPrefix)
	if err != nil {
		a.storeErr(w, err)
		return
	}
	latencies, err := a.store.ListRequestLatenciesSince(r.Context(), since, keyID, sessionPrefix)
	if err != nil {
		a.storeErr(w, err)
		return
	}
	errorKinds, err := a.store.ListRequestErrorKindsSince(r.Context(), since, keyID, sessionPrefix)
	if err != nil {
		a.storeErr(w, err)
		return
	}
	successLatencies, err := a.store.ListSuccessfulRequestLatenciesSince(r.Context(), since, keyID, sessionPrefix)
	if err != nil {
		a.storeErr(w, err)
		return
	}
	errorRate := 0.0
	if agg.Count > 0 {
		errorRate = float64(agg.Errors) / float64(agg.Count)
	}
	// 缓存命中率按「缓存读取 ÷(输入 + 缓存读取)」计算,分母为 0 时记 0。
	cacheRate := 0.0
	if agg.UsageIn+agg.UsageCache > 0 {
		cacheRate = float64(agg.UsageCache) / float64(agg.UsageIn+agg.UsageCache)
	}
	kinds := make([]map[string]any, 0, len(errorKinds))
	for _, row := range errorKinds {
		kinds = append(kinds, map[string]any{"kind": row.Kind, "count": row.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":          agg.Count,
		"errors":         agg.Errors,
		"errors_4xx":     agg.Errors4xx,
		"errors_5xx":     agg.Errors5xx,
		"error_rate":     errorRate,
		"error_kinds":    kinds,
		"p50_ms":         percentile(latencies, 0.50),
		"p95_ms":         percentile(latencies, 0.95),
		"p99_ms":         percentile(latencies, 0.99),
		"success_count":  len(successLatencies),
		"success_p95_ms": percentile(successLatencies, 0.95),
		"success_p99_ms": percentile(successLatencies, 0.99),
		"usage_in":       agg.UsageIn,
		"usage_out":      agg.UsageOut,
		"usage_cache":    agg.UsageCache,
		"cache_rate":     cacheRate,
	})
}
