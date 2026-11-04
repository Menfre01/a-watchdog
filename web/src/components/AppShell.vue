<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { sidebarCollapsed, setSidebarCollapsed, type ShellItem } from '../shell'
import { current, toggle } from '../theme'

const props = defineProps<{ items: ShellItem[]; active: string }>()
const emit = defineEmits<{ select: [id: string]; logout: [] }>()

const collapsed = ref(sidebarCollapsed())
const theme = ref(current())
// narrow 表示视口低于 720px:此时侧栏固定为图标态,不跟随手动折叠。
const narrow = ref(false)
const railCollapsed = computed(() => collapsed.value || narrow.value)

let query: MediaQueryList | null = null

function syncNarrow(): void {
  narrow.value = query?.matches ?? false
}

onMounted(() => {
  query = window.matchMedia('(max-width: 720px)')
  syncNarrow()
  query.addEventListener('change', syncNarrow)
})

onBeforeUnmount(() => {
  query?.removeEventListener('change', syncNarrow)
})

function toggleRail(): void {
  if (narrow.value) return
  collapsed.value = !collapsed.value
  setSidebarCollapsed(collapsed.value)
}

function toggleTheme(): void {
  theme.value = toggle()
}
</script>

<template>
  <div class="shell" :class="{ collapsed: railCollapsed }">
    <aside class="rail">
      <div class="rail-head">
        <span class="brand" :class="{ hidden: railCollapsed }">AWatchDog</span>
        <button
          type="button"
          class="icon-btn"
          aria-controls="app-nav"
          :aria-expanded="!railCollapsed"
          :disabled="narrow"
          :aria-label="narrow ? '窄屏下侧栏固定为图标态' : railCollapsed ? '展开侧栏' : '折叠侧栏'"
          @click="toggleRail"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path :d="railCollapsed ? 'M9 6l6 6-6 6' : 'M15 6l-6 6 6 6'" />
          </svg>
        </button>
      </div>

      <nav id="app-nav" aria-label="控制台导航">
        <button
          v-for="item in props.items"
          :key="item.id"
          type="button"
          class="rail-btn"
          :class="{ active: props.active === item.id }"
          :aria-current="props.active === item.id ? 'page' : undefined"
          :aria-label="item.label"
          :title="railCollapsed ? item.label : undefined"
          @click="emit('select', item.id)"
        >
          <svg class="icon" viewBox="0 0 24 24" aria-hidden="true"><path :d="item.icon" /></svg>
          <span class="label">{{ item.label }}</span>
        </button>
      </nav>

      <div class="rail-foot">
        <button
          type="button"
          class="rail-btn"
          :aria-label="theme === 'dark' ? '切换为亮色主题' : '切换为暗色主题'"
          :title="railCollapsed ? (theme === 'dark' ? '切换为亮色主题' : '切换为暗色主题') : undefined"
          @click="toggleTheme"
        >
          <svg class="icon" viewBox="0 0 24 24" aria-hidden="true">
            <path
              :d="
                theme === 'dark'
                  ? 'M12 3v2M12 19v2M5 12H3M21 12h-2M6 6l1.5 1.5M16.5 16.5L18 18M18 6l-1.5 1.5M7.5 16.5L6 18M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8Z'
                  : 'M20 14a8 8 0 1 1-9-11 6 6 0 0 0 9 11Z'
              "
            />
          </svg>
          <span class="label">{{ theme === 'dark' ? '亮色' : '暗色' }}</span>
        </button>
        <button type="button" class="rail-btn" aria-label="退出登录" @click="emit('logout')">
          <svg class="icon" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M15 12H4m4-4-4 4 4 4M14 4h5v16h-5" />
          </svg>
          <span class="label">退出登录</span>
        </button>
      </div>
    </aside>

    <main class="content">
      <slot />
    </main>
  </div>
</template>

<style scoped>
.shell {
  display: flex;
  min-height: 100vh;
}

.rail {
  width: var(--rail-expanded);
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: var(--sp-1);
  padding: var(--sp-3) 10px;
  background: var(--surface);
  border-right: 1px solid var(--line);
  position: sticky;
  top: 0;
  height: 100vh;
}

.shell.collapsed .rail {
  width: var(--rail-collapsed);
  padding-inline: 8px;
}

.rail-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-2);
  padding-bottom: var(--sp-3);
}

.brand {
  font-weight: 600;
  font-size: var(--fs-16);
  white-space: nowrap;
  overflow: hidden;
}

.brand.hidden,
.collapsed .label {
  display: none;
}

.rail-head .icon-btn {
  margin-left: auto;
}

nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.rail-btn {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 9px;
  border: 0;
  border-radius: var(--r-md);
  background: transparent;
  color: var(--muted);
  font-size: var(--fs-14);
  text-align: left;
}

.rail-btn:hover {
  background: var(--surface-2);
  color: var(--text);
}

.rail-btn.active {
  background: var(--accent-soft);
  color: var(--accent-strong);
  font-weight: 500;
}

.collapsed .rail-btn {
  justify-content: center;
  padding-inline: 0;
}

.icon-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  background: transparent;
  color: var(--muted);
}

.icon-btn:hover {
  color: var(--text);
  border-color: var(--muted);
}

svg {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.8;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.rail-foot {
  margin-top: auto;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.content {
  flex: 1;
  min-width: 0;
  padding: var(--content-pad);
}

@media (max-width: 720px) {
  .rail {
    width: var(--rail-collapsed);
    padding-inline: 8px;
  }

  .brand,
  .rail-btn .label {
    display: none;
  }

  .rail-btn {
    justify-content: center;
    padding-inline: 0;
  }
}
</style>
