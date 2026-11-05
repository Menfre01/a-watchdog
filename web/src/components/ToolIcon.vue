<script setup lang="ts">
import AppIcon from './AppIcon.vue'
import { toolIconName } from '../toolIcons'

defineProps<{ name: string }>()
</script>

<template>
  <AppIcon :name="toolIconName(name)" :size="14" />
</template>
