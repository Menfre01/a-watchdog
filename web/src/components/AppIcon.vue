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
  key: [
    'M15 5a4 4 0 1 0 0 8 4 4 0 0 0 0-8Z',
    'M12.2 12.2 4 20.4V22h1.6l8.2-8.2',
    'M4 18h2',
    'M6 16h2',
  ],
  server: ['M4 4h16v6H4z', 'M4 14h16v6H4z', 'M8 7h.01', 'M8 17h.01'],
  file: ['M14 3H6v18h12V7l-4-4Z', 'M14 3v4h4'],
  terminal: ['M4 5h16v14H4z', 'M7 9l3 3-3 3', 'M13 15h4'],
  search: ['M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14Z', 'M16.5 16.5 21 21'],
  globe: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18Z', 'M3 12h18', 'M12 3c2.5 2.5 3.5 5.5 3.5 9s-1 6.5-3.5 9c-2.5-2.5-3.5-5.5-3.5-9s1-6.5 3.5-9Z'],
  bot: ['M12 3v3', 'M6 6h12a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2Z', 'M9 11h.01', 'M15 11h.01', 'M9 15h6'],
  list: ['M8 6h13', 'M8 12h13', 'M8 18h13', 'M3 6h.01', 'M3 12h.01', 'M3 18h.01'],
  wrench: ['M14 7a4 4 0 0 0-5 5l-6 6 3 3 6-6a4 4 0 0 0 5-5l-2 2-3-3 2-2Z'],
  notebook: ['M6 3h12v18H6z', 'M9 3v18', 'M9 8h6', 'M9 12h6'],
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
