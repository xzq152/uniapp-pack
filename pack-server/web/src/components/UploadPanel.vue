<template>
  <div>
    <!-- 未上传：拖拽区 -->
    <div
      v-if="!state.upload.id || state.upload.busy"
      class="dropzone"
      :class="{ over: dragging }"
      @dragover.prevent="dragging = true"
      @dragleave.prevent="dragging = false"
      @drop.prevent="onDrop"
    >
      <t-icon name="cloud-upload" size="26px" style="color: #0052d9" />
      <div class="big">{{ state.upload.busy ? '正在上传…' : '把 zip 拖到这里，或选择上传方式' }}</div>
      <div class="sub">
        zip：HBuilderX 生成的 __UNI__XXXX.zip　|　文件夹：HBuilderX 导出的本地打包资源目录
      </div>

      <div v-if="state.upload.busy" style="margin-top: 12px">
        <t-progress :percentage="state.upload.progress" :label="state.upload.progress + '%'" />
        <div class="cell-sub" style="margin-top: 4px">
          {{ fmtBytes(state.upload.size) }} · {{ (state.upload.speed / 1024 / 1024).toFixed(1) }} MB/s
        </div>
      </div>

      <div class="pick-row" v-else>
        <t-button theme="primary" variant="outline" @click.stop="pickZip">选择 zip 文件</t-button>
        <t-button variant="outline" @click.stop="pickFolder">选择文件夹</t-button>
      </div>
    </div>

    <!-- 已上传：紧凑文件条 -->
    <div v-else class="file-card">
      <t-icon :name="state.upload.files ? 'folder-open' : 'file-zip'" size="20px" style="color: #0052d9" />
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
      <t-button size="small" variant="outline" @click="state.refillOpen = true">回填信息</t-button>
    </div>

    <input ref="zipInput" type="file" accept=".zip" data-upload="zip" hidden @change="onZip" />
    <input ref="folderInput" type="file" webkitdirectory hidden @change="onFolder" />
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { state, upload, uploadFolder } from '../store'
import { fmtBytes } from '../utils'

const zipInput = ref(null)
const folderInput = ref(null)
const dragging = ref(false)

function pickZip() {
  zipInput.value?.click()
}
function pickFolder() {
  folderInput.value?.click()
}
function reUpload() {
  // 重新选择前清掉旧的上传态
  state.upload.id = ''
  state.upload.consumed = false
  pickZip()
}

function onZip(e) {
  const f = e.target.files?.[0]
  if (f) upload(f)
  e.target.value = ''
}

function onFolder(e) {
  const files = [...(e.target.files || [])]
  e.target.value = ''
  if (!files.length) return
  const root = (files[0].webkitRelativePath || files[0].name).split('/')[0] || 'folder'
  uploadFolder(files, root)
}

function onDrop(e) {
  dragging.value = false
  const f = e.dataTransfer.files?.[0]
  if (f) upload(f)
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
