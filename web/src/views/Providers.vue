<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { api, type ProbeResult, type Provider } from '../api'

const rows = ref<Provider[]>([])
const error = ref('')
const notice = ref('')
const editingID = ref<number | null>(null)
const form = reactive({
  name: '',
  protocol: 'anthropic-messages',
  base_url: '',
  api_key: '',
  probe_model: '',
  enabled: true,
})

function msg(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

async function load() {
  error.value = ''
  try {
    rows.value = await api.get<Provider[]>('/console/api/providers')
  } catch (e) {
    error.value = msg(e)
  }
}

function reset() {
  editingID.value = null
  form.name = ''
  form.protocol = 'anthropic-messages'
  form.base_url = ''
  form.api_key = ''
  form.probe_model = ''
  form.enabled = true
}

function edit(p: Provider) {
  editingID.value = p.id
  form.name = p.name
  form.protocol = p.protocol
  form.base_url = p.base_url
  form.api_key = p.api_key
  form.probe_model = p.probe_model
  form.enabled = p.enabled
}

async function submit() {
  error.value = ''
  try {
    const body = { ...form }
    if (editingID.value === null) {
      await api.post('/console/api/providers', body)
    } else {
      await api.put(`/console/api/providers/${editingID.value}`, body)
    }
    reset()
    await load()
  } catch (e) {
    error.value = msg(e)
  }
}

async function remove(p: Provider) {
  if (!window.confirm(`删除 provider ${p.name}?`)) return
  error.value = ''
  try {
    await api.del(`/console/api/providers/${p.id}`)
    await load()
  } catch (e) {
    error.value = msg(e)
  }
}

async function probe(p: Provider) {
  notice.value = `正在测试 ${p.name} …`
  try {
    const r = await api.post<ProbeResult>(`/console/api/providers/${p.id}/test`)
    notice.value = `${p.name}: ${r.ok ? '连接成功' : '连接失败'} — ${r.detail}`
  } catch (e) {
    notice.value = `${p.name}: 测试失败 — ${msg(e)}`
  }
}

onMounted(load)
</script>

<template>
  <h2>接入 (Providers)</h2>
  <div v-if="notice" class="notice">{{ notice }}</div>
  <div class="card">
    <div class="form-row">
      <label class="field">名称<input v-model="form.name" type="text" placeholder="anthropic-official" /></label>
      <label class="field">
        协议
        <select v-model="form.protocol">
          <option value="anthropic-messages">anthropic-messages</option>
          <option value="openai-chat">openai-chat</option>
        </select>
      </label>
      <label class="field">Base URL<input v-model="form.base_url" type="text" placeholder="https://api.anthropic.com" /></label>
      <label class="field">API Key<input v-model="form.api_key" type="password" /></label>
      <label class="field">测试模型<input v-model="form.probe_model" type="text" placeholder="测试连接所用模型名" /></label>
      <label class="field">启用<input v-model="form.enabled" type="checkbox" class="check" /></label>
    </div>
    <button class="primary" @click="submit">{{ editingID === null ? '新建 Provider' : '保存修改' }}</button>
    <button v-if="editingID !== null" class="ghost" style="margin-left: 8px" @click="reset">取消编辑</button>
    <div v-if="error" class="error">{{ error }}</div>
  </div>
  <div class="card">
    <table>
      <thead>
        <tr><th>名称</th><th>协议</th><th>Base URL</th><th>测试模型</th><th>状态</th><th>操作</th></tr>
      </thead>
      <tbody>
        <tr v-for="p in rows" :key="p.id">
          <td>{{ p.name }}</td>
          <td>{{ p.protocol }}</td>
          <td>{{ p.base_url }}</td>
          <td>{{ p.probe_model }}</td>
          <td><span class="badge" :class="p.enabled ? 'on' : 'off'">{{ p.enabled ? '启用' : '停用' }}</span></td>
          <td>
            <button class="ghost" @click="probe(p)">测试连接</button>
            <button class="ghost" @click="edit(p)">编辑</button>
            <button class="ghost danger" @click="remove(p)">删除</button>
          </td>
        </tr>
        <tr v-if="rows.length === 0"><td colspan="6" class="muted">暂无 provider,请先新建。</td></tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.check {
  width: auto;
  margin-top: 8px;
}
</style>
