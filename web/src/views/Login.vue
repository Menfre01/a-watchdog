<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../api'

const emit = defineEmits<{ ok: [] }>()
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit(): Promise<void> {
  error.value = ''
  busy.value = true
  try {
    await api.post('/console/api/login', { password: password.value })
    emit('ok')
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="center">
    <form class="card login-card" :aria-busy="busy" @submit.prevent="submit">
      <h1>AWatchDog 控制台</h1>
      <label class="field">
        管理员口令
        <input
          v-model="password"
          name="admin-password"
          type="password"
          autocomplete="current-password"
          autofocus
        />
      </label>
      <button class="primary" type="submit" :disabled="busy">{{ busy ? '登录中…' : '登录' }}</button>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
    </form>
  </div>
</template>

<style scoped>
.login-card {
  width: min(320px, calc(100vw - 32px));
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
  margin: 0;
}
</style>
