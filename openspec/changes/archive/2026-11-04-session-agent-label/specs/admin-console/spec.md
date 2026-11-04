# Spec Delta

## MODIFIED Requirements

### Requirement: 会话列表视图

会话列表 **必须(MUST)** 支持按客户端原始标识前缀、时间范围与 client key 过滤。原始标识前缀与 client key 两个条件 **必须** 可叠加。每行显示会话标识、**Agent 分类**、首见与末见时间、step 数与累计 token(`steps` 即该会话的请求数。会话归属由快照承载，二者恒等。)

会话标识 **必须** 以短前缀显示(前 8 位加省略号),完整值 **必须** 作为悬停提示与详情页标题。完整标识 **不得** 在列表里铺满整列。

Agent 分类 **必须** 取值于固定集合：**Claude Code**、**Codex**、**DSH**、**Other**。分类由判定理由派生，列表 **不得** 显示逐条的判定来源与理由。

列表 **不得** 显示轮次数。推断轮次需要对每个会话做指纹比对与首行解析，成本随会话数线性放大。轮次数由轨迹视图给出，列表页 **必须** 说明这一取舍。

#### Scenario: 列表与过滤

- **WHEN** 管理员按标识前缀过滤会话列表
- **THEN** 只列出匹配的会话。每行显示 Agent 分类、首末时间、step 数与累计 token

#### Scenario: 按密钥过滤

- **WHEN** 管理员选择某个 client key 作为过滤条件
- **THEN** 只列出该密钥下的会话，其他密钥的会话不出现

#### Scenario: 两个条件叠加

- **WHEN** 管理员同时选择 client key 并输入标识前缀
- **THEN** 只列出该密钥下标识前缀匹配的会话

#### Scenario: 标识显示短前缀

- **WHEN** 管理员查看会话列表
- **THEN** 标识显示为前 8 位加省略号，完整值可在悬停时看到

#### Scenario: Agent 分类来自判定头

- **WHEN** 会话由 `x-claude-code-session-id` 判定
- **THEN** 该行的 Agent 分类显示为 Claude Code

#### Scenario: 无标识归为 Other

- **WHEN** 会话是按天分桶的匿名会话
- **THEN** 该行的 Agent 分类显示为 Other

#### Scenario: 不含轮次数

- **WHEN** 管理员打开会话列表
- **THEN** 列表不显示轮次数，并可见说明其成本原因

#### Scenario: 标识不带密钥前缀

- **WHEN** 管理员查看会话列表或轨迹头部
- **THEN** 标识显示为客户端给的原始串，不以 `<key_id>:` 开头

