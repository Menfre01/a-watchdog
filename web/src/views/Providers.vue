<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { api, type ProbeResult, type Provider } from '../api'
import AppDialog from '../components/AppDialog.vue'
import AppIcon from '../components/AppIcon.vue'
import { pushToast } from '../toast'

const rows = ref<Provider[]>([])
const error = ref('')
const formError = ref('')
const dialogOpen = ref(false)
const editingID = ref<number | null>(null)
const pendingDelete = ref<Provider | null>(null)
const createBtn = ref<HTMLButtonElement | null>(null)
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

async function load(): Promise<void> {
  error.value = ''
  try {
    rows.value = await api.get<Provider[]>('/console/api/providers')
  } catch (e) {
    error.value = msg(e)
  }
}

function reset(): void {
  editingID.value = null
  form.name = ''
  form.protocol = 'anthropic-messages'
  form.base_url = ''
  form.api_key = ''
  form.probe_model = ''
  form.enabled = true
}

function openCreate(): void {
  reset()
  formError.value = ''
  dialogOpen.value = true
}

function openEdit(p: Provider): void {
  editingID.value = p.id
  form.name = p.name
  form.protocol = p.protocol
  form.base_url = p.base_url
  form.api_key = p.api_key
  form.probe_model = p.probe_model
  form.enabled = p.enabled
  formError.value = ''
  dialogOpen.value = true
}

function closeDialog(): void {
  dialogOpen.value = false
}

async function submit(): Promise<void> {
  formError.value = ''
  try {
    const body = { ...form }
    if (editingID.value === null) {
      await api.post('/console/api/providers', body)
      pushToast(`已新建 ${form.name}`, 'ok')
    } else {
      await api.put(`/console/api/providers/${editingID.value}`, body)
      pushToast(`已保存 ${form.name}`, 'ok')
    }
    dialogOpen.value = false
    reset()
    await load()
  } catch (e) {
    formError.value = msg(e)
  }
}

async function remove(): Promise<void> {
  const target = pendingDelete.value
  if (!target) return
  pendingDelete.value = null
  error.value = ''
  try {
    await api.del(`/console/api/providers/${target.id}`)
    pushToast(`已删除 ${target.name}`, 'ok')
    await load()
    // 触发删除的行已被刷新移除,把焦点交给列表页的主操作。
    createBtn.value?.focus()
  } catch (e) {
    error.value = msg(e)
  }
}

async function probe(p: Provider): Promise<void> {
  pushToast(`正在测试 ${p.name} …`, 'info')
  try {
    const r = await api.post<ProbeResult>(`/console/api/providers/${p.id}/test`)
    pushToast(`${p.name}: ${r.ok ? '连接成功' : '连接失败'} — ${r.detail}`, r.ok ? 'ok' : 'danger')
  } catch (e) {
    pushToast(`${p.name}: 测试失败 — ${msg(e)}`, 'danger')
  }
}

onMounted(load)
</script>

<template>
  <section class="island">
    <header class="island-head">
      <h2>接入</h2>
      <button ref="createBtn" class="primary" type="button" @click="openCreate">
        <AppIcon name="plus" />新建接入
      </button>
    </header>

    <p v-if="error" class="error" role="alert">{{ error }}</p>

    <div class="table-scroll">
      <table class="table-wide">
        <thead>
          <tr>
            <th scope="col">名称</th>
            <th scope="col">协议</th>
            <th scope="col">Base URL</th>
            <th scope="col">测试模型</th>
            <th scope="col">状态</th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in rows" :key="p.id">
            <td>{{ p.name }}</td>
            <td class="mono small">{{ p.protocol }}</td>
            <td class="truncate" :title="p.base_url">{{ p.base_url }}</td>
            <td class="mono small">{{ p.probe_model || '—' }}</td>
            <td>
              <span class="badge" :class="p.enabled ? 'on' : 'off'">{{ p.enabled ? '启用' : '停用' }}</span>
            </td>
            <td class="row-actions">
              <button class="icon-btn" type="button" aria-label="测试连接" title="测试连接" @click="probe(p)">
                <AppIcon name="plug" />
              </button>
              <button class="icon-btn" type="button" aria-label="编辑" title="编辑" @click="openEdit(p)">
                <AppIcon name="pencil" />
              </button>
              <button class="icon-btn danger" type="button" aria-label="删除" title="删除" @click="pendingDelete = p">
                <AppIcon name="trash" />
              </button>
            </td>
          </tr>
          <tr v-if="rows.length === 0">
            <td colspan="6" class="muted">暂无接入。点「新建接入」添加第一个。</td>
          </tr>
        </tbody>
      </table>
    </div>

  <AppDialog :open="dialogOpen" :title="editingID === null ? '新建接入' : '编辑接入'" @close="closeDialog" @submit="submit">
    <div class="form-row">
      <label class="field">
        名称
        <input v-model="form.name" name="provider-name" type="text" autocomplete="off" placeholder="anthropic-official" />
      </label>
      <label class="field">
        协议
        <select v-model="form.protocol" name="provider-protocol">
          <option value="anthropic-messages">anthropic-messages</option>
          <option value="openai-chat">openai-chat</option>
        </select>
      </label>
    </div>
    <label class="field">
      Base URL
      <input
        v-model="form.base_url"
        name="provider-base-url"
        type="text"
        autocomplete="off"
        spellcheck="false"
        placeholder="https://api.anthropic.com"
      />
    </label>
    <label class="field">
      API Key
      <input v-model="form.api_key" name="provider-api-key" type="password" autocomplete="new-password" spellcheck="false" />
    </label>
    <label class="field">
      测试模型
      <input
        v-model="form.probe_model"
        name="provider-probe-model"
        type="text"
        autocomplete="off"
        spellcheck="false"
        placeholder="claude-sonnet-4-5"
      />
    </label>
    <label class="check-row">
      <input v-model="form.enabled" name="provider-enabled" type="checkbox" />
      <span>启用</span>
    </label>
    <p v-if="formError" class="error" role="alert">{{ formError }}</p>

    <template #footer>
      <button class="ghost" type="button" @click="closeDialog">取消</button>
      <button class="primary" type="submit">{{ editingID === null ? '新建' : '保存' }}</button>
    </template>
  </AppDialog>

  <AppDialog
    :open="pendingDelete !== null"
    :title="`删除接入 ${pendingDelete?.name ?? ''}`"
    @close="pendingDelete = null"
    @submit="remove"
  >
    <p>删除后,引用它的模型条目将无法路由。此操作不可撤销。</p>
    <template #footer>
      <button class="ghost" type="button" @click="pendingDelete = null">取消</button>
      <button class="primary" type="submit">确认删除</button>
    </template>
  </AppDialog>
  </section>
</template>

<style scoped>
.row-actions {
  flex-wrap: nowrap;
}

</style>
