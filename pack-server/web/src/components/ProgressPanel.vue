<template>
  <div v-if="visible" class="inline-progress">
    <div class="ip-head">
      <span class="ip-stage">{{ state.job.stage || '准备中' }}</span>
      <span class="ip-pct">{{ state.job.progress }}%</span>
    </div>
    <t-progress :percentage="state.job.progress" :status="status" :label="false" :stroke-width="6" />

    <div v-if="finished" class="done-box" style="margin-top: 12px">
      <t-icon
        :name="ok ? 'check-circle-filled' : 'error-circle-filled'"
        size="22px"
        :style="{ color: ok ? '#12a150' : '#d54941' }"
      />
      <div class="t">
        <div class="n">{{ ok ? apkName : '构建失败' }}</div>
        <div class="m">
          {{ ok ? `${fmtBytes(apkSize)} · 可直接下载安装` : friendlyError(state.job.error) }}
        </div>
      </div>
      <t-dropdown
        v-if="ok && apks.length > 1"
        trigger="click"
        :options="apkOptions"
        :popup-props="{ overlayInnerStyle: { minWidth: '190px' } }"
        @click="(d) => downloadOne(d.value)"
      >
        <t-button theme="primary" :loading="downloading">下载 APK ({{ apks.length }})</t-button>
      </t-dropdown>
      <t-button v-else-if="ok" theme="primary" :loading="downloading" @click="download">下载 APK</t-button>
      <t-button v-else theme="primary" variant="outline" @click="retry">一键重试</t-button>
      <t-button variant="outline" @click="resetForm">再打一个</t-button>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { state, resetForm, retryJob, loadJobs } from '../store'
import { fmtBytes, friendlyError } from '../utils'
import { fastDownload } from '../download'

const finished = computed(() => ['done', 'failed'].includes(state.job.status))
const ok = computed(() => state.job.status === 'done')

// 只在真正有进度后展示，避免空占位置
const visible = computed(() => !!state.job.id && (state.job.progress > 0 || finished.value))

const status = computed(() => {
  if (state.job.status === 'failed') return 'error'
  if (state.job.status === 'done') return 'success'
  return 'active'
})

const job = computed(() => state.jobs.find((x) => x.id === state.job.id))
const apkName = computed(() => job.value?.apk || 'APK 已生成')
const apkSize = computed(() => job.value?.apkSize || 0)

// 渠道包列表（老记录没有 apks 字段）
const apks = computed(() => {
  const j = job.value
  if (j && Array.isArray(j.apks) && j.apks.length) return j.apks
  return j && j.apk ? [{ channel: '', file: j.apk, size: j.apkSize, exists: true }] : []
})

const apkOptions = computed(() => {
  const list = apks.value
  const opts = list.map((a) => ({
    content: a.channel || '通用',
    value: a.file,
    disabled: !a.exists,
  }))
  if (list.length > 1) {
    opts.unshift({ content: `全部下载（zip）`, value: '__zip__' })
  }
  return opts
})

async function downloadOne(file) {
  const j = job.value
  if (!j) return
  if (file === '__zip__') {
    window.open(`/api/jobs/${j.id}/zip`, '_blank')
    return
  }
  const a = apks.value.find((x) => x.file === file)
  if (!a) return
  if (downloading.value) return
  downloading.value = true
  try {
    await fastDownload(j.id, a.file, a.size)
  } finally {
    downloading.value = false
  }
}

const downloading = ref(false)

async function download() {
  if (downloading.value) return
  downloading.value = true
  try {
    await loadJobs()
    const j = state.jobs.find((x) => x.id === state.job.id)
    if (!j?.apk) return
    if (!j.apkExists) {
      MessagePlugin.warning('这个产物已被清理（只保留最近 5 次）')
      return
    }
    await fastDownload(j.id, j.apk, j.apkSize)
  } finally {
    downloading.value = false
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
