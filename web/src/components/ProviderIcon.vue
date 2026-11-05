<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from './AppIcon.vue'
import { PROVIDER_ICON_PATHS } from '../providerIcons'

const props = defineProps<{ name: string; protocol?: string }>()

// providerIconKey 先用名称识别品牌,再用协议兜底;未知类型显示通用服务器图标。
const key = computed(() => {
  const name = props.name.toLowerCase()
  if (name.includes('deepseek')) return 'deepseek'
  if (name.includes('openai')) return 'openai'
  if (name.includes('anthropic') || name.includes('claude')) return 'anthropic'
  if (props.protocol === 'anthropic-messages') return 'anthropic'
  if (props.protocol === 'openai-chat') return 'openai'
  return ''
})
</script>

<template>
  <AppIcon v-if="!key" name="server" :size="16" />
  <svg v-else class="provider-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
    <path :d="PROVIDER_ICON_PATHS[key]" fill="currentColor" />
  </svg>
</template>

<style scoped>
.provider-icon {
  display: block;
  width: 16px;
  height: 16px;
  flex: none;
}
</style>
