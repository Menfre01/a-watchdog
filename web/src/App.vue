<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from './api'
import AppShell from './components/AppShell.vue'
import ToastStack from './components/ToastStack.vue'
import type { ShellItem } from './shell'
import Login from './views/Login.vue'
import Observability from './views/Observability.vue'
import Providers from './views/Providers.vue'
import Models from './views/Models.vue'
import Keys from './views/Keys.vue'

type Section = 'observability' | 'config'

const authed = ref(false)
const checking = ref(true)
const section = ref<Section>('observability')

const sections: ShellItem<Section>[] = [
  { id: 'observability', label: '可观测', icon: 'M3 12h4l2-5 3 10 2-5h7' },
  { id: 'config', label: '配置', icon: 'M4 7h9M17 7h3M4 17h3M11 17h9M15 4v6M9 14v6' },
]

function select(id: string): void {
  const next = sections.find((item) => item.id === id)
  if (next) section.value = next.id
}

async function check(): Promise<void> {
  try {
    await api.get('/console/api/session')
    authed.value = true
  } catch {
    authed.value = false
  } finally {
    checking.value = false
  }
}

async function logout(): Promise<void> {
  try {
    await api.post('/console/api/logout')
  } finally {
    authed.value = false
  }
}

onMounted(check)
</script>

<template>
  <div v-if="checking" class="center muted">加载中…</div>
  <Login v-else-if="!authed" @ok="authed = true" />
  <AppShell v-else :items="sections" :active="section" @select="select" @logout="logout">
    <Observability v-if="section === 'observability'" />
    <section v-else>
      <header class="page-head">
        <h1>配置</h1>
      </header>
      <div class="islands">
        <Providers />
        <div class="island-pair">
          <Models />
          <Keys />
        </div>
      </div>
    </section>
  </AppShell>
  <ToastStack v-if="authed && !checking" />
</template>
