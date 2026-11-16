<template>
  <t-dialog
    v-model:visible="state.dockerDialogOpen"
    header="Docker 地址"
    width="560px"
    :footer="false"
    :close-on-overlay-click="true"
  >
    <div class="dh">
      <div class="dh-current">
        <span class="k">当前地址</span>
        <code class="v">{{ state.docker.host || '检测中…' }}</code>
        <t-tag v-if="state.docker.local" size="small" variant="light">本机</t-tag>
        <t-tag v-else size="small" theme="primary" variant="light">远程</t-tag>
      </div>

      <div class="dh-form">
        <t-input
          v-model="draft"
          placeholder="tcp://192.168.1.23:2375（留空 = 本机 Docker）"
          clearable
          @enter="onTest"
        />
        <div class="dh-actions">
          <t-button variant="outline" :loading="testing" @click="onTest">测试连接</t-button>
          <t-button theme="primary" :loading="saving" :disabled="!draft.trim() && state.docker.local" @click="onSave">
            切换
          </t-button>
        </div>
      </div>

      <div v-if="probe" class="dh-probe" :class="probe.ok ? 'ok' : 'bad'">
        <t-icon :name="probe.ok ? 'check-circle-filled' : 'error-circle-filled'" size="16px" />
        <span v-if="probe.ok">
          连接正常{{ probe.version ? ` · Docker ${probe.version}` : '' }}{{ probe.memGb ? ` · ${probe.memGb}` : '' }}
        </span>
        <span v-else>{{ probe.message || '连接失败' }}</span>
      </div>

      <div class="dh-hint">
        常用写法：<code>tcp://192.168.1.23:2375</code>、<code>192.168.1.23</code>（自动补 2375）。
        留空并点「切换」= 回到本机 Docker。切换会立即生效并记住，重启服务后仍然有效。
        <br />
        对方机器需要开启 Docker 的 TCP 2375 端口（Docker Desktop → Settings → General →
        「Expose daemon on tcp://localhost:2375 without TLS」，或在 Linux 上配
        <code>-H tcp://0.0.0.0:2375</code>）。
      </div>

      <div v-if="state.docker.envHost" class="dh-hint">
        环境变量里还设着 <code>DOCKER_HOST={{ state.docker.envHost }}</code>，
        但页面保存的地址优先级更高。
      </div>
    </div>
  </t-dialog>
</template>

<script setup>
import { ref, watch } from 'vue'
import { state, testDockerHost, setDockerHost } from '../store'

const draft = ref('')
const probe = ref(null)
const testing = ref(false)
const saving = ref(false)

// 打开弹窗时用当前地址预填（本机就留空，方便直接填远程）
watch(
  () => state.dockerDialogOpen,
  (open) => {
    if (!open) return
    probe.value = null
    draft.value = state.docker.local ? '' : state.docker.raw || ''
  },
  { immediate: true },
)

async function onTest() {
  testing.value = true
  probe.value = null
  try {
    probe.value = await testDockerHost(draft.value)
  } finally {
    testing.value = false
  }
}

async function onSave() {
  saving.value = true
  try {
    const ok = await setDockerHost(draft.value)
    if (ok) state.dockerDialogOpen = false
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.dh { display: flex; flex-direction: column; gap: 14px; }
.dh-current {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  padding: 10px 12px; background: #f7f8fa; border-radius: 6px;
}
.dh-current .k { font-size: 12px; color: #8a94a1; }
.dh-current .v {
  font-family: ui-monospace, Consolas, monospace; font-size: 13px;
  color: #1c2126; word-break: break-all;
}
.dh-form { display: flex; flex-direction: column; gap: 10px; }
.dh-actions { display: flex; gap: 8px; justify-content: flex-end; }
.dh-probe {
  display: flex; align-items: center; gap: 6px;
  font-size: 12.5px; padding: 8px 10px; border-radius: 6px; line-height: 1.5;
}
.dh-probe.ok { background: #f0f9f3; color: #1a7f3c; border: 1px solid #cfe9d8; }
.dh-probe.bad { background: #fdf2f2; color: #c62f2f; border: 1px solid #f3d3d3; }
.dh-hint { font-size: 12px; color: #8a94a1; line-height: 1.7; }
.dh-hint code {
  font-family: ui-monospace, Consolas, monospace;
  background: #f2f3f5; padding: 1px 4px; border-radius: 3px; color: #4b5563;
}
</style>
