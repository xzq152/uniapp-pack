<template>
  <t-dialog
    v-model:visible="state.logOpen"
    header="构建日志"
    width="900px"
    :footer="false"
    :close-on-overlay-click="true"
    placement="center"
  >
    <div class="log-dialog">
      <div class="log-meta">
        <t-tag :theme="tagTheme" variant="light" size="small">{{ statusText }}</t-tag>
        <span class="stage">{{ state.job.stage || '等待中' }}</span>
        <span class="pct">{{ state.job.progress }}%</span>
        <span style="flex: 1"></span>
        <t-button size="small" variant="text" @click="copyLog">复制</t-button>
        <t-button size="small" variant="text" :disabled="!state.job.id" @click="downloadLog">下载</t-button>
        <t-checkbox v-model="autoScroll" size="small">自动滚动</t-checkbox>
      </div>

      <div ref="box" class="logbox" style="height: 52vh">
        <div v-for="(line, i) in state.job.logs" :key="i" :class="lineClass(line)">{{ line }}</div>
        <div v-if="!state.job.logs.length" style="color: #6b7480">
          {{ state.job.id ? '等待输出…' : '还没有构建任务' }}
        </div>
      </div>
    </div>
  </t-dialog>
</template>

<script setup>
import { computed, nextTick, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { state } from '../store'

const box = ref(null)
const autoScroll = ref(true)

const statusText = computed(
  () => ({ queued: '排队中', running: '构建中', done: '完成', failed: '失败' }[state.job.status] || '等待中'),
)
const tagTheme = computed(
  () => ({ queued: 'warning', running: 'primary', done: 'success', failed: 'danger' }[state.job.status] || 'default'),
)

watch(
  () => [state.job.logs.length, state.logOpen],
  async () => {
    if (!autoScroll.value) return
    await nextTick()
    const el = box.value
    if (el) el.scrollTop = el.scrollHeight
  },
)

function lineClass(line) {
  if (/✓|BUILD SUCCESSFUL/.test(line)) return 'ok'
  if (/^!|Warning|warning/.test(line)) return 'warn'
  if (/✗|FAILED|error|Error|错误/.test(line)) return 'err'
  if (/^━━|^> Task /.test(line)) return 'step'
  return ''
}

async function copyLog() {
  try {
    await navigator.clipboard.writeText(state.job.logs.join('\n'))
    MessagePlugin.success('日志已复制')
  } catch (e) {
    MessagePlugin.warning('复制失败，请用「下载」')
  }
}

function downloadLog() {
  if (state.job.id) location.href = `/api/jobs/${state.job.id}/log?download=1`
}
</script>

<style scoped>
.log-meta {
  display: flex; align-items: center; gap: 10px;
  margin-bottom: 8px; font-size: 12.5px; color: #5a6472;
}
.log-meta .stage { color: #5a6472; }
.log-meta .pct { font-variant-numeric: tabular-nums; font-weight: 600; color: #1c2126; }
</style>
