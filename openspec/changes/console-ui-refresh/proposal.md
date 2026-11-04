# Proposal

## Why

控制台是唯一的运维入口，当前外壳停留在首版形态。侧栏固定 220px 不可收起，内容固定 1100px，只有亮色一套配色，新建与编辑表单常驻页面顶部。日常配置 providers 与排查轨迹时，这四点持续拖慢操作。

## What Changes

- **侧栏可折叠**。展开 232px 显示文字，折叠 56px 只显示图标。折叠状态写入 `localStorage`,刷新后保持。
- **内容自适应**。主区不设固定宽度上限，改用 `clamp()` 内边距。表格进横向滚动容器，窄屏下整页不出现横向滚动。
- **亮暗主题**。暗色为默认，亮色可切换。初值取系统 `prefers-color-scheme`,手动选择后持久化。颜色全部改为 CSS 变量，并写入 `color-scheme` 与 `theme-color`。
- **新建与编辑改弹窗**。providers、models 的新建与编辑改用 `<dialog>` 弹窗，列表页只保留列表与操作按钮。弹窗支持 `Esc` 关闭、焦点回收与内联错误。
- **可访问性基线**。可点的行与标题改为 `<button>`。表单控件补标签与 `autocomplete`。异步提示加 `aria-live`。焦点样式统一 `:focus-visible`。数字列用等宽字体与 `tabular-nums`。
- **设计令牌**。颜色、间距、字号、圆角收敛为一组 CSS 变量，组件不再写裸色值。

不改变后端接口、数据模型与鉴权流程。

## Capabilities

### New Capabilities

无。控制台界面属既有 `admin-console` 能力的组成部分。

### Modified Capabilities

- `admin-console`: 新增「控制台外壳」「主题切换」「弹窗式编辑」「可访问性与数值显示基线」四项行为要求。既有配置管理、可观测与轨迹要求不变。

## Impact

- 前端：`web/src/App.vue`、`web/src/views/*.vue`、`web/src/components/` 新增外壳与弹窗组件、`web/index.html`。
- 样式：全站裸色值改为令牌，新增暗色主题与断点。
- 构建：仍由 `web/` 打包后嵌入二进制，不新增运行时依赖。字体优先系统栈。
- 后端与数据库：无改动。
