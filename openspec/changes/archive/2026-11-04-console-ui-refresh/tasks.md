# Tasks

## 1. 样式底座

- [x] 1.1 新增 `web/src/styles/tokens.css`:定义颜色、间距、字号、圆角、焦点环变量，暗色在 `:root`,亮色在 `html[data-theme='light']`,两处都声明 `color-scheme`。该文件在 `main.ts` 中首个导入。验证：`pnpm typecheck` 通过，页面仍能正常渲染。
- [x] 1.2 在 `web/index.html` 内联主题引导脚本并补 `<meta name="theme-color">`。验证：系统为暗色时首屏直接是暗色，无亮色闪帧。
- [x] 1.3 新增 `web/src/theme.ts`,导出 `current`、`set`、`toggle`,统一读写 `awatchdog.theme` 并同步 `data-theme`、`color-scheme`、`theme-color`。验证：切换后刷新页面，主题保持。

## 2. 外壳与响应式

- [x] 2.1 新增 `web/src/components/AppShell.vue`:侧栏、折叠按钮、内容区 slot。折叠按钮带 `aria-expanded` 与 `aria-controls`,折叠态保留图标与 `aria-label`。验证：用 `Tab` 可聚焦折叠按钮并切换状态。
- [x] 2.2 `App.vue` 改用 `AppShell`,保留鉴权与 tab 逻辑，导航项补 `aria-current="page"`,`<nav>` 补 `aria-label`。验证：`pnpm typecheck` 通过，五个 tab 均可切换。
- [x] 2.3 折叠状态写入 `awatchdog.sidebar`,刷新后保持。内容区 `padding` 改 `clamp(16px, 3vw, 32px)`,去掉固定 `max-width`。验证：折叠后刷新仍为折叠态，大屏内容区随视口加宽。
- [x] 2.4 表格外包 `.table-scroll` 并设 `min-width`。720px 以下页面不出现整页横向滚动。验证：375px 视口下打开 providers 页，页面无整页横滚，表格可横滚。

## 3. 弹窗式编辑

- [x] 3.1 新增 `web/src/components/AppDialog.vue`,基于原生 `<dialog>` 与 `showModal()`,支持标题、内容、底部操作、`close` 回焦。验证：打开后焦点在首个输入框，`Esc` 关闭，焦点回到触发按钮。
- [x] 3.2 `Providers.vue` 的新建与编辑移入弹窗，列表页只留列表与「新建」按钮。保存失败时弹窗不关、错误显示在弹窗内。验证：提交重名 provider 时弹窗保持打开且已填内容不丢。
- [x] 3.3 `Models.vue` 同上。验证：新建与编辑均只在弹窗内出现，保存成功后弹窗关闭且列表刷新。
- [x] 3.4 删除确认由 `window.confirm` 改为 `AppDialog` 承载。验证：删除按钮打开确认弹窗，取消不发送请求。

## 4. 可访问性与数值显示

- [x] 4.1 轨迹视图的 `turn-head`、`step-head`、`msg-head` 改为 `<button type="button">` 并补 `aria-expanded`。外层布局样式不变。验证：仅用键盘可展开轮次与步，读屏可读出展开状态。
- [x] 4.2 会话列表整行点击改为行内「查看」按钮，行点击保留为附加手段。验证：键盘 `Tab` 到「查看」按钮并回车可进入轨迹。
- [x] 4.3 表单补关联标签与 `autocomplete`:密码与新密钥用 `new-password`/`current-password`。代码类字段设 `spellcheck="false"`。`Observability` 与 `ToolsPanel` 的过滤框补标签。验证：浏览器不再提示保存错误类型的凭据，读屏可读出控件名称。
- [x] 4.4 保存结果、错误与加载完成接入 `aria-live` 区域。验证：保存失败时读屏念出错误文本。
- [x] 4.5 `format.ts` 改用 `Intl.DateTimeFormat('sv-SE')` 输出 `YYYY-MM-DD HH:mm(:ss)`。验证：轨迹与会话列表时间显示与改动前一致。
- [x] 4.6 数值列统一等宽字体与 `tabular-nums`。折叠箭头等装饰元素加 `aria-hidden="true"`。验证：不同位数的 token 数按位对齐。
- [x] 4.7 全站补 `:focus-visible` 样式，删除 UA 默认以外的临时写法。验证：键盘遍历所有按钮与输入框，焦点始终可见。

## 5. 验证与收尾

- [x] 5.1 `grep` 检查裸色值：令牌文件之外不再出现十六进制颜色。验证：命令无输出。
- [x] 5.2 `pnpm typecheck`、`pnpm build`、`make build` 全部通过。验证：构建产物落在 `bin/awatchdog`。
- [x] 5.3 `make smoke` 通过，确认轨迹图例文案未漂移。验证：退出码 0。
- [x] 5.4 手工清单：折叠持久化、主题持久化与首屏不闪、375px 无整页横滚、弹窗 `Esc` 与回焦、键盘可展开轨迹。验证：五项逐条通过。
