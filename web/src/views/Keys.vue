<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type ClientKey } from '../api'
import AppDialog from '../components/AppDialog.vue'
import AppIcon from '../components/AppIcon.vue'
import { pushToast } from '../toast'

const rows = ref<ClientKey[]>([])
const name = ref('')
const error = ref('')
const createdKey = ref('')
const dialogOpen = ref(false)
const formError = ref('')
const copied = ref(false)
const pendingDelete = ref<ClientKey | null>(null)
const confirmName = ref('')
let copyTimer: ReturnType<typeof setTimeout> | undefined

function msg(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

async function load(): Promise<void> {
  error.value = ''
  try {
    rows.value = await api.get<ClientKey[]>('/console/api/client-keys')
  } catch (e) {
    error.value = msg(e)
  }
}

function openCreate(): void {
  name.value = ''
  formError.value = ''
  dialogOpen.value = true
}

async function create(): Promise<void> {
  formError.value = ''
  try {
    const created = await api.post<ClientKey>('/console/api/client-keys', { name: name.value })
    createdKey.value = created.key ?? ''
    name.value = ''
    dialogOpen.value = false
    await load()
  } catch (e) {
    formError.value = msg(e)
  }
}

// legacyCopy 是剪贴板接口不可用时的退路:非本机 HTTP 下没有 secure context。
function legacyCopy(text: string): boolean {
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.opacity = '0'
  document.body.appendChild(area)
  area.select()
  const ok = document.execCommand('copy')
  document.body.removeChild(area)
  return ok
}

// copyKey 复制明文密钥。剪贴板接口需要安全上下文;本机 HTTP 视为安全。
async function copyKey(): Promise<void> {
  let ok = false
  try {
    await navigator.clipboard.writeText(createdKey.value)
    ok = true
  } catch {
    ok = legacyCopy(createdKey.value)
  }
  if (!ok) {
    pushToast('复制失败,请手动选中密钥文本', 'danger')
    return
  }
  copied.value = true
  clearTimeout(copyTimer)
  copyTimer = setTimeout(() => {
    copied.value = false
  }, 1500)
}

// askDelete 打开删除确认:必须先清空上次输入的名称。
function askDelete(k: ClientKey): void {
  pendingDelete.value = k
  confirmName.value = ''
}

// removeKey 只有名称完整匹配时才删除;后端也按 id 再校验一次存在性。
async function removeKey(): Promise<void> {
  const target = pendingDelete.value
  if (!target || confirmName.value !== target.name) return
  try {
    await api.del(`/console/api/client-keys/${target.id}`)
    pushToast(`已删除 ${target.name}`, 'ok')
    pendingDelete.value = null
    await load()
  } catch (e) {
    pushToast(msg(e), 'danger')
  }
}

async function toggle(k: ClientKey): Promise<void> {
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
  <section class="island">
    <header class="island-head">
      <h2>客户端密钥</h2>
      <button class="text-action" type="button" @click="openCreate"><AppIcon name="plus" />新建</button>
    </header>

    <p v-if="error" class="error" role="alert">{{ error }}</p>

  <p v-if="createdKey" class="notice ok secret" role="status" aria-live="polite">
    新密钥(仅展示一次,请立即保存):
    <span class="secret-line">
      <code class="mono">{{ createdKey }}</code>
      <button
        type="button"
        class="ghost copy"
        :class="{ done: copied }"
        :aria-label="copied ? '已复制' : '复制密钥'"
        :title="copied ? '已复制' : '复制密钥'"
        @click="copyKey"
      >
        <AppIcon :name="copied ? 'check' : 'copy'" />
      </button>
      <span class="sr-only" aria-live="polite">{{ copied ? '已复制' : '' }}</span>
    </span>
    <button type="button" class="close" aria-label="关闭通知" @click="createdKey = ''">
      <AppIcon name="close" />
    </button>
  </p>

    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th scope="col">名称</th>
            <th scope="col">状态</th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="k in rows" :key="k.id">
            <td>{{ k.name }}</td>
            <td>
              <span class="badge" :class="k.enabled ? 'on' : 'off'">{{ k.enabled ? '启用' : '停用' }}</span>
            </td>
            <td class="row-actions">
              <button
                class="icon-btn"
                type="button"
                :aria-label="k.enabled ? '停用' : '启用'"
                :title="k.enabled ? '停用' : '启用'"
                @click="toggle(k)"
              >
                <AppIcon name="power" />
              </button>
              <button class="icon-btn danger" type="button" aria-label="删除" title="删除" @click="askDelete(k)">
                <AppIcon name="trash" />
              </button>
            </td>
          </tr>
          <tr v-if="rows.length === 0">
            <td colspan="3" class="empty-cell">
              <button class="empty-action" type="button" @click="openCreate">
                <AppIcon name="plus" />添加第一个密钥
              </button>
              <p class="empty-hint">新建密钥后即可接入客户端。</p>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <AppDialog :open="dialogOpen" title="新建密钥" @close="dialogOpen = false" @submit="create">
      <label class="field">
        用途名称
        <input v-model="name" name="key-name" type="text" autocomplete="off" placeholder="claude-code" />
      </label>
      <p v-if="formError" class="error" role="alert">{{ formError }}</p>
      <template #footer>
        <button class="ghost" type="button" @click="dialogOpen = false">取消</button>
        <button class="primary" type="submit">新建</button>
      </template>
    </AppDialog>

    <AppDialog
      :open="pendingDelete !== null"
      :title="`删除密钥 ${pendingDelete?.name ?? ''}`"
      @close="pendingDelete = null"
      @submit="removeKey"
    >
      <p>删除后该密钥立刻失效,且不可恢复。请输入完整名称以确认:</p>
      <label class="field">
        名称
        <input v-model="confirmName" name="key-delete-confirm" type="text" autocomplete="off" spellcheck="false" />
      </label>
      <template #footer>
        <button class="ghost" type="button" @click="pendingDelete = null">取消</button>
        <button class="primary" type="submit" :disabled="confirmName !== pendingDelete?.name">确认删除</button>
      </template>
    </AppDialog>
  </section>
</template>

<style scoped>

code {
  overflow-wrap: anywhere;
}

/* 密钥提示要留到复制完成,不自动消失;给一个手动关闭入口。 */
.secret {
  position: relative;
  padding-right: 32px;
}

.secret .close {
  position: absolute;
  top: 6px;
  right: 8px;
  border: 0;
  background: none;
  color: var(--muted);
  font-size: var(--fs-16);
  line-height: 1;
}

.secret .close:hover {
  color: var(--text);
}

.secret-line {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  margin-top: var(--sp-1);
}

/* 复制按钮定宽:文案在「复制 / 已复制」之间切换时不挪位 */
.copy {
  flex: 0 0 auto;
  padding: 5px 7px;
}

/* 复制成功后图标转为对勾并短暂变绿,替代文字反馈 */
.copy.done {
  color: var(--ok);
  border-color: var(--ok);
}
</style>
