import { reactive, computed } from 'vue'
import { api, uploadFile, uploadBundle } from './api'
import { MessagePlugin } from 'tdesign-vue-next'

export const state = reactive({
  booted: false,
  needLogin: false,

  env: { image: '', dockerMem: '', auth: false, keepJobs: 20, keepApks: 5 },
  runtime: { dockerOk: false, imageOk: false, running: false, message: '', image: '', dockerMem: '' },

  step: 0,
  upload: { id: '', name: '', size: 0, progress: 0, speed: 0, busy: false, iconHint: '', consumed: false },
  icon: { id: '', name: '' },
  splash: { id: '', name: '', file: null },
  keystores: [],

  form: {
    appId: '',
    appName: '',
    packageName: '',
    versionName: '',
    versionCode: '',
    appKey: '',
    keystore: 'auto',
    keystoreId: '',
    channels: [],
  },

  detecting: true,
  logOpen: false,
  refillOpen: false,
  jobs: [],
  trash: [],
  job: { id: '', status: '', progress: 0, stage: '', error: '', logs: [] },
  watching: false,
})

let es = null
let timersStarted = false

// ---------------------------------------------------------------- 环境
export const runtimeOn = computed(() => !!(state.runtime.dockerOk && state.runtime.running))

export async function loadEnv() {
  try {
    const d = await api.env()
    state.env = d
    state.needLogin = !!d.auth && d.needLogin
  } catch (e) {
    if (e.status === 401) state.needLogin = true
  }
}

export async function loadRuntime() {
  try {
    const d = await api.runtime()
    state.runtime = d
    state.env.image = d.image || state.env.image
    state.env.dockerMem = d.dockerMem || state.env.dockerMem
  } catch (e) {
    if (e.status === 401) state.needLogin = true
    else state.runtime = { ...state.runtime, dockerOk: false, message: '无法连接服务' }
  } finally {
    state.detecting = false
  }
}

export async function runtimeAction(action) {
  try {
    const d = await api.runtimeAction(action)
    state.runtime = d
    MessagePlugin.success({ start: '打包环境已启动', stop: '打包环境已停止', restart: '环境已重建' }[action] || '完成')
  } catch (e) {
    MessagePlugin.error(String(e.message))
  }
}

export async function login(password) {
  await api.login(password)
  state.needLogin = false
  await boot()
}

export async function logout() {
  await api.logout()
  state.needLogin = true
}

// ---------------------------------------------------------------- 上传
export async function upload(file) {
  if (!runtimeOn.value) {
    MessagePlugin.warning('请先启动打包环境')
    return
  }
  state.upload.busy = true
  state.upload.name = file.name
  state.upload.progress = 0
  const t0 = performance.now()
  try {
    const d = await uploadFile(file, (p, loaded) => {
      state.upload.progress = Math.round(p * 100)
      const sec = (performance.now() - t0) / 1000
      state.upload.speed = sec > 0.2 ? loaded / sec : 0
      state.upload.size = loaded
    })
    state.upload.id = d.uploadId
    state.upload.size = d.size
    state.upload.iconHint = d.iconHint || ''
    state.upload.consumed = false

    const m = d.manifest || {}
    if (m.name) state.form.appName = m.name
    if (m.appId) state.form.appId = m.appId
    if (m.versionName) state.form.versionName = m.versionName
    if (m.versionCode) state.form.versionCode = m.versionCode
    suggestVersion()
    await fillAppKey()
    if (state.step === 0) state.step = 1
    MessagePlugin.success('资源包上传完成')
  } catch (e) {
    MessagePlugin.error('上传失败：' + e.message)
    state.upload.id = ''
  } finally {
    state.upload.busy = false
  }
}

export async function uploadFolder(files, rootName) {
  if (!runtimeOn.value) {
    MessagePlugin.warning('请先启动打包环境')
    return
  }
  if (!files || !files.length) return
  state.upload.busy = true
  state.upload.name = (rootName || '文件夹') + '/'
  state.upload.progress = 0
  const t0 = performance.now()
  try {
    const d = await uploadBundle(files, rootName, (p, loaded) => {
      state.upload.progress = Math.round(p * 100)
      const sec = (performance.now() - t0) / 1000
      state.upload.speed = sec > 0.2 ? loaded / sec : 0
      state.upload.size = loaded
    })
    state.upload.id = d.uploadId
    state.upload.size = d.size
    state.upload.files = d.files
    state.upload.name = d.name
    state.upload.iconHint = d.iconHint || ''

    const m = d.manifest || {}
    if (m.name) state.form.appName = m.name
    if (m.appId) state.form.appId = m.appId
    if (m.versionName) state.form.versionName = m.versionName
    if (m.versionCode) state.form.versionCode = m.versionCode
    suggestVersion()
    await fillAppKey()
    if (state.step === 0) state.step = 1
    MessagePlugin.success(`文件夹上传完成（${d.files} 个文件）`)
  } catch (e) {
    MessagePlugin.error('上传失败：' + e.message)
    state.upload.id = ''
  } finally {
    state.upload.busy = false
  }
}

async function fillAppKey() {
  if (!state.form.appId || state.form.appKey) return
  try {
    const d = await api.appkey(state.form.appId)
    if (d.appKey && !state.form.appKey) state.form.appKey = d.appKey
  } catch (e) { /* 忽略 */ }
}

export function suggestVersion() {
  const last = lastByPackage(state.form.packageName)
  if (!last) return
  if (!state.form.versionName) state.form.versionName = bumpName(last.versionName)
  if (!state.form.versionCode) state.form.versionCode = String((parseInt(last.versionCode, 10) || 0) + 1)
}

export function bumpVersion() {
  const last = lastByPackage(state.form.packageName)
  if (!last) {
    MessagePlugin.warning('没有找到同包名的历史记录')
    return
  }
  state.form.versionName = bumpName(last.versionName)
  state.form.versionCode = String((parseInt(last.versionCode, 10) || 0) + 1)
  MessagePlugin.success(`已按上次 +1：${state.form.versionName} (${state.form.versionCode})`)
}

export function bumpName(v) {
  const parts = String(v || '1.0.0').split('.')
  const n = parseInt(parts[parts.length - 1], 10)
  parts[parts.length - 1] = String(isNaN(n) ? 1 : n + 1)
  return parts.join('.')
}

export function lastByPackage(pkg) {
  if (!pkg) return null
  const hits = state.jobs.filter((j) => j.package === pkg && j.status === 'done')
  hits.sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt))
  return hits[0] || null
}

// ---------------------------------------------------------------- 证书 / 图标
export async function loadKeystores() {
  try {
    state.keystores = await api.keystores()
  } catch (e) { /* 忽略 */ }
}

export async function addKeystore(file, meta) {
  const d = await api.putKeystore(file, meta)
  await loadKeystores()
  return d
}

export function pickIcon(file) {
  state.icon.name = file ? file.name : ''
  state.icon.file = file || null
}

export function pickSplash(file) {
  state.splash.name = file ? file.name : ''
  state.splash.file = file || null
}

// ---------------------------------------------------------------- 提交
export async function submit() {
  if (!runtimeOn.value) {
    MessagePlugin.warning('请先启动打包环境')
    return
  }

  // 版本留空时按同包名历史推一个，避免打出 1.0.0(1)
  if (!state.form.versionName.trim() || !state.form.versionCode.trim()) {
    const last = lastByPackage(state.form.packageName.trim())
    if (!state.form.versionName.trim()) {
      state.form.versionName = last ? bumpName(last.versionName) : '1.0.0'
    }
    if (!state.form.versionCode.trim()) {
      state.form.versionCode = String(last ? (parseInt(last.versionCode, 10) || 0) + 1 : 1)
    }
  }
  try {
    let keystore = 'auto'
    if (state.form.keystore === 'saved') keystore = state.form.keystoreId
    if (state.form.keystore === 'new') {
      const f = state.form.newKeystore?.file
      const meta = {
        name: state.form.newKeystore?.name || (f ? f.name : '证书'),
        alias: state.form.newKeystore?.alias || '',
        storePass: state.form.newKeystore?.storePass || '',
        keyPass: state.form.newKeystore?.keyPass || '',
      }
      const ks = await addKeystore(f, meta)
      keystore = ks.id
      state.form.keystore = 'saved'
      state.form.keystoreId = ks.id
    }

    let iconId = ''
    if (state.icon.file) {
      iconId = (await api.icon(state.icon.file)).id
    }

    let splashId = ''
    if (state.splash.file) {
      splashId = (await api.splash(state.splash.file, state.splash.name)).id
    }

    const body = {
      uploadId: state.upload.id,
      appId: state.form.appId.trim(),
      appName: state.form.appName.trim(),
      package: state.form.packageName.trim(),
      versionName: state.form.versionName.trim(),
      versionCode: state.form.versionCode.trim(),
      appKey: state.form.appKey.trim(),
      keystore,
      icon: iconId,
      splash: splashId,
      channels: state.form.channels || [],
    }
    const d = await api.build(body)
    watchJob(d.id)
    state.step = 2
  } catch (e) {
    MessagePlugin.error('提交失败：' + e.message)
  }
}

// ---------------------------------------------------------------- 任务
export function watchJob(id) {
  if (es) es.close()
  state.job = { id, status: 'queued', progress: 0, stage: '排队中', error: '', logs: [] }
  state.watching = true
  es = new EventSource('/api/jobs/' + id + '/events')

  es.addEventListener('snapshot', (ev) => {
    const d = JSON.parse(ev.data)
    state.job.logs = d.logs || []
    applyState(d)
  })
  es.addEventListener('log', (ev) => {
    state.job.logs.push(JSON.parse(ev.data).line)
    if (state.job.logs.length > 1500) state.job.logs.splice(0, 500)
  })
  es.addEventListener('state', (ev) => {
    applyState(JSON.parse(ev.data))
  })
  es.onerror = () => { /* 浏览器自动重连 */ }
}

function applyState(d) {
  if (d.status) state.job.status = d.status
  if (typeof d.progress === 'number') state.job.progress = d.progress
  if (d.stage) state.job.stage = d.stage
  if (d.error) state.job.error = d.error
  if (d.status === 'done') state.upload.consumed = true

  if (d.status === 'done' || d.status === 'failed') {
    state.watching = false
    if (es) { es.close(); es = null }
    state.step = 3
    loadJobs()
    if (d.status === 'done') MessagePlugin.success('打包完成')
    else MessagePlugin.error('打包失败，可查看日志或一键重试')
  }
}

export async function loadJobs() {
  try {
    state.jobs = await api.jobs()
  } catch (e) { /* 忽略 */ }
}

export async function retryJob(id) {
  try {
    const d = await api.retry(id)
    watchJob(d.id)
    state.step = 2
    MessagePlugin.success('已重新提交')
  } catch (e) {
    MessagePlugin.error('重试失败：' + e.message)
  }
}

export async function removeJob(id) {
  try {
    await api.remove(id)
    await Promise.all([loadJobs(), loadTrash()])
    MessagePlugin.success('已移入回收站')
  } catch (e) {
    MessagePlugin.error('删除失败：' + e.message)
  }
}

export async function loadTrash() {
  try {
    state.trash = await api.trash()
  } catch (e) { /* 忽略 */ }
}

export async function restoreJob(id) {
  try {
    await api.restore(id)
    await Promise.all([loadJobs(), loadTrash()])
    MessagePlugin.success('已恢复')
  } catch (e) {
    MessagePlugin.error(String(e.message))
  }
}

export async function purgeJob(id) {
  try {
    await api.purge(id)
    await loadTrash()
    MessagePlugin.success('已彻底删除')
  } catch (e) {
    MessagePlugin.error(String(e.message))
  }
}

// ---------------------------------------------------------------- 回填 / 重置
// 从历史记录回填「填写过的信息」，不动当前已上传的资源包
export function fillFrom(job) {
  state.form.appId = job.appId || ''
  state.form.appName = job.appName || ''
  state.form.packageName = job.package || ''
  state.form.versionName = job.versionName || ''
  state.form.versionCode = job.versionCode || ''
  if (job.appKey) state.form.appKey = job.appKey
  state.form.channels = Array.isArray(job.channels) ? [...job.channels] : []
  if (job.keystore && job.keystore !== 'auto') {
    state.form.keystore = 'saved'
    state.form.keystoreId = job.keystore
  } else {
    state.form.keystore = 'auto'
    state.form.keystoreId = ''
  }
  state.refillOpen = false
  MessagePlugin.success(`已回填 ${job.appName || job.package} 的打包信息`)
}

export function resetForm() {
  state.upload = { id: '', name: '', size: 0, progress: 0, speed: 0, busy: false, iconHint: '', consumed: false, files: 0 }
  state.icon = { id: '', name: '', file: null }
  state.splash = { id: '', name: '', file: null }
  state.form = {
    appId: '', appName: '', packageName: '', versionName: '', versionCode: '',
    appKey: state.form.appKey, keystore: 'auto', keystoreId: '', channels: [],
  }
  state.job = { id: '', status: '', progress: 0, stage: '', error: '', logs: [] }
  state.step = 0
}

// ---------------------------------------------------------------- 启动
export async function boot() {
  await Promise.all([loadEnv(), loadRuntime(), loadKeystores(), loadJobs(), loadTrash()])
  state.booted = true

  // 支持 ?job=<id> 直接打开某次构建（方便把链接发给同事看进度）
  const wanted = new URLSearchParams(location.search).get('job')
  if (wanted) {
    watchJob(wanted)
    state.step = 2
  }
  if (!timersStarted) {
    timersStarted = true
    setInterval(() => { loadRuntime() }, 6000)
    setInterval(() => { if (!state.watching) loadJobs() }, 8000)
  }
}
