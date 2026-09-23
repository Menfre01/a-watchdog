<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { api, type Setting } from '../api'

const rows = ref<Setting[]>([])
const error = ref('')
const form = reactive({ key: '', value: '' })

function msg(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

async function load() {
  error.value = ''
  try {
    rows.value = await api.get<Setting[]>('/console/api/settings')
  } catch (e) {
    error.value = msg(e)
  }
}

async function submit() {
  error.value = ''
  try {
    await api.put(`/console/api/settings/${encodeURIComponent(form.key)}`, { value: form.value })
    form.key = ''
    form.value = ''
    await load()
  } catch (e) {
    error.value = msg(e)
  }
}

function edit(s: Setting) {
  form.key = s.key
  form.value = s.value
}

onMounted(load)
</script>

<template>
  <h2>全局配置 (Settings)</h2>
  <div class="card">
    <div class="form-row">
      <label class="field">键<input v-model="form.key" type="text" placeholder="upstream_timeout_ms" /></label>
      <label class="field">值<input v-model="form.value" type="text" /></label>
    </div>
    <button class="primary" @click="submit">保存</button>
    <div v-if="error" class="error">{{ error }}</div>
  </div>
  <div class="card">
    <table>
      <thead>
        <tr><th>键</th><th>值</th><th>操作</th></tr>
      </thead>
      <tbody>
        <tr v-for="s in rows" :key="s.key">
          <td><code>{{ s.key }}</code></td>
          <td>{{ s.value }}</td>
          <td><button class="ghost" @click="edit(s)">编辑</button></td>
        </tr>
        <tr v-if="rows.length === 0"><td colspan="3" class="muted">暂无配置项。</td></tr>
      </tbody>
    </table>
  </div>
</template>
