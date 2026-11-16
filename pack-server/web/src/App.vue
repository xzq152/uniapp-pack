<template>
  <LoginView v-if="state.needLogin" />

  <div v-else class="app-shell">
    <header class="topbar">
      <div class="brand">
        <span class="logo"><t-icon name="logo-android" size="20px" /></span>
        <h1>uniapp 安卓本地打包</h1>

        <div class="env-pill" :class="pillClass" :title="dockerTip">
          <span class="dot" :class="{ pulse: state.detecting }"></span>
          <span>{{ envText }}</span>
          <t-button
            v-if="!state.detecting"
            size="small"
            variant="text"
            class="host-btn"
            :title="'Docker 地址：' + (state.docker.host || '本机') + '（点击切换）'"
            @click="state.dockerDialogOpen = true"
          >
            <t-icon name="server" size="14px" style="margin-right: 3px" />
            {{ hostLabel }}
          </t-button>
          <t-button v-if="needRecheck" size="small" variant="outline" @click="refresh">重新检测</t-button>
          <t-button
            v-else-if="canStart"
            size="small"
            theme="primary"
            :loading="busy"
            @click="act('start')"
          >
            启动
          </t-button>
          <template v-else-if="runtimeOn">
            <t-button size="small" variant="outline" :loading="busy" @click="act('restart')">重建</t-button>
            <t-button size="small" variant="text" @click="act('stop')">停止</t-button>
          </template>
        </div>
      </div>

      <div class="topbar-right">
        <t-button v-if="state.env.auth" size="small" variant="text" @click="onLogout">退出</t-button>
      </div>
    </header>

    <main class="content">
      <!-- Docker 状态只在顶栏胶囊里体现，这里不再重复提示 -->
      <div v-if="!state.booted" class="detecting">
        <t-loading size="small" />
        <span>加载中…</span>
      </div>

      <t-card v-else :bordered="true" class="main-card">
        <template #header>
          <div class="card-head">
            <span class="card-title">新建打包</span>
            <span class="card-desc">{{ state.upload.id ? '确认信息后开始打包' : '先上传资源包' }}</span>
          </div>
        </template>

        <UploadPanel />

        <template v-if="state.upload.id">
          <div class="divider"></div>
          <ConfigPanel />
        </template>
      </t-card>
    </main>

    <LogDialog />
    <DockerHostDialog />
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { state, runtimeOn, boot, runtimeAction, logout, loadRuntime } from './store'
import LoginView from './components/LoginView.vue'
import UploadPanel from './components/UploadPanel.vue'
import ConfigPanel from './components/ConfigPanel.vue'
import LogDialog from './components/LogDialog.vue'
import DockerHostDialog from './components/DockerHostDialog.vue'

const busy = ref(false)

const canStart = computed(
  () => !state.detecting && state.runtime.dockerOk && state.runtime.imageOk && !state.runtime.running,
)

// Docker 连不上时，顶栏胶囊是唯一的操作入口
const needRecheck = computed(() => !state.detecting && !state.runtime.dockerOk)

// 悬停显示连的是哪个 daemon + 具体原因
const dockerTip = computed(() => {
  const host = state.docker.host || state.runtime.dockerHost || '本机'
  const msg = state.runtime.message || ''
  return `Docker: ${host}${msg ? '\n' + msg : ''}`
})

// 顶栏小标签：本机就写「本机」，远程只显示 IP:端口，避免把胶囊撑太宽。
// 用 host（轮询会持续同步）而不是 raw（只在本人切换时更新），
// 这样别人在另一个浏览器切换地址时，这里也能跟着变。
const hostLabel = computed(() => {
  if (state.docker.local) return '本机'
  const h = state.docker.host || state.docker.raw || state.runtime.dockerHost || ''
  return h.replace(/^tcp:\/\//, '').replace(/^http:\/\//, '') || '远程'
})

const pillClass = computed(() => {
  if (state.detecting) return 'checking'
  if (!state.runtime.dockerOk || !state.runtime.imageOk) return 'bad'
  return state.runtime.running ? 'on' : ''
})

const envText = computed(() => {
  if (state.detecting) return '检测 Docker 服务…'
  if (!state.runtime.dockerOk) return 'Docker 未连接'
  if (!state.runtime.imageOk) return '打包镜像缺失'
  return state.runtime.running ? '打包环境运行中' : '打包环境已停止'
})

async function act(action) {
  busy.value = true
  await runtimeAction(action)
  busy.value = false
}

async function onLogout() {
  await logout()
}

function refresh() {
  state.detecting = true
  loadRuntime()
}

onMounted(boot)
</script>
