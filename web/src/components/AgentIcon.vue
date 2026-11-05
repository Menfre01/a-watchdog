<script setup lang="ts">
import { computed } from 'vue'
import { AGENT_ICON_PATHS } from '../agentIcons'

const props = defineProps<{ agent: string }>()
const path = computed(() => AGENT_ICON_PATHS[props.agent] ?? AGENT_ICON_PATHS.other)
</script>

<template>
  <svg class="agent-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
    <path :d="path" fill="currentColor" fill-rule="evenodd" clip-rule="evenodd" />
  </svg>
</template>

<style scoped>
.agent-icon {
  display: block;
  width: 14px;
  height: 14px;
  flex: none;
}
</style>
