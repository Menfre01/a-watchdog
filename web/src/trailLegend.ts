// 轨迹视图的图例与词表:文案的单一来源。
// 原型 docs/trail-view.html 与冒烟脚本复用同一批字符串,避免两处各写一份而漂移。
export interface LegendEntry {
  text: string
  // from 标明该条目的来源:实测、推断,或需解析(渐进填充)。
  from: 'measured' | 'inferred' | 'lazy'
  // note 是补充口径:与分布条等处共用的同一句话,避免两处各写一份。
  note?: string
}

// LINE_LEGEND 是线型图例:实线 / 虚线 / 虚线灰框的语义。
export const LINE_LEGEND: LegendEntry[] = [
  { text: '实线:实测值,直接来自记录侧字段', from: 'measured' },
  { text: '虚线:推断值,由行指纹比对得出', from: 'inferred' },
  { text: '虚线灰框与虚线下划线:需解析,可渐进填充', from: 'lazy' },
  { text: '斜纹:harness 侧时间,由同链相邻两步的时间戳推得', from: 'inferred' },
]

// NO_OVERLAP_NOTE 把「无重合」与链数分开:它由分布条与图例共用,故只写一次。
export const NO_OVERLAP_NOTE = '该分类统计链内内容断层的步,不是链数。链数见会话头的链汇总。'

// KIND_LEGEND 是分类徽标的含义。
export const KIND_LEGEND: LegendEntry[] = [
  { text: '首步:会话第一条快照,无前序可比', from: 'inferred' },
  { text: '追加:本次序列是上次的前缀延伸', from: 'inferred' },
  { text: '重复:与上一步内容完全相同', from: 'inferred' },
  { text: '分叉:前缀相同,尾部被替换或删除', from: 'inferred' },
  { text: '改写:无共同前缀,但有共享尾部', from: 'inferred' },
  {
    text: '无重合:与同链上一步既无共同前缀,也无共享尾部',
    from: 'inferred',
    note: NO_OVERLAP_NOTE,
  },
]

// MARK_LEGEND 是细分标记与记录侧标记的含义。
export const MARK_LEGEND: LegendEntry[] = [
  { text: '末行更新:行数不变,只剩末行不同,方向由末行两侧字节数给出', from: 'inferred' },
  { text: '摘要压缩:改写且行数骤降,旧序列末尾被保留', from: 'inferred' },
  { text: '上下文清零:无重合且行数不超过 3', from: 'inferred' },
  { text: '网关侧归因:被拒或短路的请求带记录侧错误分类', from: 'measured' },
]

// KIND_TEXT 是分类的中文名:徽标、叙述与分布条共用同一个词。
export const KIND_TEXT: Record<string, string> = {
  new: '首步',
  added: '追加',
  retry: '重复',
  fork: '分叉',
  rewrite: '改写',
  chain: '无重合',
}

// USAGE_LEGEND 是用量列的口径:in 的含义随协议不同,不写明会被用错加法。
export const USAGE_LEGEND: LegendEntry[] = [
  { text: 'Anthropic:in 是 input_tokens,不含缓存。cache 是缓存读加缓存写。in + cache 等于总输入', from: 'measured' },
  {
    text: 'OpenAI 兼容与 DeepSeek:in 是 prompt_tokens,已含缓存命中。cache 是它的子集,in + cache 会重复计数',
    from: 'measured',
  },
]

// MIXED_PROTOCOL_NOTE 说明混合协议会话的读法:各步按自己的口径记录,跨协议不可相加。
export const MIXED_PROTOCOL_NOTE =
  '本会话出现多个协议:各步按各自的协议记录用量,跨协议的用量不可直接相加。'

// USAGE_NOTE 挂在协议标记上,说明该协议下 in 的含义。
export const USAGE_NOTE =
  'in 的含义随协议不同:Anthropic 不含缓存(与 cache 可相加);OpenAI 兼容与 DeepSeek 已含缓存命中(cache 是子集,不可相加)。'

// KINDS_NEW_NOTE 给出「首步 = 链数」这条不变量(含退化例外)。
export const KINDS_NEW_NOTE =
  '「首步」段等于链总数,即主链 1 条加旁路链 N 条,可与会话头的链汇总互相印证。链首快照为空时会记成「无重合」,该情形不在此列。'

// JUDGE_NOTE 说明两种判据的分工:链归属与内容分类用的不是同一种证据。
export const JUDGE_NOTE =
  '链归属用稳定指纹(system 前缀加首条 user 前缀),内容分类用整行字节。两者判据不同,所以「同一条链」与「零重合」可以同时成立。'

// 时间轴的显示口径:空隙的含义与轴的覆盖范围必须写在界面上。
export const AXIS_NOTE =
  '轴覆盖本轮全部请求,含旁路请求。斜纹段是 harness 侧时间:同链下一步请求的开始 − 本步结束,含工具执行、客户端处理与人的思考。本步耗时从请求体读完开始计时,故不含请求体的读取时间。'

// HARNESS_NOTE 是 harness 侧耗时的口径:它是推算值,且三部分不可拆分。
export const HARNESS_NOTE =
  'harness 侧时间(推算):同链下一步请求的开始时间 − 本步结束时间。窗口含工具在客户端的执行、客户端自身的处理、人的思考,也含同期为其它链所做的工作与不生成 step 的其它请求,故它不是该步独占的工作量,也不称「工具耗时」。'

// START_NOTE 说明两个时间口径的差别:轮次起始时间只取主路,轴左端含旁路。
// 旁路请求先到时轴会更早,两者都对,不写明会被读成数据打架。
export const START_NOTE =
  '轮次起始时间取首个主路步的时间。轴左端取本轮全部请求里最早的一次:旁路先到时轴会更早。两者口径不同,数值都对。'

// CHAIN_NOTE 是链汇总的判据:链归属是推断,读者必须能看到凭什么这么分。
export const CHAIN_NOTE =
  '推断:同一条链的判据是 system 指纹与首条 user 指纹两级相同。主链是含会话首步的那条,恒为 1 条。'

// TURN_CHAIN_NOTE 区分两级口径:轮级说的是「本轮出现的链」,会话级说的是整会话链数。
export const TURN_CHAIN_NOTE =
  '口径:本轮出现的链。会话层的「链」是整个会话的链数,两者不同。旁路为客户端自动请求(标题生成 / 安全监控等),与主路分列呈现。'
