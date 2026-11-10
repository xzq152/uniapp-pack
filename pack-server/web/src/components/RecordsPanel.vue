<template>
  <div>
    <div class="toolbar">
      <t-input
        v-model="kw"
        class="grow"
        placeholder="搜索 应用 / 包名 / 版本 / 文件名"
        clearable
      >
        <template #prefix-icon><t-icon name="search" /></template>
      </t-input>

      <t-select v-model="statusFilter" :options="statusOptions" style="width: 132px" />

      <t-button variant="outline" @click="openTrash">
        回收站
        <t-tag v-if="state.trash.length" size="small" variant="light" style="margin-left: 6px">
          {{ state.trash.length }}
        </t-tag>
      </t-button>

      <t-button variant="text" @click="reload">刷新</t-button>
    </div>

    <t-table
      row-key="id"
      table-layout="fixed"
      style="width: 100%"
      :data="pageData"
      :columns="columns"
      :pagination="pagination"
      :sort="sortState"
      :expanded-row-keys="expandedKeys"
      :row-class-name="rowClassName"
      :expand-icon="true"
      hover
      @page-change="onPageChange"
      @sort-change="onSortChange"
      @update:expanded-row-keys="(v) => (expandedKeys = v)"
    >
      <template #appName="{ row }">
        <div class="cell-app">
          <span class="nm">{{ row.appName || row.source || '-' }}</span>
          <t-tag v-if="row.splash" size="small" theme="primary" variant="light" title="本次使用了自定义启动图">
            自定义启动图
          </t-tag>
          <t-tag v-if="row.status === 'done' && row.testSigned" size="small" theme="warning" variant="light">
            测试签名
          </t-tag>
        </div>
        <div v-if="row.status === 'failed' && row.error" class="cell-err">
          {{ friendlyError(row.error) }}
        </div>
      </template>

      <template #versionName="{ row }">
        <div>{{ row.versionName || '-' }} <span class="cell-sub">({{ row.versionCode || '-' }})</span></div>
        <div class="cell-sub">{{ row.package || '-' }}</div>
      </template>

      <template #createdAt="{ row }">
        <span>{{ fmtTime(row.createdAt) }}</span>
        <div class="cell-sub">{{ row.elapsed ? `耗时 ${row.elapsed}` : '' }}</div>
      </template>

      <template #apkSize="{ row }">
        <div>{{ row.apkSize ? fmtBytes(row.apkSize) : '-' }}</div>
        <div v-if="row.minSdk" class="cell-sub">minSdk {{ row.minSdk }} / {{ row.targetSdk }}</div>
      </template>

      <template #ops="{ row }">
        <div class="ops-cell">
          <t-dropdown
            v-if="row.status === 'done' && row.apkExists && apkList(row).length > 1"
            trigger="click"
            :options="apkOptions(row)"
            :popup-props="{ overlayInnerStyle: { minWidth: '190px' } }"
            @click="(d) => downloadOne(row, d.value)"
          >
            <t-button size="small" theme="primary" variant="text">
              下载 ({{ apkList(row).length }})
            </t-button>
          </t-dropdown>
          <t-button
            v-else-if="row.status === 'done' && row.apk && row.apkExists"
            size="small"
            theme="primary"
            variant="text"
            @click.stop="download(row)"
          >
            下载
          </t-button>
          <span v-else-if="row.status === 'done'" class="dim-note">产物已清理</span>
          <t-button
            v-if="row.status === 'failed' && row.hasUpload"
            size="small"
            theme="primary"
            variant="text"
            @click.stop="retry(row)"
          >
            重试
          </t-button>
          <t-button
            v-if="row.status === 'running' || row.status === 'queued'"
            size="small"
            variant="text"
            @click.stop="view(row)"
          >
            查看
          </t-button>
          <t-button
            v-if="row.status === 'done' || row.status === 'failed'"
            size="small"
            theme="danger"
            variant="text"
            @click.stop="remove(row)"
          >
            删除
          </t-button>
        </div>
      </template>

      <template #expanded-row="{ row }">
        <div class="detail-grid">
          <span class="k">appid</span><span class="v">{{ row.appId || '-' }}</span>
          <span class="k">渠道</span>
          <span class="v">{{ row.channels && row.channels.length ? row.channels.join(' / ') : '通用包（未打渠道）' }}</span>
          <span class="k">SHA-256</span><span class="v">{{ row.sha256 || '-' }}</span>
          <span class="k">签名指纹</span><span class="v">{{ row.signature || '-' }}</span>
          <span class="k">签名主体</span><span class="v">{{ row.signerDN || '-' }}</span>
          <span class="k">appkey</span>
          <span class="v">{{ row.appKeySet ? '已设置' : '未设置（真机会报错）' }}</span>
          <span class="k">来源包</span><span class="v">{{ row.source || '-' }}</span>
        </div>
      </template>
    </t-table>

    <div v-if="!filtered.length" style="padding: 26px 0">
      <t-empty :description="state.jobs.length ? '没有匹配的记录' : '还没有打包记录'" />
    </div>

    <t-dialog
      v-model:visible="trashVisible"
      header="回收站 · 保留 7 天"
      width="620px"
      :footer="false"
    >
      <t-empty v-if="!state.trash.length" description="回收站是空的" />
      <div v-for="t in state.trash" :key="t.id" class="trash-row">
        <div style="min-width: 0">
          <div style="font-weight: 500">{{ t.appName || t.source || '-' }}</div>
          <div class="cell-sub">
            {{ t.versionName }} ({{ t.versionCode }}) · {{ t.package }}
          </div>
          <div class="cell-sub">删除于 {{ fmtTime(t.deletedAt) }}</div>
        </div>
        <t-space size="small">
          <t-button size="small" variant="outline" @click="restore(t)">恢复</t-button>
          <t-button size="small" theme="danger" variant="text" @click="purge(t)">彻底删除</t-button>
        </t-space>
      </div>
    </t-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import {
  state, loadJobs, loadTrash, retryJob, removeJob, restoreJob, purgeJob, watchJob,
} from '../store'
import { fmtBytes, fmtTime, friendlyError } from '../utils'
import { fastDownload } from '../download'

const kw = ref('')
const statusFilter = ref('')
const sortState = reactive({ sortBy: 'createdAt', descending: true })
const pagination = reactive({
  current: 1, pageSize: 5, total: 0, showJumper: true, pageSizeOptions: [5, 10, 20],
})
const expandedKeys = ref([])
const trashVisible = ref(false)

const statusOptions = [
  { label: '全部状态', value: '' },
  { label: '完成', value: 'done' },
  { label: '失败', value: 'failed' },
  { label: '构建中', value: 'running' },
  { label: '排队中', value: 'queued' },
]

const columns = [
  { colKey: 'appName', title: '应用', width: 270, ellipsis: false },
  { colKey: 'versionName', title: '版本 / 包名', width: 230 },
  { colKey: 'createdAt', title: '构建时间', width: 175, sorter: true, sortType: 'desc' },
  { colKey: 'apkSize', title: '体积 / 兼容', width: 135, align: 'right' },
  { colKey: 'ops', title: '操作', width: 186, align: 'right' },
]

const filtered = computed(() => {
  const q = kw.value.trim().toLowerCase()
  let list = state.jobs
  if (statusFilter.value) list = list.filter((j) => j.status === statusFilter.value)
  if (q) {
    list = list.filter((j) =>
      [j.appName, j.package, j.versionName, j.versionCode, j.apk, j.source]
        .some((v) => String(v || '').toLowerCase().includes(q)),
    )
  }
  const dir = sortState.descending ? -1 : 1
  return [...list].sort((a, b) => {
    const d = Date.parse(a.createdAt) - Date.parse(b.createdAt)
    return d === 0 ? 0 : d > 0 ? dir : -dir
  })
})

const pageData = computed(() => {
  const start = (pagination.current - 1) * pagination.pageSize
  return filtered.value.slice(start, start + pagination.pageSize)
})

watch(filtered, (v) => {
  pagination.total = v.length
  const maxPage = Math.max(1, Math.ceil(v.length / pagination.pageSize))
  if (pagination.current > maxPage) pagination.current = maxPage
}, { immediate: true })

watch([kw, statusFilter], () => { pagination.current = 1 })

function onPageChange(info) {
  pagination.current = info.current
  pagination.pageSize = info.pageSize
}

function onSortChange(sort) {
  sortState.sortBy = sort.sortBy
  sortState.descending = sort.descending
}

// 用整行底色表达状态，比一列小标签直观
function rowClassName({ row }) {
  return `row-${row.status || 'queued'}`
}

async function reload() {
  await Promise.all([loadJobs(), loadTrash()])
  MessagePlugin.success('已刷新')
}

// 渠道包列表（老记录没有 apks 字段，退回单个包）
function apkList(row) {
  if (row.apks && row.apks.length) return row.apks
  return row.apk ? [{ channel: '', file: row.apk, size: row.apkSize, exists: true }] : []
}

function apkOptions(row) {
  const list = apkList(row)
  const opts = list.map((a) => ({
    content: a.channel || '通用',
    value: a.file,
    disabled: !a.exists,
  }))
  if (list.length > 1) {
    opts.unshift({ content: `全部下载（zip）`, value: '__zip__' })
  }
  return opts
}

function downloadOne(row, file) {
  if (file === '__zip__') {
    window.open(`/api/jobs/${row.id}/zip`, '_blank')
    return
  }
  const a = apkList(row).find((x) => x.file === file)
  if (a) fastDownload(row.id, a.file, a.size)
}

async function download(row) {
  await fastDownload(row.id, row.apk, row.apkSize)
}

function retry(row) {
  retryJob(row.id)
}

function view(row) {
  watchJob(row.id)
  state.step = 2
}

function remove(row) {
  const dlg = DialogPlugin.confirm({
    header: '移入回收站',
    body: `「${row.appName || row.apk}」将移入回收站，7 天内可恢复。`,
    theme: 'warning',
    confirmBtn: '移入回收站',
    onConfirm: () => { dlg.destroy(); removeJob(row.id) },
    onCancel: () => dlg.destroy(),
  })
}

async function openTrash() {
  await loadTrash()
  trashVisible.value = true
}

function restore(t) {
  restoreJob(t.id)
}

function purge(t) {
  const dlg = DialogPlugin.confirm({
    header: '彻底删除',
    body: '删除后不可恢复，确定？',
    theme: 'danger',
    confirmBtn: '彻底删除',
    onConfirm: () => { dlg.destroy(); purgeJob(t.id) },
    onCancel: () => dlg.destroy(),
  })
}
</script>

<style scoped>
/* 整行底色：作用在 tr 上，td 透明，避免展开列出现白块 */
:deep(.t-table__body tr.row-done) { background: #eef9f1; }
:deep(.t-table__body tr.row-failed) { background: #fdeeee; }
:deep(.t-table__body tr.row-running) { background: #edf4ff; }
:deep(.t-table__body tr.row-queued) { background: #fff7ea; }

:deep(.t-table__body tr.row-done:hover) { background: #e2f4e8; }
:deep(.t-table__body tr.row-failed:hover) { background: #fbe2e2; }
:deep(.t-table__body tr.row-running:hover) { background: #e2ecff; }
:deep(.t-table__body tr.row-queued:hover) { background: #ffeed6; }

:deep(.t-table__body tr[class*="row-"] td) { background: transparent; }

/* 行首状态色条，弱化对颜色的单依赖 */
:deep(.t-table__body tr.row-done) td:first-child { box-shadow: inset 3px 0 0 #12a150; }
:deep(.t-table__body tr.row-failed) td:first-child { box-shadow: inset 3px 0 0 #d54941; }
:deep(.t-table__body tr.row-running) td:first-child { box-shadow: inset 3px 0 0 #0052d9; }
:deep(.t-table__body tr.row-queued) td:first-child { box-shadow: inset 3px 0 0 #e37318; }

.dim-note { font-size: 12px; color: #8a94a1; }
.ops-cell {
  display: flex; gap: 2px; justify-content: flex-end;
  align-items: center; white-space: nowrap;
}
.ops-cell :deep(.t-button) { padding-left: 6px; padding-right: 6px; }
.trash-row {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  padding: 10px 0; border-top: 1px solid #f0f1f3;
}
.trash-row:first-child { border-top: none; }
</style>
