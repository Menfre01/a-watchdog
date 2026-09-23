<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type ClientKey } from '../api'

const rows = ref<ClientKey[]>([])
const name = ref('')
const error = ref('')
const createdKey = ref('')

function msg(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

async function load() {
  error.value = ''
  try {
    rows.value = await api.get<ClientKey[]>('/console/api/client-keys')
  } catch (e) {
    error.value = msg(e)
  }
}

async function create() {
  error.value = ''
  createdKey.value = ''
  try {
    const created = await api.post<ClientKey>('/console/api/client-keys', { name: name.value })
    createdKey.value = created.key ?? ''
    name.value = ''
    await load()
  } catch (e) {
    error.value = msg(e)
  }
}

async function toggle(k: ClientKey) {
  error.value = ''
  try {
    await api.put(`/console/api/client-keys/${k.id}`, { enabled: !k.enabled })
    await load()
  } catch (e) {
    error.value = msg(e)
  }
}

onMounted(load)
</script>

<template>
  <h2>客户端密钥 (Keys)</h2>
  <div v-if="createdKey" class="notice">
    新密钥(仅展示一次,请立即保存):<br />
    <code>{{ createdKey }}</code>
  </div>
  <div class="card">
    <div class="form-row">
      <label class="field">用途名称<input v-model="name" type="text" placeholder="claude-code" /></label>
    </div>
    <button class="primary" @click="create">生成新密钥</button>
    <div v-if="error" class="error">{{ error }}</div>
  </div>
  <div class="card">
    <table>
      <thead>
        <tr><th>名称</th><th>前缀</th><th>状态</th><th>操作</th></tr>
      </thead>
      <tbody>
        <tr v-for="k in rows" :key="k.id">
          <td>{{ k.name }}</td>
          <td><code>{{ k.key_prefix }}…</code></td>
          <td><span class="badge" :class="k.enabled ? 'on' : 'off'">{{ k.enabled ? '启用' : '停用' }}</span></td>
          <td>
            <button class="ghost" @click="toggle(k)">{{ k.enabled ? '停用' : '启用' }}</button>
          </td>
        </tr>
        <tr v-if="rows.length === 0"><td colspan="4" class="muted">暂无密钥。</td></tr>
      </tbody>
    </table>
  </div>
</template>
