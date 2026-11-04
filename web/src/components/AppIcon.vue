<script setup lang="ts">
// 线性图标集:统一 24×24 视野、1.8 描边、圆头圆角。
// 图标只做装饰与识别,可访问名称由调用方的 aria-label 给出。
const props = defineProps<{ name: string; size?: number }>()

const paths: Record<string, string[]> = {
  plus: ['M12 5v14', 'M5 12h14'],
  pencil: ['M4 20h4L20 8l-4-4L4 16v4Z', 'M13 7l4 4'],
  trash: ['M4 7h16', 'M9 7V5h6v2', 'M6 7l1 13h10l1-13', 'M10 11v6', 'M14 11v6'],
  plug: ['M9 4v3', 'M15 4v3', 'M7 7h10v4a5 5 0 0 1-10 0V7Z', 'M12 16v4'],
  copy: ['M9 9h10v12H9Z', 'M15 9V5H5v12h4'],
  close: ['M6 6l12 12', 'M18 6L6 18'],
  check: ['M5 13l4 4L19 7'],
  power: ['M12 4v8', 'M7 6a7 7 0 1 0 10 0'],
  chevron: ['M15 6l-6 6 6 6'],
}

const size = props.size ?? 16
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    aria-hidden="true"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
  >
    <path v-for="(d, i) in paths[props.name] ?? []" :key="i" :d="d" />
  </svg>
</template>
