<script setup lang="ts">
import { dismissToast, toastItems } from '../toast'
</script>

<template>
  <div class="toasts" role="status" aria-live="polite">
    <div v-for="item in toastItems" :key="item.id" class="toast" :class="item.kind">
      <span class="text">{{ item.text }}</span>
      <button type="button" class="close" aria-label="关闭通知" @click="dismissToast(item.id)">×</button>
    </div>
  </div>
</template>

<style scoped>
.toasts {
  position: fixed;
  right: var(--sp-5);
  bottom: var(--sp-5);
  z-index: 40;
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
  width: min(380px, calc(100vw - 32px));
  pointer-events: none;
}

.toast {
  display: flex;
  align-items: flex-start;
  gap: var(--sp-3);
  padding: 10px 12px;
  border: 1px solid var(--line);
  border-left: 3px solid var(--muted);
  border-radius: var(--r-md);
  background: var(--surface);
  box-shadow: var(--shadow);
  font-size: var(--fs-13);
  pointer-events: auto;
}

.toast.ok {
  border-color: var(--ok);
  background: var(--ok-soft);
}

.toast.info {
  border-color: var(--accent);
  background: var(--accent-soft);
}

.toast.danger {
  border-color: var(--danger);
  background: var(--danger-soft);
}

.text {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}

.close {
  flex: 0 0 auto;
  border: 0;
  background: none;
  color: var(--muted);
  font-size: var(--fs-16);
  line-height: 1;
  padding: 0 2px;
}

.close:hover {
  color: var(--text);
}
</style>
