package config

// ProviderInfo 是快照中的上游接入信息。
type ProviderInfo struct {
	ID         int64
	Name       string
	Protocol   string
	BaseURL    string
	APIKey     string
	ProbeModel string
	Enabled    bool
}

// ModelInfo 是快照中的模型目录条目信息(按客户端可见名检索)。
type ModelInfo struct {
	Name          string
	ProviderID    int64
	UpstreamModel string
	Enabled       bool
}

// KeyInfo 是快照中的客户端密钥信息(不含明文与哈希)。
type KeyInfo struct {
	ID      int64
	Name    string
	Enabled bool
}

// Snapshot 是配置的进程内一致性视图:请求路径只读快照,不查询数据库;
// 写路径落库成功后整体重建并原子替换,旧视图保持不变
// (由此天然满足「单次请求内视图一致」与「写后新请求即刻生效」)。
type Snapshot struct {
	providers    map[int64]ProviderInfo
	modelsByName map[string]ModelInfo
	keysByHash   map[string]KeyInfo
	settings     map[string]string
}

// LookupProvider 按 id 检索上游接入。
func (s *Snapshot) LookupProvider(id int64) (ProviderInfo, bool) {
	p, ok := s.providers[id]
	return p, ok
}

// LookupModel 按客户端可见名检索模型目录条目。
func (s *Snapshot) LookupModel(name string) (ModelInfo, bool) {
	m, ok := s.modelsByName[name]
	return m, ok
}

// LookupKey 按密钥哈希检索客户端密钥。
func (s *Snapshot) LookupKey(keyHash string) (KeyInfo, bool) {
	k, ok := s.keysByHash[keyHash]
	return k, ok
}

// Setting 检索全局配置项。
func (s *Snapshot) Setting(key string) (string, bool) {
	v, ok := s.settings[key]
	return v, ok
}
