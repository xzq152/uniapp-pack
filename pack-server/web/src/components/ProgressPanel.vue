<template>
  <div v-if="visible" class="inline-progress">
    <div class="ip-head">
      <span class="ip-stage">{{ state.job.stage || '准备中' }}</span>
      <span class="ip-pct">{{ state.job.progress }}%</span>
    </div>
    <t-progress :percentage="state.job.progress" :status="status" :label="false" :stroke-width="6" />

    <div
      v-if="finished"
      v-motion="doneMotion"
      class="done-box"
      style="margin-top: 12px"
    >
      <span
        class="done-icon"
        :class="ok ? 'ok' : 'bad'"
        v-motion="iconMotion"
      >
        <t-icon :name="ok ? 'check-circle-filled' : 'error-circle-filled'" size="22px" />
      </span>
      <div class="t">
        <div class="n">{{ ok ? apkLabel : '构建失败' }}</div>
        <div class="m">{{ ok ? doneMeta : friendlyError(state.job.error) }}</div>
        <div v-if="ok && outDir" class="m path" :title="outDir">{{ outDir }}</div>
      </div>
      <!-- 本地构建：产物就在本机目录里，直接打开文件夹比再下载一遍更实用 -->
      <t-button v-if="ok" theme="primary" :loading="opening" @click="openFolder">打开文件夹</t-button>
      <t-button v-else theme="primary" variant="outline" @click="retry">一键重试</t-button>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { state, retryJob, revealJob } from '../store'
import { fmtBytes, friendlyError } from '../utils'

const finished = computed(() => ['done', 'failed'].includes(state.job.status))
const ok = computed(() => state.job.status === 'done')

// 完成态入场：整体淡入上移，图标再带一点弹性放大。
// 用对象传给 v-motion（写成 :initial/:enter 属性会把这些属性原样输出到 DOM）。
const doneMotion = {
  initial: { opacity: 0, y: 10 },
  enter: { opacity: 1, y: 0, transition: { duration: 320 } },
}
const iconMotion = {
  initial: { scale: 0.6, opacity: 0 },
  enter: { scale: 1, opacity: 1, transition: { delay: 100, duration: 380 } },
}

// 只在真正有进度后展示，避免空占位置
const visible = computed(() => !!state.job.id && (state.job.progress > 0 || finished.value))

const status = computed(() => {
  if (state.job.status === 'failed') return 'error'
  if (state.job.status === 'done') return 'success'
  return 'active'
})

const job = computed(() => state.jobs.find((x) => x.id === state.job.id))
const apkLabel = computed(() => job.value?.apk || 'APK 已生成')
const apkSize = computed(() => job.value?.apkSize || 0)
const outDir = computed(() => job.value?.outDir || '')

// 渠道包列表（老记录没有 apks 字段）
const apks = computed(() => {
  const j = job.value
  if (j && Array.isArray(j.apks) && j.apks.length) return j.apks
  return j && j.apk ? [{ channel: '', file: j.apk, size: j.apkSize, exists: true }] : []
})

const doneMeta = computed(() => {
  const n = apks.value.length
  const extra = n > 1 ? `${n} 个渠道包 · ` : ''
  return `${extra}${fmtBytes(apkSize.value)} · 已保存在本机`
})

const opening = ref(false)

async function openFolder() {
  if (opening.value) return
  // 用 state.job.id：列表还没加载完也能打开（job.value 可能暂时是 undefined）
  const id = state.job.id
  if (!id) return
  opening.value = true
  try {
    const dir = await revealJob(id)
    if (dir) MessagePlugin.success('已在文件管理器中打开', 2000)
  } finally {
    opening.value = false
  }
}

function retry() {
  const j = state.jobs.find((x) => x.id === state.job.id)
  if (!j) return
  if (!j.hasUpload) {
    MessagePlugin.warning('原包已清理，请重新上传后再打包')
    return
  }
  retryJob(j.id)
}
</script>

<style scoped>
/* 产物目录只在桌面端完整显示，窄屏省略中间部分，悬停看全路径 */
.path {
  font-family: ui-monospace, Consolas, monospace;
  font-size: 11.5px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 完成图标：包一层好做缩放动画，颜色按成功/失败区分 */
.done-icon {
  display: inline-flex; align-items: center; justify-content: center;
  flex: none;
}
.done-icon.ok { color: #12a150; }
.done-icon.bad { color: #d54941; }
</style>
