# Design

## Context

- 行为契约见本变更 `specs/`(trajectory-capture / trajectory-analysis / admin-console 增量);动机见 `proposal.md`。
- 依赖 C1 已落地的设施:责任链 finalizer(record 环节)、SQLite 存储与迁移、控制台会话与 API 形态、`chain.Context`(已携带请求体与头信息)。
- 三层结构的「可重放」级联:1 原始 body →(解析规则升级)→ 2 规范化快照 →(推断规则升级)→ 3 视图(增量分类 / 轮次 / tool 明细)。1 不可再生;2 记录时固化但可重放;3 每次现算。

## Goals / Non-Goals

**Goals:**
- 真实数据可用:Claude Code 等 agent 的会话在控制台中可见完整的 messages、每轮 tool 调用明细、耗时与 token 用量
- 保真优先:原始层全量、快照最大保真,规则可迭代
- 零干扰:捕捉全异步,任何捕捉故障不影响请求与记录

**Non-Goals:**
- 行级 LCS diff、压缩事件识别与概览指标(本轮不做)
- 判定重算与维护审计(判定一次性写入;纠错直接改数据)
- go-git / git 结构存储、MongoDB、docker-compose(均已在讨论中否决,维持单二进制 + SQLite)
- 全文检索、跨会话的语义分析、导出格式(后续按需)
- OpenTelemetry 导出(本轮不做)
- 轨迹数据的修改/删除能力(只读视图)
- 捕捉开关与保留策略(全量捕捉、全留不删,待有真实痛点再说)

## Decisions

### D1 存储模型(迁移 0003)

新增两张表;全部聚合(会话列表/计数)从数据推导,不预存:

```sql
-- 原始层:与请求记录一一关联(独立表,避免列表查询拖大字段)
request_bodies(
  request_id INTEGER PRIMARY KEY REFERENCES requests(id),
  method     TEXT NOT NULL,
  path       TEXT NOT NULL,
  query      TEXT NOT NULL,    -- 原始查询串(无前导 ?),无查询时为空串
  body_gzip  BLOB NOT NULL,
  headers    TEXT NOT NULL,    -- 入向请求头 canonical JSON(凭据类头值已脱敏,Host 已补入)
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
)

-- 事实层:每请求一条规范化快照
snapshots(
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id INTEGER NOT NULL UNIQUE REFERENCES requests(id),
  session_id TEXT    NOT NULL,          -- 判定结果(见 D4)
  session_source TEXT NOT NULL,         -- 'explicit' | 'anon'
  session_note TEXT  NOT NULL,          -- 判定理由(见 D4):命中头名/降级事件/匿名兜底
  seq        INTEGER NOT NULL,          -- 会话内序号(1 起,同 session_id 内递增)
  ts         TEXT    NOT NULL,
  ndjson_gzip BLOB   NOT NULL,          -- 规范化快照(gzip)
  line_hashes BLOB   NOT NULL,          -- 行指纹:每行内容哈希(sha256 前 8 字节)按行序拼接
  line_sizes  BLOB   NOT NULL           -- 行长度:每行内容字节数(uint32 大端)按行序拼接
)
CREATE INDEX idx_snapshots_session ON snapshots(session_id, seq);
```

- 二进制(图片/文档字节)不进快照:以 `{type:"blob", hash, media_type, bytes}` 引用;字节本体在原始层(base64 原文)可取到,无需独立 blob 存储
- gzip 用标准库 `compress/gzip`(零新依赖;与 zstd 的压缩率差异不值得引库)
- 请求行(method/path/query)与头部、body 并列落盘:path 是端点语义的唯一来源——`/v1/messages` 与 `/v1/messages/count_tokens` 的 body 形状相同,仅凭 body 无法区分
- 入向请求头随原始层持久化:canonical JSON(`map<头名, []string>`)存 TEXT,不做 gzip(单请求约 1-3KB);`Host` 从 `r.Host` 补入(Go 的 `r.Header` 不含 Host);显式 session 判定与头回溯均从落盘头部提取——头部不可再生,必须随 1 层落盘
- 凭据脱敏:头名(不区分大小写)匹配 `authorization` / `api-key` / `apikey` / `cookie` / `secret` / `token` 的头,值写入前替换为 `[redacted]`,头名与其它头原样保留;session/agent 标识头不受影响
- 判定理由随行:`session_note` 存判定依据的短理由(如 `explicit: x-claude-code-session-id`、`no identifier (anon by date)`、`degraded: invalid identifier (len 200)`);机器语义仍由 `session_source` 枚举承载,理由仅为人可读的排查线索
- 行指纹列:`line_hashes` 是定长记录(8 字节 × 行数,按行序拼接,大端字节序),不压缩——本身是随机字节且比对需要随机访问;不建独立表(按行建表的行开销大于指纹本体,而查询永远是整条序列)
- 哈希定义:对每行的 canonical JSON 字节做 sha256 取前 8 字节。64 位指纹在单会话 10⁵ 行量级的碰撞概率约 2.7×10⁻¹⁰,可忽略,且碰撞只影响分类徽标、不动数据
- **指纹剔除易变注解**(真实流量采样修正):`cache_control` 是客户端标记缓存断点的注解,位置随会话滚动;它原样保留在快照本体(保真),但不参与指纹与行长度。否则每一步的前缀都会断在标记那一行,分类全部退化为「分叉」。注意:注入**内容**的变化(如 `<total_tokens>… tokens left` 的计数)是真实变化,仍照常分类为分叉——只有注解被剔除
- 切分规则(忽略尾随空行、按 `\n` 切、去 `\r`)必须与序列化共用同一份实现:两侧不一致会让指纹序列错位
- 指纹与分类规则无关,是确定性事实:比对只读指纹,JSON 解析只发生在「本轮新增的行」;快照从原始层重放时指纹一并重算
- 成本:8 字节/行,相对该快照 gzip 后体积约 8%–15%(取决于行的平均长度与压缩率)
- 行长度列:`line_sizes` 与 `line_hashes` 同源同序,每行 4 字节(uint32 大端)。它让「差异行的长度对比」在不解压的前提下可算:步骨架据此判断末行**变长 / 变短 / 等长改写**,差异块给出两侧字节数。成本 4 字节/行,与指纹一起在序列化时算出

### D2 捕捉管线(与 C1 record 环节的衔接)

**扩展现有 record 环节的异步管线**(而非新增独立 finalizer):队列元素扩展为「请求记录参数 + 请求行(method/path/query)+ 原始 body + 全量入向头(构造队列元素时统一脱敏,后续管道只接触脱敏副本)」;写协程内按序完成:

```
记录事务(逐条 INSERT ... RETURNING id)→ 解析+判定 → 轨迹事务(request_bodies + snapshots)
```

- **队列容量与内存上界**:元素自携带原始 body 后,单元素量级从字节级升到 MB 级,队列深度由 C1 的 1024 下调为 **128**——驻留上界 ≈ 深度 × 单请求体(典型几十 MB,极端情形仍有界);容量以元素数计,字节预算留待真实观测到丢弃升高再引入。
- **request_id 获取**:记录事务内**逐条** `INSERT INTO requests ... RETURNING id`(sqlc `:one` 查询或沿用现有手写路径),id 与队列元素一一对应,消除「按 id 倒序回取」对写入时序的隐式依赖;存储层批量写入相应改为返回 id 列表。
- **事务与失败隔离**:记录批量事务提交后,逐请求独立事务写 `request_bodies` + `snapshots`;轨迹事务失败只回滚该请求的轨迹(记录已在、不受影响,对应「捕捉失败不影响请求记录」);body 不可解析时不产快照、仅落 `request_bodies`,日志记录
- **会话归属由快照承载的后果**:`snapshots.session_id` 是会话归属的唯一来源,因此**无快照的请求不进入任何会话**(未携带标识或标识不合规者归入按天匿名会话,见 D4)。这类请求(请求体超限/读取失败/不可解析)只能在原始层与请求记录中查阅,轨迹视图不做呈现——视图侧 **不得** 声称覆盖了全部请求

理由:单一异步通道(无跨队列排序与关联问题、复用 C1 的 drain/丢弃语义);request_id 关联在同一写协程内确定。备选(独立 capture 环节 + 各自关联)因交叉排序复杂度被否。

### D3 规范化器(协议 → 主干)

主干 = `{role, blocks[]}`,一行一条 canonical JSON(键序稳定、无缩进);**块保留原始 JSON 的全部键**,仅做三件事:类型名归一、跨协议字段补齐、二进制转引用。

类型映射:

| 来源 | 主干块类型 |
|---|---|
| Anthropic `text` / OpenAI content 字符串或 text parts | `text` |
| Anthropic `thinking`(含 `redacted_thinking`) | `thinking`(`signature` 保留) |
| DeepSeek 风格 `reasoning_content`(OpenAI 兼容入向) | `thinking`(原文保留) |
| Anthropic `tool_use` / OpenAI `tool_calls[]`(`arguments` 字符串解析失败则保原样) | `tool_use`(`id`/`name`/`input` 保真,供配对) |
| Anthropic `tool_result` / OpenAI `role=tool` | `tool_result`(`tool_use_id`/`content`/`is_error` 保真) |
| Anthropic `image`/`document`、OpenAI `image_url`(base64 或 data URL) | `blob`(hash/media_type/bytes) |
| 其他/未知块 | 原样保留(`type` 用原值) |

- **角色归一**:OpenAI 的 `role=tool` 归一为 `role=user`——工具结果在 Anthropic 侧位于 user 消息的 `tool_result` 块,主干按后者归一,跨协议骨架才可比;`system`/`user`/`assistant` 原样保留
- **入参归一**:OpenAI 的 `tool_calls[].function.arguments` 是 JSON 字符串,解析为对象后填入 `input`;解析失败保留原样字符串
- Anthropic `system` 与 OpenAI `system` 消息均作为首条伪消息进主干;`tools` 定义不进
- 解析在异步侧进行(请求路径只入队 body 字节)
- **生成范围**:仅会话型推理端点(如 `/v1/messages`、`/v1/chat/completions`)生成快照;辅助端点(如 `/v1/messages/count_tokens`)**不生成**(按请求行 path 过滤),原始层照存;被鉴权拒绝或被短路的请求只要 body 可解析即如实生成并进入轨迹(轮次状态由记录侧 `status_code`/`error_kind` 区分);body 不可解析则只落原始层

### D4 session 判定(仅显式标识)

版本取舍:主流客户端(Claude Code 的 `x-claude-code-session-id`、Codex 的 `session-id`/`thread-id`)都携带可靠的会话标识头,**链合兜底(按消息内容推断会话延续)暂不实现**——候选窗口、前缀/共享尾部匹配、内容哈希与内存索引的复杂度远高于收益;需要时再作为独立变更补上。

session 身份含客户端维度:`session_id` 存为 `<key_id>:<标识串>`——不同 key 不会互相归并。

判定流程(异步写协程内串行执行,无并发问题):

1. **显式标识**:按协议取头——Anthropic 方向 `x-claude-code-session-id`;OpenAI 方向 `session-id`/`session_id`/`thread-id`/`thread_id`(两种拼写都认);值即标识串 → `session_source='explicit'`,理由 `explicit: <头名>`。agent 层级头随原始层落盘,需要时从落盘头回溯
   - **标识规范化(安全边界)**:标识串先 trim,要求长度 ≤ 128 且仅含 `[A-Za-z0-9._-]`;不合规即视为无标识,降级事件写入理由(`degraded: invalid identifier (len <n>)`)。由此 `session_id` 只含白名单字符与单个 `:` 分隔符,可安全进入 URL 路径与前端渲染(客户端可控值不直通)
2. **无标识兜底**:未携带标识头(或标识不合规)的请求归入**按天分桶的匿名会话** `<key_id>:anon-<YYYYMMDD>`(UTC),理由记 `no identifier (anon by date)`——避免所有无头请求挤进同一会话,也避免它们污染显式会话
3. **一次性写入**:判定结果与理由随快照行写入,不提供在线重算。纠错方式为「按 `session_note` 定位 → 直接修改数据」;代价是没有审计与事务化的重算工具,收益是删掉判定重放逻辑与审计表(见 Risks)

### D5 轮次推断与 tool 抽取(查看时计算)

全部为查询侧纯计算:输入是同一 session 的快照指纹序列、按需解压的指定行与 requests 行;输出是增量分类、轮次分组与 tool 明细。不写任何派生数据。

**1. 增量分类(相邻 step 行级前缀比对)**

- 比对对象:`line_hashes` 指纹序列(每行 8 字节);`prefixLen` = 公共前缀指纹数。比对不触发 gzip 解压与 JSON 解析
- 解析边界:比对只读指纹,JSON 解析只发生在「本轮新增的行」——`tool_result` 必在新增行,`tool_use` 在上一轮的新增行,因此 tool 配对与轮次边界判定都无需解析历史
- **轮层级聚合分两档**:压缩次数、重试次数、step 数、时长与 token 汇总全部由指纹与 `requests` 得出(零解析,随骨架返回);tool 调用数需解析该轮各 step 的新增行,走增强端点渐进填充(见 D6),不得阻塞轮次列表呈现
- 分类规则:

| 条件 | 分类 |
|---|---|
| `prefixLen == len(prev)` 且 `len(cur) == len(prev)` | 重复(内容与上一步完全相同) |
| `prefixLen + 1 ≥ len(prev)` 且 `len(cur) > len(prev)` | 追加(**含末行刷新**:客户端每轮更新末尾注入内容,如剩余 token 计数,形式上表现为「末行被替换 + 尾部追加」) |
| `prefixLen == len(prev)` 且 `len(cur) < len(prev)` | 分叉(尾部删除) |
| `0 < prefixLen < len(prev)` | 分叉(尾部替换,记录首个分歧位置) |
| `prefixLen == 0` 且存在共享尾部(`cur` 末尾连续 ≥2 个指纹在 `prev` 中出现) | 改写(历史被替换) |
| `prefixLen == 0` 且无共享尾部 | 无重合 |

- **三段区间**:每次比较同时给出确定的行号区间——公共前缀与共享尾部为「保留」、旧序列中其余行为「移除」、新序列中其余行为「新增」。区间只用指纹算出,不解析 JSON;文本在展开时按区间懒加载
- **形态细分**:两种形态各自标记——**摘要压缩**(改写形态且 `len(cur) < len(prev) × 0.6` 且有共享尾部)与**上下文清零**(无重合且 `len(cur) ≤ 3`,`/clear` 形态),均由指纹与行数判定、零解析;阈值与注入排除表一同由真实流量采样校准(tasks 5.1)。**不设「内容压缩」细分**:曾按"行数相近且替换行长度骤降"识别,但该判据只能证明"这个位置的内容变短了"——实测一次误报是长通知被另一条短通知占据(17441B → 305B),与"历史被压缩"无关,故改为只呈现**差异块**,由查看者判断
- **差异块用 LCS 求,不用逐位置对齐**(实测:104 步的会话里 74 处分叉中 28 处的消息条数发生变化)。逐位置对齐的问题:一条消息被插入或删除后,其后所有行的位置都偏移,对齐会报出一大片无关的"不同";LCS 只报真正变动的部分,删除与新增也各自成块。规模上限 400 万格(n×m),超出时退回单块替换。**必须与同链上一步比较**:会话内相邻的两步可能分属不同链(主路步的前一步常是旁路请求),拿它当比较对象会得出无意义的差异
- **压缩的展示契约**:界面必须给出前后行数与三段计数(移除/保留/新增),并支持展开被移除区间的内容(需解压上一条快照,属懒加载路径)
- **末行更新**:分叉形态的细分——只剩末行不同、行数不变,方向不限。变长、变短、等长改写都算。客户端刷新末尾注入内容(`<total_tokens>` 计数)、旁路请求重写自己的转录行都落在这个形态上。指纹是整行哈希,分辨不出「更新」与「改写」,方向靠 `line_sizes` 给出。界面据此显示「末行扩展 +NB」「末行收缩 −NB」「末行改写」,不显示笼统的「分叉」
- **末行更新的判据必须双向**:只认变长会漏掉变短。真实流量里客户端会重建转录,把先前的行写短,这类变化属于末行更新。按「分叉」呈现会读成主路出了问题
- **行长度缺失时不报方向**:`line_sizes` 缺列的旧数据下,`LineSize` 返回 -1。此时只陈述「末行被更新」,不印出哨兵值,也不把不可比说成「字节数不变」
- **旁路步不计入轮层级的形态计数**:轮级的压缩数、重试数、失败数与轮次起始时间只取主路步。`side_count` 单独给出旁路步数,列表仍按 tab 分列。旁路请求自带高频末行更新,计入会让轮级概览被噪声淹没。步数与 token 仍含旁路,那是本轮的真实开销
- **重复计数**:连续相同内容的 step 构成一组,组内次序 = 组内序号(展示为「第 n/N 次相同」)。判据是**内容完全相同**——它是中性事实,不声称"失败后重发"(真实流量里同样存在一次成功请求后 1.1 秒又发相同内容的形态)。tool 失败(`is_error`)与 agent 内部重新生成不属于此类
- 复杂度 O(行数);单会话几百轮、快照千级行,毫秒量级
- 规则为启发式:误判(如连续两步都改写了同一处中间消息)会出现,分类仅作徽标呈现,不阻断查看

**2. tool 抽取与配对**

- 每 step 抽出两类块:`tool_use`(`id`/`name`/`input`)与 `tool_result`(`tool_use_id`/`content`/`is_error`)
- 配对:从发起 step 起向后查找最早匹配 `tool_use_id` 的 `tool_result`;找不到则状态为「结果未见」
- 状态:结果存在且 `is_error` 为真 → 失败;结果存在且非 error → 成功;无结果 → 未见
- 展示层截断长入参与长结果(存储保持完整)

**3. 链归属与轮次(Turn)分组**

- 遍历 session 内 step 序列,首个 step 开启一轮
- **先归属链,再在链内分轮**:一个 session 里可能交错着多条消息链——客户端的旁路请求(Claude Code 每轮发一次安全监控:独立 system prompt + 对话转录)带同一个会话标识,与主链完全不相交
- **归属判据是两级的首条消息指纹**:① **system 指纹**定「请求类型」——主对话、安全监控、子 agent、生成标题各有自己的系统提示,剔除易变注解(`cache_control`、`<total_tokens>` 计数行)后同类型稳定、跨类型互不相同;② **首条 user 文本前缀的指纹**定「同类型内的实例」——3 个并行子 agent 共用同一套系统提示,靠各自的任务描述区分(取前缀而非全文,因为监控请求的首条 user 含随对话增长的转录)。两级都相同才归入同一条链,任一级缺失时跳过该级。**不解析客户端特有的标记,也不跟踪工具调用关系**,任何协议都适用
- **主链 = 含会话首步的链**,其余为旁路:会话的第一条请求必然是用户对话。不用步数或长度判据——两者均被真实流量证伪(27 步的监控链超过 15 步的对话链;45 行的子 agent 链超过 42 行的主对话链,后者从主对话分叉、上下文同样长)
- **归属前按 `ts` 排序**:`seq` 是写入序,并发请求(多个 background subagent)下与发起序不一致(实测:同一会话里 seq=23 的时间戳比 seq=22 早 9 秒)
- 实测效果:104 步的会话分出 7 条链——主对话 17 步、3 个并行子 agent 各 15/17/17 步、三类监控与标题生成各成一条;3 个子 agent 共用 system 指纹,仅凭一级判据会合成一条 49 步的链
- 轮次边界在其**所属链内**判定;旁路链的 step 不参与边界判定,归入其**之后**最近的主路步所在轮(监控请求与被检查的操作常在同一秒发出——实测:用户第二问与一次监控同为 09:05:50,按"前一个主路步"归属会全部错放到上一轮),其后没有主路步时并入最后一轮;界面在轮次内以切换控件区分主路与旁路;旁路不计入主路的轮次统计;重复组按链统计、不跨链
- 边界 = 该 step 分类为「追加」**或「分叉」**,且新增尾部含**非 `tool_result` 的 user 消息**,且该消息不命中**注入排除表**
- **分叉也要看尾部**(真实流量采样修正):客户端每轮都会刷新注入内容(Claude Code 的 `<total_tokens>… tokens left</total_tokens>`),前缀在那一行断掉,于是"纯追加"退化为"分叉";若只认追加,整场会话只会开出一个轮次
- 注入排除表:按消息文本前缀匹配(`<system-reminder>`、`<command-name>`、`<command-message>`、`<command-args>`、`<local-command-stdout>`、`<local-command-stderr>`、`Caveat:`、`The user stepped away and is coming back.`、`Another Claude session sent a message:`),并在列表之外补一条通用判据——以 `<tag>` 形式开头者一律视为注入。硬编码列表会随客户端版本腐化(实测 `<command-message>` 曾漏网开出假轮次),**以真实流量采样持续校准**(见 tasks 5.1)
- **子 agent 结果的归属**:子 agent 完成时,harness 把结果作为 user 消息注入主对话(实测形态 `Another Claude session sent a message:` + `<agent-message from="…">`)。它**不是人的输入**,而是**发起该子 agent 那一轮的延续**——同一诉求从"派出"到"汇总"是一个轮次,故其必须命中排除表、不得开启新轮次
- 分组在界面标注为推断;推断失败只影响分组可读性,不丢数据

**4. 近似口径**

- tool 阶段耗时 = 下一 step 的 `requests.ts` − (本 step 的 `requests.ts` + 本 step 的 `latency_ms`)
- 该值含客户端本地执行时间与人工思考时间,**必须**标为近似,不得表述为工具执行耗时

**5. 缓存与性能**

- 会话级推断结果进程内 TTL 缓存(60 秒),键 = `session_id` + 该会话最后一轮的 `request_id`(有新增即失效)
- 单会话成本:解压 + 行比对 + 块抽取,个人量级毫秒到十毫秒;无新增轮次时命中缓存

**6. 规则注释惯例**

判定与分类的每条启发式(注入排除表、共享尾部阈值)在实现处以真实触发案例注释(输入特征 + 处理结果),规则迭代保留历史案例,便于误报考古。

### D6 控制台 API 与前端

只读 REST(复用会话鉴权与 CSRF 形态):

| 端点 | 内容 |
|---|---|
| `GET /console/api/traces?q=&from=&to=&key_id=` | 会话列表(过滤:标识前缀 / 时间范围 / client key):`[{session_id, session_source, session_note, first_seen, last_seen, steps, usage_in/out/cache}]`。`steps` 即该会话的请求数(会话归属由快照承载,二者恒等,不重复计列)。**不含轮次数**:推断轮次要对每个会话做比对与首行解析,成本随会话数线性放大,轮次数由详情页给出 |
| `GET /console/api/traces/{session_id}` | 骨架(指纹级,**不解析历史消息**):`{session_id, session_source, session_note, first_seen, last_seen, steps, usage_in/out/cache, turns:[{index, started_at, parallel, compresses, retries, failures, steps:[{seq, ts, model, status_code, error_kind?, latency_ms, stream, usage_in/out/cache, delta:{kind, added, removed, kept, first_diff, note}, compress:{before_lines, after_lines, from_seq}?, content_rows?, cleared?, retry:{n, of}}]}]}` |
| `GET /console/api/traces/{session_id}/digest` | 渐进填充(需解析各步新增行,由会话级缓存吸收):轮次标题与工具聚合 `{by_turn:[{turn, title, calls, by_name}], totals:{calls,ok,err,unseen}, tools:[{name, source, calls, ok, err, unseen, gaps_ms, turns}]}`;`title` = 该轮首个 step 的新增行中第一条 user 消息的文本片段,取不到时为空。前端在骨架渲染后请求,填进轮次行与工具视图 |
| `GET /console/api/traces/{session_id}/{seq}` | 单 step 详情(懒加载):`{seq, ts, delta:{kind, note}, inherited, messages:[消息], tools:[{name, status, input, result, issued_seq, result_seq}], compress?:{before, after, kept, removed, added, ...}, content_rows?:[{line, before, after, ...}]}`;messages 默认只返回本轮新增,`?full=1` 返回全量 |

**解析边界的实际口径**:比对与分类只读 `line_hashes`/`line_sizes`;轮次边界判定需要"该步新增了什么",因此骨架会**解压**快照并只**解析新增区间** `[prefixLen, curLines)`。gzip 不支持随机访问,解压无法避免;省掉的是历史行的 JSON 解析——这是"解析只发生在本轮新增行"的真实含义。全量解析只出现在两处:mm 压缩对照与 `?full=1`。

`{session_id}` 路径参数由 D4 的字符白名单保证 URL 安全(不含 `/`);前端按文本转义渲染,不信任该值。

前端三个 tab:**会话**(跨会话入口,可搜索;不含轮次数)→ **轨迹**(轮次 → step 骨架 → 展开懒加载 tool 明细与 messages)与**工具**(会话级聚合 + 名称/状态/来源筛选)。

**形态基准**:视图的信息密度、层级展开方式与视觉语言(实线=实测值、虚线紫框=推断、虚线灰框/虚线下划线=需解析可渐进填充)以静态原型 `docs/trail-view.html` 为准;该页覆盖九种形态:普通追加、重复、摘要压缩、差异块(改动 / 删除 / 新增)、末行更新(turn 9,徽标给出方向与字节数)、被拒请求(带网关侧归因)、上下文清零、无重合、多工具单步与结果未见。

### D7 与 C1 的兼容与失败隔离

- `requests` 表与现有 API 不变;两张新表在每次启动迁移时创建(0003)
- 捕捉任一步失败(gzip/解析/判定/写库)只落日志:该请求失去轨迹但不影响请求、记录与后续轮次;轨迹按「单请求事务」写入(bodies+snapshots 同事务,独立于记录事务),避免半条轨迹
- 丢弃语义沿用 record 环节的有界队列(轨迹与记录同生共死:整体丢弃并计数)

## Risks / Trade-offs

- [推断规则是启发式] → 分类与轮次分组只在展示层、不落库;误判靠规则注释与真实案例迭代修复,不需要重放数据
- [判定一次写入、无在线重算] → 纠错需直接改数据。这是用「删掉重算与审计」换复杂度削减的取舍:显式标识下基本不会误判,`session_note` 足以定位误判来源
- [大上下文解析成本] → 全在异步侧;单请求 ms 级(gzip+解析),队列背压与 C1 同款;查询侧按会话 TTL 缓存
- [队列内存放大(元素携带 body)] → 深度下调至 128,驻留上界 ≈ 深度 × 单请求体,有界;若观测到丢弃升高再引入字节预算
- [存储增长] → 已接受「全留不删」;单快照 gzip ≈30KB 量级,个人使用年增约 2GB
- [已知推断盲区] → 子 agent 平行链、`/clear` 复用 session id、压缩后的 messages 形态三处当前均为推断,须先用真实流量采样确认(tasks 5.1)再定稿排除表与阈值

## Migration Plan

- 启动时自动应用 0003(幂等);既有数据无需回填(历史请求缺轨迹,属预期)
- 回滚 = 停进程、删除数据文件(无外部状态);或仅删两张新表(不伤既有记录)

## Open Questions

- 客户端注入内容的形态(system-reminder 等)待真实流量采样确认,注入排除表据此定稿
- 子 agent 是否共享 session id 待实测;若共享,其 step 会落入「无重合」分类——本变更只标记不分组,链归属(把交错出现的多条链正确归组)留待后续
