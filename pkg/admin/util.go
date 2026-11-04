package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"awatchdog/pkg/config"
	"awatchdog/pkg/config/db"
)

// maxAdminBody 是管理 API 请求体的体积上限。
const maxAdminBody = 1 << 20

// writeJSON 以 JSON 输出响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// 编码失败意味着响应已部分写出,只能放弃(固定结构下理论不可达)。
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError 输出 {"error": message}。
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// decodeJSON 解析请求体(限 maxAdminBody)。
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAdminBody))
	if err := dec.Decode(v); err != nil {
		return err
	}
	// 收紧输入面:拒绝尾随内容(第二个 JSON 值或垃圾)。
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing content")
	}
	return nil
}

// pathID 解析 {id} 路径参数。
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

// storeErr 把存储错误映射为 HTTP 状态输出:未找到 → 404,唯一/外键冲突 → 409,其余 → 500。
func (a *Admin) storeErr(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	msg := err.Error()
	switch {
	case errors.Is(err, config.ErrNotFound):
		status = http.StatusNotFound
	case strings.Contains(msg, "UNIQUE constraint failed"), strings.Contains(msg, "FOREIGN KEY constraint failed"):
		status = http.StatusConflict
	}
	writeJSONError(w, status, msg)
}

// providerJSON 是 provider 的 API 视图。
func providerJSON(p db.Provider) map[string]any {
	return map[string]any{
		"id":          p.ID,
		"name":        p.Name,
		"protocol":    p.Protocol,
		"base_url":    p.BaseUrl,
		"api_key":     p.ApiKey,
		"probe_model": p.ProbeModel,
		"enabled":     p.Enabled != 0,
	}
}

// modelJSON 是模型目录条目的 API 视图。
func modelJSON(m db.Model) map[string]any {
	return map[string]any{
		"id":             m.ID,
		"name":           m.Name,
		"provider_id":    m.ProviderID,
		"upstream_model": m.UpstreamModel,
		"enabled":        m.Enabled != 0,
	}
}

// clientKeyJSON 是客户端密钥的 API 视图(不含哈希;明文仅创建时附带)。
func clientKeyJSON(k db.ClientKey) map[string]any {
	return map[string]any{
		"id":         k.ID,
		"name":       k.Name,
		"key_prefix": k.KeyPrefix,
		"enabled":    k.Enabled != 0,
		"created_at": k.CreatedAt,
	}
}

// settingJSON 是全局配置项的 API 视图。
func settingJSON(s db.Setting) map[string]any {
	return map[string]any{"key": s.Key, "value": s.Value}
}

// intFromBoolStr 兼容 JSON bool 的 enabled 字段转换为 0/1。
func enabledInt(enabled bool) int64 { return config.BoolToInt(enabled) }
