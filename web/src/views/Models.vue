<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { api, type Model, type Provider } from '../api'

const rows = ref<Model[]>([])
const providers = ref<Provider[]>([])
const error = ref('')
const editingID = ref<number | null>(null)
const form = reactive({
  name: '',
  provider_id: 0,
  upstream_model: '',
  enabled: true,
})

function msg(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

async function load() {
  error.value = ''
  try {
    ;[rows.value, providers.value] = await Promise.all([
      api.get<Model[]>('/console/api/models'),
      api.get<Provider[]>('/console/api/providers'),
    ])
  } catch (e) {
    error.value = msg(e)
  }
}

function providerName(id: number): string {
  return providers.value.find((p) => p.id === id)?.name ?? `#${id}`
}

function reset() {
  editingID.value = null
  form.name = ''
  form.provider_id = providers.value[0]?.id ?? 0
  form.upstream_model = ''
  form.enabled = true
}

function edit(m: Model) {
  editingID.value = m.id
  form.name = m.name
  form.provider_id = m.provider_id
  form.upstream_model = m.upstream_model
  form.enabled = m.enabled
}

async function submit() {
  error.value = ''
  try {
    const body = { ...form }
    if (editingID.value === null) {
      await api.post('/console/api/models', body)
    } else {
      await api.put(`/console/api/models/${editingID.value}`, body)
    }
    reset()
    await load()
  } catch (e) {
    error.value = msg(e)
  }
}

async function remove(m: Model) {
  if (!window.confirm(`删除模型条目 ${m.name}?`)) return
  error.value = ''
  try {
    await api.del(`/console/api/models/${m.id}`)
    await load()
  } catch (e) {
    error.value = msg(e)
  }
}

onMounted(load)
</script>

<template>
  <h2>模型目录 (Models)</h2>
  <div class="card">
    <div class="form-row">
      <label class="field">客户端可见名<input v-model="form.name" type="text" placeholder="claude-sonnet-4-5" /></label>
      <label class="field">
        Provider
        <select v-model.number="form.provider_id">
          <option v-for="p in providers" :key="p.id" :value="p.id">{{ p.name }}</option>
        </select>
      </label>
      <label class="field">上游模型名<input v-model="form.upstream_model" type="text" placeholder="claude-sonnet-4-5" /></label>
      <label class="field">启用<input v-model="form.enabled" type="checkbox" class="check" /></label>
    </div>
    <button class="primary" @click="submit">{{ editingID === null ? '新建条目' : '保存修改' }}</button>
    <button v-if="editingID !== null" class="ghost" style="margin-left: 8px" @click="reset">取消编辑</button>
    <div v-if="error" class="error">{{ error }}</div>
  </div>
  <div class="card">
    <table>
      <thead>
        <tr><th>可见名</th><th>Provider</th><th>上游模型名</th><th>状态</th><th>操作</th></tr>
      </thead>
      <tbody>
        <tr v-for="m in rows" :key="m.id">
          <td>{{ m.name }}</td>
          <td>{{ providerName(m.provider_id) }}</td>
          <td>{{ m.upstream_model }}</td>
          <td><span class="badge" :class="m.enabled ? 'on' : 'off'">{{ m.enabled ? '启用' : '停用' }}</span></td>
          <td>
            <button class="ghost" @click="edit(m)">编辑</button>
            <button class="ghost danger" @click="remove(m)">删除</button>
          </td>
        </tr>
        <tr v-if="rows.length === 0"><td colspan="5" class="muted">暂无模型条目。</td></tr>
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
