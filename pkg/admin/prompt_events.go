package admin

import "net/http"

// handleListPromptEvents 返回某会话的提示提交事件,按时间升序。
func (a *Admin) handleListPromptEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	rows, err := a.store.ListPromptEvents(r.Context(), sessionID)
	if err != nil {
		a.storeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"session_id": row.SessionID,
			"turn_id":    row.TurnID,
			"prompt_id":  row.PromptID,
			"prompt_len": row.PromptLen,
			"ts":         row.Ts,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
