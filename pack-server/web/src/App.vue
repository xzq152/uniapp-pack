<template>
  <LoginView v-if="state.needLogin" />

  <div v-else class="app-shell">
    <header class="topbar">
      <div class="brand">
        <span class="logo"><t-icon name="logo-android" size="20px" /></span>
        <h1>uniapp 安卓本地打包</h1>

        <div class="env-pill" :class="pillClass">
          <span class="dot" :class="{ pulse: state.detecting }"></span>
          <span>{{ envText }}</span>
          <t-button
            v-if="canStart"
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
      <!-- 首次检测中 -->
      <t-card v-if="state.detecting" :bordered="true">
        <div class="detecting">
          <t-loading size="small" />
          <span>正在检测 Docker 服务…</span>
        </div>
      </t-card>

      <!-- 检测完确实连不上 -->
      <t-card v-else-if="!state.runtime.dockerOk" :bordered="true">
        <t-empty
          type="network-error"
          title="Docker 服务未连接"
          :description="state.runtime.message || '请确认 Docker Desktop 已启动，然后点重新检测'"
        >
          <template #action>
            <t-button variant="outline" @click="refresh">重新检测</t-button>
          </template>
        </t-empty>
      </t-card>

      <!-- 环境未启动 -->
      <t-card v-else-if="!runtimeOn" :bordered="true">
        <t-empty title="打包环境已停止" description="容器停止时不占内存。需要打包时启动，约 2 秒。">
          <template #action>
            <t-button theme="primary" :loading="busy" @click="act('start')">启动打包环境</t-button>
          </template>
        </t-empty>
      </t-card>

      <!-- 主流程：上传 → 表单 → 提交（进度在按钮下方） -->
      <template v-else>
        <t-card :bordered="true" class="main-card">
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

        <t-card :bordered="true">
          <template #header>
            <div class="card-head">
              <span class="card-title">打包记录</span>
              <span class="card-desc">按时间倒序 · 只保留最近 {{ state.env.keepJobs }} 条 · 展开可看签名与哈希</span>
            </div>
          </template>
          <RecordsPanel />
        </t-card>
      </template>
    </main>

    <LogDialog />
    <RefillDialog />
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { state, runtimeOn, boot, runtimeAction, logout, loadRuntime } from './store'
import LoginView from './components/LoginView.vue'
import UploadPanel from './components/UploadPanel.vue'
import ConfigPanel from './components/ConfigPanel.vue'
import RecordsPanel from './components/RecordsPanel.vue'
import LogDialog from './components/LogDialog.vue'
import RefillDialog from './components/RefillDialog.vue'

const busy = ref(false)

const canStart = computed(
  () => !state.detecting && state.runtime.dockerOk && state.runtime.imageOk && !state.runtime.running,
)

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
