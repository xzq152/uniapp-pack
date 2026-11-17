<template>
  <div class="envstep">
    <!-- 已绑定且运行中：收成一条状态条，把舞台让给打包表单 -->
    <div v-if="ready" v-motion="barMotion" class="envbar">
      <span class="dot on"></span>
      <span class="envbar-text">
        打包环境运行中
        <span class="envbar-mode">{{ modeLabel }}</span>
        <code class="envbar-host">{{ hostLabel }}</code>
      </span>
      <span class="envbar-meta">{{ state.runtime.dockerMem || '-' }} · {{ state.runtime.image }}</span>
      <t-button size="small" variant="text" @click="unbind">换环境</t-button>
    </div>

    <!-- 未绑定 / 未运行：完整的引导卡 -->
    <div v-else v-motion="cardMotion" class="binder">
      <div class="binder-head">
        <div class="binder-icon">
          <t-icon name="server" size="20px" />
        </div>
        <div>
          <h2 class="binder-title">先绑定打包镜像环境</h2>
          <p class="binder-sub">
            打包要用一台装了 <code>uniapp-android-builder</code> 镜像的 Docker。选一套环境并启动后，才能上传资源和打包。
          </p>
        </div>
      </div>

      <!-- 两种环境二选一 -->
      <div class="modes">
        <button
          class="mode"
          :class="{ active: mode === 'local', disabled: !localUsable }"
          :disabled="!localUsable"
          @click="mode = 'local'"
        >
          <div class="mode-top">
            <t-icon name="desktop" size="18px" />
            <span class="mode-name">本机 Docker</span>
            <span v-if="!localUsable" class="mode-badge">Windows 不可用</span>
          </div>
          <div class="mode-desc">
            {{ localUsable ? '用这台机器上的 Docker，最快，不占网络带宽' : 'Windows 的 Docker 走命名管道，本服务只支持 unix socket / TCP' }}
          </div>
          <div class="mode-state" :class="localUsable ? 'ok' : 'off'">
            {{ localUsable ? '可使用' : '请改用远程' }}
          </div>
        </button>

        <button
          class="mode"
          :class="{ active: mode === 'remote' }"
          @click="mode = 'remote'"
        >
          <div class="mode-top">
            <t-icon name="internet" size="18px" />
            <span class="mode-name">远程 Docker</span>
          </div>
          <div class="mode-desc">打包机在别的机器上，填它的 Docker API 地址</div>
          <div class="mode-state ok">可使用</div>
        </button>
      </div>

      <!-- 远程地址输入 -->
      <transition name="slide-fade">
        <div v-if="mode === 'remote'" class="remote-box">
          <t-input
            v-model="remoteHost"
            placeholder="tcp://192.168.1.23:2375（也可只填 192.168.1.23，自动补 2375）"
            clearable
            @enter="onProbe"
          />
          <t-button variant="outline" :loading="probing" @click="onProbe">测试连接</t-button>
        </div>
      </transition>

      <!-- 探测结果 -->
      <transition name="slide-fade">
        <div v-if="probe" class="probe" :class="probe.ok ? 'ok' : 'bad'">
          <t-icon :name="probe.ok ? 'check-circle-filled' : 'error-circle-filled'" size="15px" />
          <span v-if="probe.ok">
            连接正常 · Docker {{ probe.version }}<template v-if="probe.memGb"> · {{ probe.memGb }}</template>
          </span>
          <span v-else>{{ probe.message || '连接失败' }}</span>
        </div>
      </transition>

      <div class="binder-actions">
        <t-button
          theme="primary"
          size="large"
          :loading="state.envBind.binding"
          :disabled="!canBind"
          @click="onBind"
        >
          {{ bindText }}
        </t-button>
        <span class="binder-hint">{{ hintText }}</span>
      </div>

      <div v-if="errText" class="binder-err">{{ errText }}</div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import {
  state, workflowReady, bindLocalEnv, bindRemoteEnv, unbindEnv, testDockerHost,
} from '../store'

const mode = ref('')
const remoteHost = ref('')
const probe = ref(null)
const probing = ref(false)

// v-motion 的变体用对象传入（写成 :initial/:enter 属性会漏到 DOM 上，
// 而且属性名会被当成普通 HTML attribute 输出 initial="[object Object]"）。
const cardMotion = {
  initial: { opacity: 0, y: 12 },
  enter: { opacity: 1, y: 0, transition: { duration: 380 } },
}
const barMotion = {
  initial: { opacity: 0, y: -6 },
  enter: { opacity: 1, y: 0, transition: { duration: 300 } },
}

const ready = computed(() => workflowReady.value)

// Windows 上本机 Docker 走 npipe，本项目只支持 unix socket / TCP，所以直接标不可用
const localUsable = computed(() => !/Windows/i.test(navigator.userAgent))

// 未绑定态下决定默认选中哪一项，并把当前端点填进远程输入框。
// 这些取值依赖 boot() 之后的 state.docker / state.booted，所以一并 watch，
// 不要写成 immediate —— 首次渲染时 boot() 还没返回，读到的是空值。
watch(
  [() => state.booted, ready],
  ([booted, isReady]) => {
    if (!booted || isReady) return
    probe.value = null
    if (!mode.value) mode.value = localUsable.value ? 'local' : 'remote'
    if (mode.value === 'remote' && !remoteHost.value) {
      remoteHost.value = state.docker.raw || ''
    }
  },
  { immediate: true },
)

const canBind = computed(() => {
  if (state.envBind.binding) return false
  if (mode.value === 'local') return localUsable.value
  return !!remoteHost.value.trim()
})

const bindText = computed(() => {
  if (mode.value === 'remote') return '绑定并启动远程环境'
  return '绑定并启动本机环境'
})

const hintText = computed(() => {
  if (mode.value === 'remote' && !remoteHost.value.trim()) {
    // 服务端还没配过远程地址时，直接给一个可照抄的写法
    return state.docker.raw ? '请填写远程 Docker 地址' : '例如 tcp://192.168.1.23:2375（对方需开放 TCP 2375）'
  }
  if (mode.value === 'remote') return '会先试连，再确认镜像并启动容器'
  return '会确认本机镜像并启动容器'
})

const modeLabel = computed(() => (state.envBind.mode === 'remote' ? '远程' : '本机'))

// 只有真正需要用户处理时才显示红条：runtime.message 在成功时也是「打包环境已启动」，
// 不能一律当错误渲染（之前就把它显示成红色了）。
const errText = computed(() => {
  if (state.detecting) return ''
  const m = state.runtime.message || ''
  if (!m) return ''
  // 正常状态的说法，不当作错误
  if (/已启动|已停止|可随时启动/.test(m)) return ''
  return m
})

const hostLabel = computed(() => {
  if (state.envBind.mode !== 'remote') return 'local'
  const h = state.docker.host || state.docker.raw || ''
  return h.replace(/^tcp:\/\//, '') || 'remote'
})

async function onProbe() {
  if (!remoteHost.value.trim()) {
    MessagePlugin.warning('请先填写远程 Docker 地址')
    return
  }
  probing.value = true
  probe.value = null
  try {
    probe.value = await testDockerHost(remoteHost.value)
  } finally {
    probing.value = false
  }
}

async function onBind() {
  if (!canBind.value) return
  probe.value = null
  if (mode.value === 'local') await bindLocalEnv()
  else await bindRemoteEnv(remoteHost.value)
}

function unbind() {
  unbindEnv()
  // 停在原环境上，用户重新选时会看到当前状态
  mode.value = state.docker.local ? (localUsable.value ? 'local' : 'remote') : 'remote'
  remoteHost.value = state.docker.raw || ''
}
</script>

<style scoped>
.envstep { margin-bottom: 16px; }

/* ---------- 已就绪：细状态条 ---------- */
.envbar {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 16px;
  background: #f0f9f3;
  border: 1px solid #cfe9d8;
  border-radius: 8px;
  font-size: 13px;
  color: #1a7f3c;
}
.envbar .dot {
  width: 8px; height: 8px; border-radius: 50%;
  background: #12a150; flex: none;
  box-shadow: 0 0 0 3px rgba(18, 161, 80, .15);
}
.envbar-text { display: flex; align-items: center; gap: 8px; font-weight: 500; }
.envbar-mode {
  font-size: 11px; font-weight: 400;
  padding: 1px 7px; border-radius: 9px;
  background: #dff3e6; color: #1a7f3c;
}
.envbar-host {
  font-family: ui-monospace, Consolas, monospace;
  font-size: 11.5px; color: #4b7d5e;
  background: transparent;
}
.envbar-meta {
  margin-left: auto;
  font-size: 12px; color: #6b8a78;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

/* ---------- 未就绪：引导卡 ---------- */
.binder {
  background: #fff;
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 24px 26px;
}
.binder-head { display: flex; gap: 14px; align-items: flex-start; margin-bottom: 20px; }
.binder-icon {
  width: 40px; height: 40px; flex: none;
  border-radius: 10px;
  background: linear-gradient(135deg, #0052d9, #4b8bff);
  color: #fff;
  display: flex; align-items: center; justify-content: center;
}
.binder-title { font-size: 16px; font-weight: 600; margin: 0 0 6px; }
.binder-sub { font-size: 12.5px; color: var(--text-3); margin: 0; line-height: 1.7; }
.binder-sub code {
  font-family: ui-monospace, Consolas, monospace;
  background: #f2f3f5; padding: 1px 5px; border-radius: 3px;
}

.modes { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.mode {
  text-align: left;
  border: 1.5px solid var(--line);
  border-radius: 8px;
  background: #fafbfc;
  padding: 14px 16px;
  cursor: pointer;
  transition: border-color .18s, background .18s, box-shadow .18s, transform .18s;
  font-family: inherit;
}
.mode:hover:not(.disabled) { border-color: #94b8ff; background: #f7faff; transform: translateY(-1px); }
.mode.active {
  border-color: #0052d9;
  background: #f2f6ff;
  box-shadow: 0 0 0 3px rgba(0, 82, 217, .1);
}
.mode.disabled { opacity: .55; cursor: not-allowed; }
.mode-top { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; color: #0052d9; }
.mode-name { font-size: 14px; font-weight: 600; color: var(--text-1); }
.mode-badge {
  font-size: 11px; color: #b45309;
  background: #fffbeb; border: 1px solid #fde68a;
  padding: 1px 6px; border-radius: 9px;
}
.mode-desc { font-size: 12px; color: var(--text-3); line-height: 1.6; min-height: 34px; }
.mode-state { font-size: 11.5px; margin-top: 8px; }
.mode-state.ok { color: #1a7f3c; }
.mode-state.off { color: #b45309; }

.remote-box { display: flex; gap: 8px; margin-top: 14px; }
.remote-box :deep(.t-input) { flex: 1; }

.probe {
  display: flex; align-items: center; gap: 6px;
  margin-top: 12px; padding: 9px 12px;
  border-radius: 6px; font-size: 12.5px; line-height: 1.5;
}
.probe.ok { background: #f0f9f3; color: #1a7f3c; border: 1px solid #cfe9d8; }
.probe.bad { background: #fdf2f2; color: #c62f2f; border: 1px solid #f3d3d3; }

.binder-actions { display: flex; align-items: center; gap: 12px; margin-top: 20px; flex-wrap: wrap; }
.binder-hint { font-size: 12px; color: var(--text-3); }
.binder-err {
  margin-top: 12px; font-size: 12px; color: #c62f2f;
  background: #fdf2f2; border-radius: 6px; padding: 9px 12px;
}

/* 展开/收起过渡 */
.slide-fade-enter-active { transition: all .28s cubic-bezier(.22,.61,.36,1); }
.slide-fade-leave-active { transition: all .18s ease; }
.slide-fade-enter-from,
.slide-fade-leave-to { opacity: 0; transform: translateY(-6px); }

@media (max-width: 760px) {
  .modes { grid-template-columns: 1fr; }
  .binder { padding: 18px; }
  .envbar { flex-wrap: wrap; }
  .envbar-meta { margin-left: 0; }
}
</style>
