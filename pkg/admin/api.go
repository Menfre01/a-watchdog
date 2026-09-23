package admin

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"ds-context/pkg/config"
	"ds-context/pkg/config/db"
	"ds-context/pkg/protocol"
)

// ---- providers ----

// handleListProviders 返回全部上游接入。
func (a *Admin) handleListProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListProviders(r.Context())
	if err != nil {
		a.storeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		out = append(out, providerJSON(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateProvider 新建上游接入(协议必须在注册表中)。
func (a *Admin) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		Protocol   string `json:"protocol"`
		BaseURL    string `json:"base_url"`
		APIKey     string `json:"api_key"`
		ProbeModel string `json:"probe_model"`
		Enabled    bool   `json:"enabled"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.BaseURL == "" {
		writeJSONError(w, http.StatusBadRequest, "name and base_url are required")
		return
	}
	if _, err := protocol.Get(req.Protocol); err != nil {
		writeJSONError(w, http.StatusBadRequest, "unknown protocol: "+req.Protocol)
		return
	}
	p, err := a.store.CreateProvider(r.Context(), db.CreateProviderParams{
		Name:       req.Name,
		Protocol:   req.Protocol,
		BaseUrl:    req.BaseURL,
		ApiKey:     req.APIKey,
		ProbeModel: req.ProbeModel,
		Enabled:    enabledInt(req.Enabled),
	})
	if err != nil {
		a.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, providerJSON(p))
}

// handleUpdateProvider 更新上游接入(整体替换)。
func (a *Admin) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Name       string `json:"name"`
		Protocol   string `json:"protocol"`
		BaseURL    string `json:"base_url"`
		APIKey     string `json:"api_key"`
		ProbeModel string `json:"probe_model"`
		Enabled    bool   `json:"enabled"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.BaseURL == "" {
		writeJSONError(w, http.StatusBadRequest, "name and base_url are required")
		return
	}
	if _, err := protocol.Get(req.Protocol); err != nil {
		writeJSONError(w, http.StatusBadRequest, "unknown protocol: "+req.Protocol)
		return
	}
	if err := a.store.UpdateProvider(r.Context(), db.UpdateProviderParams{
		ID:         id,
		Name:       req.Name,
		Protocol:   req.Protocol,
		BaseUrl:    req.BaseURL,
		ApiKey:     req.APIKey,
		ProbeModel: req.ProbeModel,
		Enabled:    enabledInt(req.Enabled),
	}); err != nil {
		a.storeErr(w, err)
		return
	}
	p, err := a.store.GetProvider(r.Context(), id)
	if err != nil {
		a.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, providerJSON(p))
}

// handleDeleteProvider 删除上游接入(被模型引用时返回 409)。
func (a *Admin) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.DeleteProvider(r.Context(), id); err != nil {
		a.storeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTestProvider 对指定 provider 执行连通性测试。
func (a *Admin) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := a.store.GetProvider(r.Context(), id)
	if err != nil {
		a.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.probeProvider(r.Context(), p))
}

// ---- models(模型目录) ----

// handleListModels 返回全部模型目录条目。
func (a *Admin) handleListModels(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListModels(r.Context())
	if err != nil {
		a.storeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, modelJSON(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateModel 新建模型目录条目。
func (a *Admin) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		ProviderID    int64  `json:"provider_id"`
		UpstreamModel string `json:"upstream_model"`
		Enabled       bool   `json:"enabled"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.ProviderID == 0 || req.UpstreamModel == "" {
		writeJSONError(w, http.StatusBadRequest, "name, provider_id and upstream_model are required")
		return
	}
	m, err := a.store.CreateModel(r.Context(), db.CreateModelParams{
		Name:          req.Name,
		ProviderID:    req.ProviderID,
		UpstreamModel: req.UpstreamModel,
		Enabled:       enabledInt(req.Enabled),
	})
	if err != nil {
		a.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, modelJSON(m))
}

// handleUpdateModel 更新模型目录条目(整体替换)。
func (a *Admin) handleUpdateModel(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Name          string `json:"name"`
		ProviderID    int64  `json:"provider_id"`
		UpstreamModel string `json:"upstream_model"`
		Enabled       bool   `json:"enabled"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.ProviderID == 0 || req.UpstreamModel == "" {
		writeJSONError(w, http.StatusBadRequest, "name, provider_id and upstream_model are required")
		return
	}
	if err := a.store.UpdateModel(r.Context(), db.UpdateModelParams{
		ID:            id,
		Name:          req.Name,
		ProviderID:    req.ProviderID,
		UpstreamModel: req.UpstreamModel,
		Enabled:       enabledInt(req.Enabled),
	}); err != nil {
		a.storeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteModel 删除模型目录条目。
func (a *Admin) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.DeleteModel(r.Context(), id); err != nil {
		a.storeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- client keys ----

// handleListClientKeys 返回全部客户端密钥(不含明文与哈希)。
func (a *Admin) handleListClientKeys(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListClientKeys(r.Context())
	if err != nil {
		a.storeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, k := range rows {
		out = append(out, clientKeyJSON(k))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateClientKey 生成新客户端密钥:明文仅在本响应中出现一次。
func (a *Admin) handleCreateClientKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "generate key failed")
		return
	}
	plain := "dsctx_" + hex.EncodeToString(raw)
	k, err := a.store.CreateClientKey(r.Context(), db.CreateClientKeyParams{
		Name:      req.Name,
		KeyHash:   config.HashClientKey(plain),
		KeyPrefix: plain[:10],
		Enabled:   1,
	})
	if err != nil {
		a.storeErr(w, err)
		return
	}
	resp := clientKeyJSON(k)
	resp["key"] = plain // 明文仅此一次返回
	writeJSON(w, http.StatusCreated, resp)
}

// handleUpdateClientKey 启用/停用客户端密钥(停用即时生效)。
func (a *Admin) handleUpdateClientKey(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := a.store.SetClientKeyEnabled(r.Context(), id, req.Enabled); err != nil {
		a.storeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- settings ----

// handleListSettings 返回全部全局配置项。
func (a *Admin) handleListSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ListSettings(r.Context())
	if err != nil {
		a.storeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, s := range rows {
		out = append(out, settingJSON(s))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleUpsertSetting 写入或覆盖一个全局配置项。
func (a *Admin) handleUpsertSetting(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeJSONError(w, http.StatusBadRequest, "key is required")
		return
	}
	var req struct {
		Value string `json:"value"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := a.store.UpsertSetting(r.Context(), key, req.Value); err != nil {
		a.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "value": req.Value})
}
