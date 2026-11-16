<template>
  <div>
    <!-- 未上传：拖拽区（只接受文件夹） -->
    <div
      v-if="!state.upload.id || state.upload.busy"
      class="dropzone"
      :class="{ over: dragging }"
      @dragover.prevent="dragging = true"
      @dragleave.prevent="dragging = false"
      @drop.prevent="onDrop"
    >
      <t-icon name="cloud-upload" size="26px" style="color: #0052d9" />
      <div class="big">{{ state.upload.busy ? '正在上传…' : '把资源文件夹拖到这里，或选择文件夹' }}</div>
      <div class="sub">
        HBuilderX「发行 → 原生App-本地打包 → 生成本地打包App资源」导出的目录（含 __UNI__XXXX）
      </div>

      <div v-if="state.upload.busy" style="margin-top: 12px">
        <t-progress :percentage="state.upload.progress" :label="state.upload.progress + '%'" />
        <div class="cell-sub" style="margin-top: 4px">
          {{ fmtBytes(state.upload.size) }} · {{ (state.upload.speed / 1024 / 1024).toFixed(1) }} MB/s
        </div>
      </div>

      <div class="pick-row" v-else>
        <t-button theme="primary" variant="outline" @click.stop="pickFolder">选择文件夹</t-button>
      </div>
    </div>

    <!-- 已上传：紧凑文件条 -->
    <div v-else class="file-card">
      <t-icon name="folder-open" size="20px" style="color: #0052d9" />
      <div style="min-width: 0">
        <div class="fc-name">{{ state.upload.name }}</div>
        <div class="cell-sub">
          {{ fmtBytes(state.upload.size) }}
          <template v-if="state.upload.files"> · {{ state.upload.files }} 个文件</template>
          <template v-if="state.upload.iconHint"> · 含图标 {{ state.upload.iconHint }}</template>
        </div>
      </div>
      <span style="flex: 1"></span>
      <t-tag v-if="state.upload.consumed" size="small" theme="warning" variant="light">原包已清理</t-tag>
      <t-tag v-else size="small" theme="success" variant="light">已上传</t-tag>
      <t-button size="small" variant="text" @click="reUpload">重新选择</t-button>
    </div>

    <input ref="folderInput" type="file" webkitdirectory hidden @change="onFolder" />
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { state, uploadFolder } from '../store'
import { fmtBytes } from '../utils'

const folderInput = ref(null)
const dragging = ref(false)

function pickFolder() {
  folderInput.value?.click()
}

function reUpload() {
  // 重新选择前清掉旧的上传态
  state.upload.id = ''
  state.upload.consumed = false
  pickFolder()
}

function onFolder(e) {
  const files = [...(e.target.files || [])]
  e.target.value = ''
  if (!files.length) return
  const root = (files[0].webkitRelativePath || files[0].name).split('/')[0] || 'folder'
  uploadFolder(files, root)
}

// ---- 拖拽文件夹 ----
// webkitGetAsEntry 必须在 drop 事件同步阶段调用，await 之后就失效了
async function readAll(reader) {
  const all = []
  // readEntries 一次最多返回 100 条，要循环读到空
  for (;;) {
    const batch = await new Promise((res, rej) => reader.readEntries(res, rej))
    if (!batch.length) break
    all.push(...batch)
  }
  return all
}

async function collect(entry, prefix, out) {
  if (entry.isFile) {
    const file = await new Promise((res, rej) => entry.file(res, rej))
    try {
      // 补上相对路径，和 input[webkitdirectory] 的行为保持一致
      Object.defineProperty(file, 'webkitRelativePath', {
        value: prefix + file.name,
        configurable: true,
      })
    } catch (e) { /* 只读就算了，靠文件名兜底 */ }
    out.push(file)
    return
  }
  const children = await readAll(entry.createReader())
  for (const child of children) await collect(child, prefix + entry.name + '/', out)
}

async function onDrop(e) {
  dragging.value = false
  const entries = [...(e.dataTransfer.items || [])]
    .map((it) => (it.webkitGetAsEntry ? it.webkitGetAsEntry() : null))
    .filter(Boolean)

  const dirs = entries.filter((en) => en.isDirectory)
  if (!dirs.length) {
    MessagePlugin.warning('请拖入文件夹（HBuilderX 导出的本地打包资源目录）')
    return
  }
  if (dirs.length > 1) {
    MessagePlugin.warning('一次只能拖一个文件夹')
    return
  }

  const out = []
  try {
    await collect(dirs[0], '', out)
  } catch (err) {
    MessagePlugin.error('读取文件夹失败：' + (err?.message || err))
    return
  }
  if (!out.length) {
    MessagePlugin.warning('文件夹是空的')
    return
  }
  uploadFolder(out, dirs[0].name)
}
</script>

<style scoped>
.pick-row { display: flex; gap: 10px; justify-content: center; margin-top: 14px; }
.file-card {
  display: flex; align-items: center; gap: 10px;
  padding: 12px 14px;
  border: 1px solid #d9eadf;
  background: #f6fbf8;
  border-radius: 6px;
}
.fc-name { font-size: 13.5px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
