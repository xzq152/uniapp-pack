<template>
  <div class="deploy">
    <div class="deploy-head">
      <div class="deploy-title">
        <t-icon name="download" size="18px" />
        <span>自己部署一套打包环境</span>
      </div>
      <div class="deploy-sub">
        不想用现成的服务？用这三个文件在本机（或任意一台装了 Docker 的机器）起一套打包环境，
        命令行打包，或者再跑一个网页版。
      </div>
    </div>

    <!-- 三步概览 -->
    <div class="steps">
      <div v-for="s in DEPLOY_STEPS" :key="s.n" class="step">
        <span class="step-n">{{ s.n }}</span>
        <div class="step-body">
          <div class="step-title">{{ s.title }}</div>
          <div class="step-desc">{{ s.body }}</div>
        </div>
      </div>
    </div>

    <!-- 文件标签页 -->
    <div class="files">
      <div class="tabs">
        <button
          v-for="f in DEPLOY_FILES"
          :key="f.key"
          class="tab"
          :class="{ active: active === f.key }"
          @click="active = f.key"
        >
          <code>{{ f.name }}</code>
        </button>
      </div>

      <div class="file-bar">
        <span class="file-desc">{{ current.desc }}</span>
        <span class="grow"></span>
        <t-button size="small" variant="text" @click="copyCurrent">
          {{ copied === current.key ? '已复制' : '复制' }}
        </t-button>
        <t-button size="small" variant="text" @click="downloadCurrent">下载</t-button>
      </div>

      <!-- README 是散文，长行折行更好读；yml / env 是配置，必须保持原样不折行 -->
      <pre class="code" :class="{ wrap: current.key === 'readme' }"><code>{{ current.content }}</code></pre>
    </div>

    <!-- 一键全下 -->
    <div class="deploy-foot">
      <t-button theme="primary" variant="outline" size="small" @click="downloadAll">
        下载全部（{{ DEPLOY_FILES.length }} 个文件）
      </t-button>
      <span class="foot-hint">
        下载后放进同一个目录，按 README 三步操作即可
      </span>
    </div>

    <div class="deploy-tip">
      <t-icon name="info-circle" size="13px" />
      <span>
        拿到 <code>.env.example</code> 后要<strong>改名为 <code>.env</code></strong> 再填参数——带
        <code>.example</code> 是为了不被 git 提交；Windows 上在资源管理器里改名会提示「需确认扩展名」，同意即可。
      </span>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { DEPLOY_FILES, DEPLOY_STEPS } from '../deployFiles'

const active = ref(DEPLOY_FILES[0].key)
const copied = ref('')

const current = computed(() => DEPLOY_FILES.find((f) => f.key === active.value) || DEPLOY_FILES[0])

async function copyCurrent() {
  const text = current.value.content
  try {
    // navigator.clipboard 在非 https / 非 localhost 下不可用，退回 execCommand
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
    } else {
      const ta = document.createElement('textarea')
      ta.value = text
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      ta.remove()
    }
    copied.value = current.value.key
    MessagePlugin.success(`已复制 ${current.value.name}`)
    setTimeout(() => { if (copied.value === current.value.key) copied.value = '' }, 2000)
  } catch (e) {
    MessagePlugin.error('复制失败，请手动选中内容复制')
  }
}

function saveText(name, text) {
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 5000)
}

function downloadCurrent() {
  saveText(current.value.name, current.value.content)
  MessagePlugin.success(`已下载 ${current.value.name}`)
}

// 全部下载：浏览器通常会拦掉短时间内的连续下载（只放行第一个），
// 所以先尝试打成一个 zip；失败再退化为逐个下载并明确告知用户。
async function downloadAll() {
  const ok = await tryZipDownload()
  if (ok) {
    MessagePlugin.success(`已下载 uniapp-pack-deploy.zip`)
    return
  }
  DEPLOY_FILES.forEach((f, i) => {
    setTimeout(() => saveText(f.name, f.content), i * 300)
  })
  MessagePlugin.info('浏览器不支持打包下载，已改为逐个下载；若被拦截请允许本页面的多个下载')
}

// tryZipDownload 用 zip.Store（不压缩）手写一个最小 zip，避免引入依赖。
// 返回 false 表示环境不支持某一步，调用方走降级路径。
async function tryZipDownload() {
  try {
    const enc = new TextEncoder()
    const chunks = []
    const central = []
    let offset = 0
    const now = new Date()
    const dosTime = ((now.getHours() << 11) | (now.getMinutes() << 5) | (now.getSeconds() >> 1)) & 0xffff
    const dosDate = (((now.getFullYear() - 1980) << 9) | ((now.getMonth() + 1) << 5) | now.getDate()) & 0xffff

    for (const f of DEPLOY_FILES) {
      const nameBytes = enc.encode(f.name)
      const data = enc.encode(f.content)
      const crc = crc32(data)
      const local = new DataView(new ArrayBuffer(30))
      local.setUint32(0, 0x04034b50, true)   // 本地文件头签名
      local.setUint16(4, 20, true)           // 版本
      local.setUint16(6, 0x0800, true)       // flag: UTF-8 文件名
      local.setUint16(8, 0, true)            // Store，不压缩
      local.setUint16(10, dosTime, true)
      local.setUint16(12, dosDate, true)
      local.setUint32(14, crc, true)
      local.setUint32(18, data.length, true)
      local.setUint32(22, data.length, true)
      local.setUint16(26, nameBytes.length, true)
      local.setUint16(28, 0, true)
      chunks.push(new Uint8Array(local.buffer), nameBytes, data)

      const cd = new DataView(new ArrayBuffer(46))
      cd.setUint32(0, 0x02014b50, true)      // 中央目录签名
      cd.setUint16(4, 20, true)
      cd.setUint16(6, 20, true)
      cd.setUint16(8, 0x0800, true)
      cd.setUint16(10, 0, true)
      cd.setUint16(12, dosTime, true)
      cd.setUint16(14, dosDate, true)
      cd.setUint32(16, crc, true)
      cd.setUint32(20, data.length, true)
      cd.setUint32(24, data.length, true)
      cd.setUint16(28, nameBytes.length, true)
      cd.setUint32(42, offset, true)
      central.push(new Uint8Array(cd.buffer), nameBytes)

      offset += 30 + nameBytes.length + data.length
    }

    const cdSize = central.reduce((n, c) => n + c.length, 0)
    const end = new DataView(new ArrayBuffer(22))
    end.setUint32(0, 0x06054b50, true)       // 中央目录结束签名
    end.setUint16(8, DEPLOY_FILES.length, true)
    end.setUint16(10, DEPLOY_FILES.length, true)
    end.setUint32(12, cdSize, true)
    end.setUint32(16, offset, true)

    const parts = [...chunks, ...central, new Uint8Array(end.buffer)]
    const blob = new Blob(parts, { type: 'application/zip' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'uniapp-pack-deploy.zip'
    document.body.appendChild(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 5000)
    return true
  } catch (e) {
    return false
  }
}

// crc32 —— zip 头必须带校验值，标准查表实现
const CRC_TABLE = (() => {
  const t = new Uint32Array(256)
  for (let i = 0; i < 256; i++) {
    let c = i
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    t[i] = c >>> 0
  }
  return t
})()

function crc32(bytes) {
  let c = 0xffffffff
  for (let i = 0; i < bytes.length; i++) c = CRC_TABLE[(c ^ bytes[i]) & 0xff] ^ (c >>> 8)
  return (c ^ 0xffffffff) >>> 0
}
</script>

<style scoped>
.deploy { display: flex; flex-direction: column; gap: 16px; }
.deploy-head { display: flex; flex-direction: column; gap: 6px; }
.deploy-title {
  display: flex; align-items: center; gap: 8px;
  font-size: 14px; font-weight: 600; color: var(--text-1);
}
.deploy-sub { font-size: 12.5px; color: var(--text-3); line-height: 1.7; }

/* 三步 */
.steps { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; }
.step {
  display: flex; gap: 10px;
  padding: 12px 14px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: #fafbfc;
}
.step-n {
  flex: none;
  width: 20px; height: 20px; border-radius: 50%;
  background: #0052d9; color: #fff;
  font-size: 11.5px; font-weight: 600;
  display: flex; align-items: center; justify-content: center;
}
.step-body { min-width: 0; }
.step-title { font-size: 12.5px; font-weight: 600; margin-bottom: 3px; }
.step-desc { font-size: 11.5px; color: var(--text-3); line-height: 1.6; }

/* 文件区 */
.files {
  border: 1px solid var(--line);
  border-radius: 8px;
  overflow: hidden;
}
.tabs {
  display: flex; gap: 2px;
  padding: 6px 8px 0;
  background: #fafbfc;
  border-bottom: 1px solid var(--line);
}
.tab {
  border: none; background: transparent;
  padding: 7px 12px; cursor: pointer;
  border-radius: 6px 6px 0 0;
  font-family: inherit;
  transition: background .15s, color .15s;
}
.tab code {
  font-family: ui-monospace, Consolas, monospace;
  font-size: 12px; color: var(--text-2);
}
.tab:hover { background: #f0f2f5; }
.tab.active { background: #fff; box-shadow: 0 -1px 0 #0052d9 inset; }
.tab.active code { color: #0052d9; font-weight: 600; }

.file-bar {
  display: flex; align-items: center; gap: 6px;
  padding: 7px 12px;
  border-bottom: 1px solid #f0f1f3;
}
.file-desc { font-size: 12px; color: var(--text-3); }
.grow { flex: 1; }

.code {
  margin: 0;
  max-height: 340px;
  overflow: auto;
  background: #1b1f24;
  color: #d7dde5;
  padding: 14px 16px;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 11.5px;
  line-height: 1.7;
  white-space: pre;
}
/* 长行需要横向滚动，给个常驻的细滚动条，别让人以为内容被截断了 */
.code::-webkit-scrollbar { width: 9px; height: 9px; }
.code::-webkit-scrollbar-thumb {
  background: #3d4650; border-radius: 5px;
}
.code::-webkit-scrollbar-thumb:hover { background: #525d68; }
.code::-webkit-scrollbar-track { background: transparent; }
.code::-webkit-scrollbar-corner { background: transparent; }

/* Markdown 允许折行（中文长句多，横向拖动读起来很累）；
   yml / env 要保留缩进与对齐，不能折行 */
.code.wrap { white-space: pre-wrap; word-break: break-word; }

.deploy-foot { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.foot-hint { font-size: 11.5px; color: var(--text-3); }

/* .env 改名提醒：这类"拿不到就跑不起来"的步骤值得单独强调 */
.deploy-tip {
  display: flex; align-items: flex-start; gap: 7px;
  padding: 9px 12px;
  background: #fffbeb;
  border-left: 2px solid #f59e0b;
  border-radius: 4px;
  font-size: 12px;
  color: #92400e;
  line-height: 1.7;
}
.deploy-tip code {
  font-family: ui-monospace, Consolas, monospace;
  background: #fef3c7; padding: 1px 4px; border-radius: 3px;
}

@media (max-width: 760px) {
  .steps { grid-template-columns: 1fr; }
  .tabs { overflow-x: auto; }
}
</style>
