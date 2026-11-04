<script setup lang="ts">
import { computed } from 'vue'
import { marked, Renderer } from 'marked'
import DOMPurify from 'dompurify'

const props = defineProps<{ text: string }>()

// 内容里的尖括号标记不是 HTML:Claude Code 等客户端用 <transcript>、
// <system-reminder> 这类标签标注注入内容。它们会被 marked 当成 HTML 标签,
// 再被 DOMPurify 剥掉——标签消失后只剩空白,看起来像"空块"。
// 这里先转义成纯文本,让结构标记原样可见,顺带也堵掉了内联 HTML 的执行路径。
const renderer = new Renderer()
renderer.html = ({ text }: { text: string }) =>
  text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

// 内容来自模型或客户端,属不可信输入:渲染后仍过一遍 DOMPurify(双保险)。
const html = computed(() => {
  const raw = marked.parse(props.text, { async: false, gfm: true, breaks: true, renderer })
  return DOMPurify.sanitize(raw as string)
})
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -- 内容已由 DOMPurify 净化 -->
  <div class="md" v-html="html"></div>
</template>

<style scoped>
.md {
  font-size: 13px;
  line-height: 1.6;
  word-break: break-word;
}
.md :deep(p) {
  margin: 0 0 8px;
}
.md :deep(p:last-child) {
  margin-bottom: 0;
}
.md :deep(h1),
.md :deep(h2),
.md :deep(h3),
.md :deep(h4) {
  margin: 12px 0 6px;
  font-weight: 600;
  line-height: 1.3;
}
.md :deep(h1) {
  font-size: 17px;
}
.md :deep(h2) {
  font-size: 15px;
}
.md :deep(h3),
.md :deep(h4) {
  font-size: 14px;
}
.md :deep(ul),
.md :deep(ol) {
  margin: 0 0 8px;
  padding-left: 22px;
}
.md :deep(li) {
  margin: 2px 0;
}
.md :deep(code) {
  font-family: var(--mono);
  font-size: 12px;
  background: var(--code-bg);
  border-radius: var(--r-sm);
  padding: 1px 5px;
}
.md :deep(pre) {
  margin: 8px 0;
  padding: 10px 12px;
  background: var(--surface-2);
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  overflow-x: auto;
}
.md :deep(pre code) {
  background: none;
  padding: 0;
  font-size: 12px;
  line-height: 1.5;
}
.md :deep(blockquote) {
  margin: 8px 0;
  padding: 2px 0 2px 12px;
  border-left: 3px solid var(--line);
  color: var(--muted);
}
.md :deep(table) {
  border-collapse: collapse;
  margin: 8px 0;
  font-size: 12px;
}
.md :deep(th),
.md :deep(td) {
  border: 1px solid var(--line);
  padding: 4px 8px;
  text-align: left;
}
.md :deep(th) {
  background: var(--surface-2);
  font-weight: 600;
}
.md :deep(a) {
  color: var(--accent);
}
.md :deep(hr) {
  border: 0;
  border-top: 1px solid var(--line);
  margin: 12px 0;
}
.md :deep(img) {
  max-width: 100%;
}
</style>
