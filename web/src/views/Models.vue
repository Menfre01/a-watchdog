<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { api, type Model, type Provider } from '../api'
import AppDialog from '../components/AppDialog.vue'
import AppIcon from '../components/AppIcon.vue'
import { pushToast } from '../toast'

const rows = ref<Model[]>([])
const providers = ref<Provider[]>([])
const error = ref('')
const formError = ref('')
const dialogOpen = ref(false)
const editingID = ref<number | null>(null)
const pendingDelete = ref<Model | null>(null)
const createBtn = ref<HTMLButtonElement | null>(null)
const form = reactive({
  name: '',
  provider_id: 0,
  upstream_model: '',
  enabled: true,
})

function msg(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

async function load(): Promise<void> {
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

function reset(): void {
  editingID.value = null
  form.name = ''
  form.provider_id = providers.value[0]?.id ?? 0
  form.upstream_model = ''
  form.enabled = true
}

function openCreate(): void {
  reset()
  formError.value = ''
  dialogOpen.value = true
}

function openEdit(m: Model): void {
  editingID.value = m.id
  form.name = m.name
  form.provider_id = m.provider_id
  form.upstream_model = m.upstream_model
  form.enabled = m.enabled
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
      await api.post('/console/api/models', body)
      pushToast(`已新建 ${form.name}`, 'ok')
    } else {
      await api.put(`/console/api/models/${editingID.value}`, body)
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
    await api.del(`/console/api/models/${target.id}`)
    pushToast(`已删除 ${target.name}`, 'ok')
    await load()
    // 触发删除的行已被刷新移除,把焦点交给列表页的主操作。
    createBtn.value?.focus()
  } catch (e) {
    error.value = msg(e)
  }
}

onMounted(load)
</script>

<template>
  <section class="island">
    <header class="island-head">
      <h2>模型目录</h2>
      <button ref="createBtn" class="primary" type="button" @click="openCreate">
        <AppIcon name="plus" />新建条目
      </button>
    </header>

    <p v-if="error" class="error" role="alert">{{ error }}</p>

    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th scope="col">可见名</th>
            <th scope="col">状态</th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in rows" :key="m.id">
            <td class="mono truncate">{{ m.name }}</td>
            <td>
              <span class="badge" :class="m.enabled ? 'on' : 'off'">{{ m.enabled ? '启用' : '停用' }}</span>
            </td>
            <td class="row-actions">
              <button class="icon-btn" type="button" aria-label="编辑" title="编辑" @click="openEdit(m)">
                <AppIcon name="pencil" />
              </button>
              <button class="icon-btn danger" type="button" aria-label="删除" title="删除" @click="pendingDelete = m">
                <AppIcon name="trash" />
              </button>
            </td>
          </tr>
          <tr v-if="rows.length === 0">
            <td colspan="3" class="muted">暂无模型条目。先新建接入,再添加条目。</td>
          </tr>
        </tbody>
      </table>
    </div>

  <AppDialog :open="dialogOpen" :title="editingID === null ? '新建条目' : '编辑条目'" @close="closeDialog" @submit="submit">
    <label class="field">
      客户端可见名
      <input
        v-model="form.name"
        name="model-name"
        type="text"
        autocomplete="off"
        spellcheck="false"
        placeholder="claude-sonnet-4-5"
      />
    </label>
    <label class="field">
      Provider
      <select v-model.number="form.provider_id" name="model-provider">
        <option v-for="p in providers" :key="p.id" :value="p.id">{{ p.name }}</option>
      </select>
    </label>
    <label class="field">
      上游模型名
      <input
        v-model="form.upstream_model"
        name="model-upstream"
        type="text"
        autocomplete="off"
        spellcheck="false"
        placeholder="claude-sonnet-4-5"
      />
    </label>
    <label class="check-row">
      <input v-model="form.enabled" name="model-enabled" type="checkbox" />
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
    :title="`删除条目 ${pendingDelete?.name ?? ''}`"
    @close="pendingDelete = null"
    @submit="remove"
  >
    <p>删除后,客户端用该名称发起的请求将无法路由。此操作不可撤销。</p>
    <template #footer>
      <button class="ghost" type="button" @click="pendingDelete = null">取消</button>
      <button class="primary" type="submit">确认删除</button>
    </template>
  </AppDialog>
  </section>
</template>

<style scoped>


</style>
