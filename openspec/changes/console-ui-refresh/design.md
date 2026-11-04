# Design

## Context

见 `proposal.md` 的 Why。当前实现的约束:

- 前端为 Vue 3 单页应用，无路由、无组件库。全局样式集中在 `web/src/App.vue` 的 `<style>` 内，各视图另有 scoped 样式。
- 控制台产物由 `web/` 打包后经 `go:embed` 打进二进制，资源体积直接影响二进制大小。
- 前端没有测试运行器。可用的机械检查是 `pnpm typecheck`、`pnpm build`、`make build` 与 `make smoke`。
- `make smoke` 只比对 `docs/trail-view.html` 原型与 `web/src/trailLegend.ts` 的图例文案，不覆盖外壳结构，本次改动不影响它。

## Goals / Non-Goals

**Goals:**

- 样式层令牌化：颜色、间距、字号、圆角、焦点环各有变量，组件不写裸色值。
- 外壳支持折叠与窄屏，主题可切换且不闪。
- 配置编辑走弹窗，列表页只留列表。
- 键盘与读屏可用性达到 specs 中的基线。

**Non-Goals:**

- 不引入路由。当前 tab 切换与视图内状态保持原样。
- 不引入组件库或 CSS 框架。
- 不加载网络字体，不改变二进制资源结构。
- 不改轨迹视图的信息架构与图例文案。
- 不做长列表虚拟化。它需要单独的性能变更。

## Decisions

### 1. 令牌集中在单个 CSS 文件

新增 `web/src/styles/tokens.css`,由 `main.ts` 首个导入。结构:

- `:root` 定义暗色值，同时声明 `color-scheme: dark`。
- `html[data-theme='light']` 覆盖为亮色，并声明 `color-scheme: light`。
- 令牌分四组：`--bg` / `--surface` / `--surface-2` / `--line` / `--text` / `--muted` / `--accent` / `--ok` / `--warn` / `--danger` / `--infer`。间距为 `--sp-1..6`。字号为 `--fs-12..28`。圆角为 `--r-sm/md`。焦点环为 `--focus`。

理由：变量在运行时切换，不需要换样式表，也不会重载页面。备选是两套 CSS 按主题加载，代价是切换要重载、会闪。

### 2. 首屏主题在 HTML 内联设好

`web/index.html` 的 `<head>` 内联一段脚本，在样式表之前按 `localStorage.dsctx.theme` 写入 `html[data-theme]`,读不到时用 `matchMedia('(prefers-color-scheme: light)')` 回落。同时写入 `<meta name="theme-color">`。

理由：等 Vue 挂载后再设主题会闪一帧。内联脚本体积不到 300 字节。

### 3. 主题与外壳状态各一个模块

新增 `web/src/theme.ts` 与沿用 `localStorage` 键 `dsctx.theme`、`dsctx.sidebar`。`theme.ts` 导出 `current()`、`set(theme)`、`toggle()`,并负责同步 `data-theme`、`color-scheme` 与 `theme-color`。组件只调用函数，不直接读写键。

理由：键名与副作用集中一处，避免各组件各写一份。

### 4. 外壳拆成 AppShell

新增 `web/src/components/AppShell.vue`,承担侧栏、折叠按钮与内容区。`App.vue` 只保留鉴权判断、tab 状态与视图映射。

- 折叠按钮是原生 `<button>`,带 `aria-expanded` 与 `aria-controls`,折叠态用 `title` 与 `aria-label` 保留文字线索。
- 侧栏 `<nav aria-label="控制台导航">`,当前项带 `aria-current="page"`。
- 折叠态宽度 56px,展开 232px,用变量 `--rail` 控制，内容区用 `flex: 1` 自动加宽。

### 5. 弹窗用原生 dialog

新增 `web/src/components/AppDialog.vue`,内部 `<dialog>` + `showModal()`。它提供标题、默认 slot 与底部操作 slot,并在 `close` 事件里把焦点还给打开前的元素。

理由：原生 `<dialog>` 自带 `Esc` 关闭、背景 `inert` 与焦点限制，自建遮罩要手写这三件事。备选是自建遮罩，风险与代码量都更高。

表单状态与提交逻辑留在 `Providers.vue`、`Models.vue`、`Keys.vue`。弹窗只管外壳，三个配置区的创建入口由此保持一致。这样错误显示、字段检查与请求代码不动，改动面更小。

### 6. 响应式按断点 720px

- 主区 `padding: clamp(16px, 3vw, 32px)`,不设 `max-width`。
- 表格外层 `.table-scroll { overflow-x: auto }`,表格 `min-width` 保证列不挤压。
- 720px 以下隐藏侧栏文字、表格保持横向滚动，页面本身不横滚。
- 不做表格转卡片。CSS 把表格改成块级元素会让读屏丢失表格语义，收益不抵风险。

### 7. 时间格式化改走 Intl

`web/src/format.ts` 的 `formatDateTime` / `formatFullTime` 改为 `Intl.DateTimeFormat('sv-SE', {...})`。`sv-SE` 输出 `YYYY-MM-DD HH:mm`,与现状一致，且不依赖手写补零。紧凑计数(如 `1.2k`)与带单位的紧凑数值(如 `45ms`、`1.2KB`)保留现有写法。单位是界面约定，不随 locale 变化。

### 8. 可点击容器改原生按钮

轨迹视图的三处可点容器(`turn-head`、`step-head`、`msg-head`)改为 `<button type="button">`,外层保留布局样式，按钮内保留原有 span 结构，补 `aria-expanded`。会话列表的整行点击改为行内「查看」按钮，行点击保留为附加手段。

理由：键盘可达与读屏可识别无法靠 `tabindex` 与 `role` 可靠补齐，换标签最省事，也顺带解决焦点样式。

## Risks / Trade-offs

- **裸色值面大，一次替换可能漏改** → 分两批：外壳与通用控件先改并目检，轨迹视图后改。改完用 `grep` 检查 `#` 字面色值是否只剩令牌文件。
- **`<dialog>` 在旧浏览器缺失 `showModal`** → 目标为本机现代浏览器，不写降级分支。半套降级会丢失 `Esc`、遮罩与焦点限制，比不支持更危险。
- **暗色成为默认会改变现有使用习惯** → 保留亮色一键切换，并在切换按钮上给出去向文案。
- **内联主题脚本可能触发 CSP 拦截** → 控制台无 CSP 头，风险为零。若将来加 CSP,需要 nonce。
- **表格窄屏横滚后操作列不易发现** → 操作列固定在末列并保持 `min-width`。不做 sticky,避免与横向滚动容器打架。

## Migration Plan

无数据迁移。改浏览器本地键(`dsctx.theme`、`dsctx.sidebar`)可随时丢弃。回滚方式为回退前端提交，后端与数据库不受影响。

## Open Questions

- 是否要在后续变更里做长会话列表虚拟化。当前不改，单独提案。
