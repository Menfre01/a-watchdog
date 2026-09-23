<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../api'

const emit = defineEmits<{ ok: [] }>()
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
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
    <form class="card login-card" @submit.prevent="submit">
      <h2>ds-context 控制台</h2>
      <label class="field">
        管理员口令
        <input v-model="password" type="password" autofocus />
      </label>
      <button class="primary" type="submit" :disabled="busy">登录</button>
      <div v-if="error" class="error">{{ error }}</div>
    </form>
  </div>
</template>

<style scoped>
.login-card {
  width: 320px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
</style>
