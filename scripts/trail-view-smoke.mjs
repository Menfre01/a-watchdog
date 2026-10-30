// 轨迹视图原型冒烟:抽出 docs/trail-view.html 的内联脚本,在最小 DOM 桩下执行,
// 断言新形态真的渲染出来,并比对图例文案与 web/src/trailLegend.ts 一致 ——
// 形态基准与实现各写一份文案必然漂移,这里用机器比对把它们钉在一起。
//
// 用法:node scripts/trail-view-smoke.mjs(退出码 0 表示全部通过)
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const html = readFileSync(resolve(root, 'docs/trail-view.html'), 'utf8')
const legendSource = readFileSync(resolve(root, 'web/src/trailLegend.ts'), 'utf8')

// runPrototype 在 DOM 桩下执行原型脚本,返回渲染出的 HTML。
function runPrototype() {
  const start = html.indexOf('<script>') + '<script>'.length
  const end = html.indexOf('</script>', start)
  const script = html.slice(start, end)

  const captured = []
  const stub = {
    set innerHTML(value) {
      captured.push(value)
    },
    get innerHTML() {
      return captured[captured.length - 1] ?? ''
    },
    hidden: false,
    dataset: {},
    classList: { toggle() {}, add() {}, remove() {}, contains: () => false },
    addEventListener() {},
    querySelectorAll: () => [],
    querySelector: () => null,
    parentElement: null,
    appendChild() {},
  }
  globalThis.document = {
    getElementById: () => stub,
    querySelectorAll: () => [],
    querySelector: () => null,
    addEventListener() {},
    createElement: () => stub,
    body: stub,
  }
  globalThis.window = { addEventListener() {}, location: { hash: '' } }
  globalThis.location = { hash: '', href: '' }

  new Function(script)()
  return captured.join('\n')
}

const out = runPrototype()
const countOf = (needle) => out.split(needle).length - 1

// legendTexts 抽出 trailLegend.ts 里的图例文案,用于与原型逐条比对。
const legendTexts = [...legendSource.matchAll(/\{ text: '([^']+)'/g)].map((m) => m[1])

// 口径文案(export const X_NOTE = '...'):每一条都必须出现在原型的渲染结果里。
// 口径文案散在两处正是漂移的起点,所以这里按名字取出来逐条比对。
const noteTexts = [...legendSource.matchAll(/export const ([A-Z_]+_NOTE) =\s*'([^']+)'/g)].map((m) => ({
  name: m[1],
  text: m[2],
}))
const overlapNote = noteTexts.find((note) => note.name === 'NO_OVERLAP_NOTE')

const checks = [
  ['分类分布条渲染出分段', countOf('class="seg k') >= 5],
  ['分类分布不含 0 步的段', !out.includes('class="seg k5"')],
  ['图例文案与 trailLegend.ts 一致', legendTexts.length >= 10 && legendTexts.every((text) => out.includes(text))],
  ['叙述渲染出可点片段', out.includes('class="part link"') && out.includes('class="arrow"')],
  ['失败清单列出状态码与原因', out.includes('class="failures"') && out.includes('状态码 429')],
  ['时间轴给出轴头与端口标注', out.includes('class="head axis-row"') && out.includes('轴覆盖本轮全部请求')],
  ['每步在轴上有一根条', countOf('class="fill') === 14],
  ['极值只标最慢与最多 token', out.includes('>最慢<') && out.includes('>最多 token<')],
  ['末行更新徽标带方向与字节数', out.includes('末行扩展 +26B')],
  ['加载入口写出解析代价', /工具 · 解析 \d+ 行/.test(out)],
  ['链汇总给出主链与旁路链数', out.includes('>1 + 1<')],
  ['旁路占比给出步数与 token', /旁路<\/div>\s*<div class="v">1 步 · in 1\.8k \/ out 0<\/div>/.test(out)],
  ['链数标明为推断并带判据', out.includes('同一条链的判据是 system 指纹与首条 user 指纹两级相同')],
  ['退化形态给出「—」与原因', out.includes('缺 system 指纹时该处显示:链 —(缺 system 指纹,链不可分)')],
  ['轮级链口径写明本轮', out.includes('本轮链 1 条 · 旁路 1 步')],
  ['harness 段以斜纹标出', /class="hatch" style="left:/.test(out)],
  ['harness 数值给出或显示「—」', /harness \d/.test(out) && out.includes('harness —')],
  ['harness 口径写明三部分', out.includes('窗口含工具在客户端的执行、客户端自身的处理、人的思考')],
  ['harness 口径写明窗口范围', out.includes('也含同期为其它链所做的工作与不生成 step 的其它请求')],
  ['轴注写明请求体读取不计入', out.includes('本步耗时从请求体读完开始计时,故不含请求体的读取时间')],
  ['后继跨轮时裁掉并说明', out.includes('本段止于轴末端,该值延续到下一轮')],
  ['起始时间与轴口径写明差别', out.includes('轴左端取本轮全部请求里最早的一次')],
  ['轮次 chip 只数主路步', out.includes('>3 steps<')],
  ['会话头给出协议', out.includes('>anthropic-messages<')],
  ['用量口径写明两套加法', out.includes('in + cache 等于总输入') && out.includes('in + cache 会重复计数')],
  ['混合协议给出不可相加说明', out.includes('跨协议的用量不可直接相加')],
  ['全部口径文案同步', noteTexts.length >= 8 && noteTexts.every((note) => out.includes(note.text))],
  ['无重合口径出现在分布条与图例两处', !!overlapNote && out.split(overlapNote.text).length - 1 >= 2],
  ['分类用词为无重合', out.includes('无重合') && out.includes('⇄ 不相交')],
  ['原型不再出现旧词', !out.includes('新链')],
]

// 产物检查:形态基准与图例文案改了,而构建产物没跟上时,界面看到的还是旧文案。
const assetsDir = resolve(root, 'pkg/admin/dist/assets')
try {
  const bundle = readdirSync(assetsDir)
    .filter((name) => name.endsWith('.js'))
    .map((name) => readFileSync(resolve(assetsDir, name), 'utf8'))
    .join('\n')
  checks.push([
    '构建产物含图例与轴口径文案',
    legendTexts.every((text) => bundle.includes(text)) &&
      noteTexts.every((note) => bundle.includes(note.text)) &&
      bundle.includes('轴覆盖本轮全部请求'),
  ])
} catch {
  console.log('SKIP 构建产物检查:未找到 pkg/admin/dist/assets(先跑 make build)')
}

let failed = 0
for (const [name, ok] of checks) {
  if (!ok) failed++
  console.log(`${ok ? 'PASS' : 'FAIL'} ${name}`)
}
if (failed) console.log(`\n${failed} 项未通过`)
process.exit(failed === 0 ? 0 : 1)
