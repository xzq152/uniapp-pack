import { reactive, computed } from 'vue'
import { api, uploadBundle } from './api'
import { MessagePlugin } from 'tdesign-vue-next'

export const state = reactive({
  booted: false,
  needLogin: false,

  env: { image: '', dockerMem: '', auth: false, keepJobs: 20, keepApks: 5 },
  runtime: { dockerOk: false, imageOk: false, running: false, message: '', image: '', dockerMem: '', dockerHost: '' },

  // Docker 端点：当前生效地址 / 是否本机 / 页面里正在编辑的草稿
  docker: { host: '', raw: '', envHost: '', local: true, loaded: false },

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

  // 安卓原生配置：SDK 版本 + 权限
  android: {
    minSdk: 21,
    targetSdk: 33,
    compileSdk: 35,
    defMinSdk: 21,
    defTargetSdk: 33,
    sdkVersion: '',
    baseline: [],   // [{name,label}] SDK 模板自带、可取消勾选的权限
    catalog: [],    // [{name,label,note,minSdk}] 可额外添加的权限
    moduleHints: {}, // uni-app 模块 → 建议权限
    loaded: false,
  },
  perm: {
    keep: [],  // 勾选保留的基线权限名
    add: [],   // 额外添加的权限名
  },
  manifestName: '', // 上传的项目 manifest.json 文件名（仅提示用）
  permSuggested: false, // 本次是否有「按模块推测」的权限

  detecting: true,
  logOpen: false,
  dockerDialogOpen: false,
  jobs: [],   // 仅用于取当前任务的产物信息（APK 名/渠道列表），不再渲染记录列表
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
    // 轮询也要同步端点：别人（另一个浏览器）切换过地址时，这里的标签才不会说错
    if (d.dockerHost) {
      state.docker.host = d.dockerHost
      state.docker.local = /^local/i.test(d.dockerHost)
    }
  } catch (e) {
    if (e.status === 401) state.needLogin = true
    else state.runtime = { ...state.runtime, dockerOk: false, message: '无法连接服务' }
  } finally {
    state.detecting = false
  }
}

// ---------------------------------------------------------------- Docker 端点
export async function loadDockerHost() {
  try {
    const d = await api.dockerHost()
    state.docker = { ...state.docker, ...d, loaded: true }
    if (d.host) state.runtime.dockerHost = d.host
  } catch (e) { /* 忽略，沿用 runtime 里的地址 */ }
}

// testDockerHost 只试连，不切换，返回探测结果
export async function testDockerHost(host) {
  try {
    return await api.dockerTest(host)
  } catch (e) {
    return { ok: false, host, message: e.message }
  }
}

// setDockerHost 切换端点；成功后重新拉一次运行时状态
export async function setDockerHost(host) {
  try {
    const d = await api.dockerSetHost(host)
    state.docker = { ...state.docker, host: d.host, raw: d.raw, local: !d.raw, loaded: true }
    if (d.runtime) state.runtime = d.runtime
    await Promise.all([loadRuntime(), loadJobs()])
    MessagePlugin.success('已切换到 ' + d.host)
    return true
  } catch (e) {
    MessagePlugin.error(e.message)
    return false
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
// 只支持文件夹：HBuilderX「生成本地打包App资源」导出的目录
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
    applyAndroidConfig(m)
    fillPackageFromHistory(m.appId)
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

// ---------------------------------------------------------------- 安卓配置
export async function loadAndroidDefaults() {
  try {
    const d = await api.androidDefaults()
    state.android.defMinSdk = d.minSdk || 21
    state.android.defTargetSdk = d.targetSdk || 33
    state.android.minSdk = d.minSdk || 21
    state.android.targetSdk = d.targetSdk || 33
    state.android.compileSdk = d.compileSdk || 35
    state.android.sdkVersion = d.sdkVersion || ''
    state.android.baseline = d.baseline || []
    state.android.catalog = d.catalog || []
    state.android.moduleHints = d.moduleHints || {}
    // 首次加载：基线权限全部保留
    if (!state.perm.keep.length) {
      state.perm.keep = state.android.baseline.map((p) => p.name)
    }
    state.android.loaded = true
  } catch (e) { /* 忽略，用内置默认值 */ }
}

const BASE_NAMES = () => state.android.baseline.map((p) => p.name)

// 缺了这些权限，应用基本不可用（断网/读不到设备），任何情况下都强制保留
export const CRITICAL_PERMS = [
  'INTERNET',
  'ACCESS_NETWORK_STATE',
  'WRITE_EXTERNAL_STORAGE',
  'READ_PHONE_STATE',
]

// applyAndroidConfig 用 manifest.json 里的信息回填 SDK 版本 / 包名 / 权限
function applyAndroidConfig(m) {
  if (!m) return
  if (m.packageName && !state.form.packageName) state.form.packageName = m.packageName
  if (m.minSdk) state.android.minSdk = m.minSdk
  if (m.targetSdk) state.android.targetSdk = m.targetSdk

  const base = BASE_NAMES()
  if (Array.isArray(m.permissions) && m.permissions.length) {
    // 注意：manifest 里的 distribute.android.permissions 是「云打包要追加的」，
    // 不是「APK 权限的完整清单」。离线打包的基线权限来自 SDK aar，本来就存在，
    // 不能因为清单里没写就删掉（例如没写 READ_MEDIA_IMAGES 不代表不要存相册，
    // 删了会导致 Android 13+ 保存图片失败）。所以这里只加不减。
    state.perm.keep = [...base]
    state.perm.add = m.permissions.filter((n) => !base.includes(n))
    state.permSuggested = false
    return
  }
  // 打包资源的 manifest 被裁剪过，没有 permissions：按 uni-app 模块推测一遍
  if (Array.isArray(m.modules) && m.modules.length) {
    const want = new Set()
    for (const mod of m.modules) {
      for (const p of state.android.moduleHints[mod] || []) want.add(p)
    }
    if (want.size) {
      state.perm.keep = [...base]
      state.perm.add = [...want].filter((n) => !base.includes(n))
      state.permSuggested = true
    }
  }
}

// ---------------------------------------------------------------- JSONC 容错
// HBuilderX 的 manifest.json 允许写注释（// 和 /* */）和尾随逗号，
// 本质是 JSONC，浏览器标准 JSON.parse 会直接失败。这里做一次降级解析。
function stripJsonComments(s) {
  let out = ''
  let i = 0
  let inStr = false
  while (i < s.length) {
    const c = s[i]
    if (inStr) {
      out += c
      if (c === '\\') {
        out += s[i + 1] || ''
        i += 2
        continue
      }
      if (c === '"') inStr = false
      i++
      continue
    }
    if (c === '"') {
      inStr = true
      out += c
      i++
      continue
    }
    if (c === '/' && s[i + 1] === '/') {
      while (i < s.length && s[i] !== '\n') i++
      continue
    }
    if (c === '/' && s[i + 1] === '*') {
      i += 2
      while (i < s.length && !(s[i] === '*' && s[i + 1] === '/')) i++
      i += 2
      continue
    }
    out += c
    i++
  }
  return out
}

function stripTrailingCommas(s) {
  let out = ''
  let i = 0
  let inStr = false
  while (i < s.length) {
    const c = s[i]
    if (inStr) {
      out += c
      if (c === '\\') {
        out += s[i + 1] || ''
        i += 2
        continue
      }
      if (c === '"') inStr = false
      i++
      continue
    }
    if (c === '"') {
      inStr = true
      out += c
      i++
      continue
    }
    if (c === ',') {
      let j = i + 1
      while (j < s.length && /\s/.test(s[j])) j++
      if (s[j] === '}' || s[j] === ']') {
        i++
        continue
      }
    }
    out += c
    i++
  }
  return out
}

// parseManifestText 先按标准 JSON 试，失败再按 JSONC 降级
function parseManifestText(raw) {
  const text = String(raw).replace(/^\uFEFF/, '')
  try {
    return { ok: true, data: JSON.parse(text) }
  } catch (e1) {
    try {
      return { ok: true, data: JSON.parse(stripTrailingCommas(stripJsonComments(text))) }
    } catch (e2) {
      return { ok: false, error: e2.message }
    }
  }
}

// 包名没填时，用同 appid 的历史记录补上：
// HBuilderX 本地 manifest 通常没有 packagename（包名在云端配置里），
// 但同一个 appid 的历史构建记录里一定有，带出来省得每次手填。
export function fillPackageFromHistory(appId) {
  if (state.form.packageName || !appId) return
  const hits = state.jobs.filter((j) => j.appId === appId && j.package)
  if (!hits.length) return
  hits.sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt))
  state.form.packageName = hits[0].package
}

// uploadManifestJson 解析 HBuilderX 项目根目录的 manifest.json（带 app-plus.distribute）
export async function uploadManifestJson(file) {
  if (!file) return
  let text = ''
  try {
    text = await file.text()
  } catch (e) {
    MessagePlugin.error('读取文件失败')
    return
  }
  const r = parseManifestText(text)
  if (!r.ok) {
    MessagePlugin.error('解析失败：' + r.error)
    return
  }
  const m = r.data
  if (!m || typeof m !== 'object' || (!m.id && !m.appid && !m.name && !m['app-plus'])) {
    MessagePlugin.warning('这个文件不像 uni-app 的 manifest.json')
    return
  }

  const an = m['app-plus']?.distribute?.android || {}
  const ver = m.version || {}
  const parsed = {
    appId: m.id || m.appid || '',
    name: m.name || '',
    versionName: ver.name || m.versionName || '',
    versionCode: ver.code || m.versionCode || '',
    packageName: an.packagename || '',
    minSdk: Number(an.minSdkVersion) || 0,
    targetSdk: Number(an.targetSdkVersion) || 0,
    permissions: Array.isArray(an.permissions)
      ? an.permissions.map((s) => {
          const hit = String(s).match(/android\.permission\.([A-Z][A-Z0-9_]*)/)
          return hit ? hit[1] : ''
        }).filter(Boolean)
      : [],
    modules: Object.keys(m['app-plus']?.modules || {}),
  }

  if (parsed.appId) state.form.appId = parsed.appId
  if (parsed.name) state.form.appName = parsed.name
  if (parsed.versionName) state.form.versionName = String(parsed.versionName)
  if (parsed.versionCode) state.form.versionCode = String(parsed.versionCode)
  if (parsed.packageName) state.form.packageName = parsed.packageName
  applyAndroidConfig(parsed)
  fillPackageFromHistory(parsed.appId)
  state.manifestName = file.name
  await fillAppKey()

  const n = parsed.permissions.length
  const parts = []
  if (n) parts.push(`${n} 项权限`)
  if (parsed.minSdk || parsed.targetSdk) parts.push(`SDK ${parsed.minSdk || '-'}/${parsed.targetSdk || '-'}`)
  if (parts.length) {
    MessagePlugin.success('已读取 ' + parts.join('、'))
  } else {
    MessagePlugin.warning('这份 manifest.json 里没有 distribute.android 配置，只回填了基础信息')
  }
}

// 权限是否被勾选（基线）
export function isKept(name) {
  return state.perm.keep.includes(name)
}
export function toggleKeep(name, on) {
  const set = new Set(state.perm.keep)
  if (on) set.add(name)
  else set.delete(name)
  state.perm.keep = [...set]
}
export function isAdded(name) {
  return state.perm.add.includes(name)
}
export function toggleAdd(name, on) {
  const set = new Set(state.perm.add)
  if (on) set.add(name)
  else set.delete(name)
  state.perm.add = [...set]
}
export function resetPerms() {
  state.perm.keep = BASE_NAMES()
  state.perm.add = []
  state.permSuggested = false
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

// bumpVersion 按「当前输入框里的值」各 +1，不看历史记录
export function bumpVersion() {
  const curName = state.form.versionName.trim()
  const curCode = state.form.versionCode.trim()
  if (!curName && !curCode) {
    MessagePlugin.warning('请先填写版本名或版本号')
    return
  }
  if (curName) state.form.versionName = bumpName(curName)
  if (curCode) state.form.versionCode = String((parseInt(curCode, 10) || 0) + 1)
  MessagePlugin.success(`已 +1：${state.form.versionName} (${state.form.versionCode})`)
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
      minSdk: Number(state.android.minSdk) || 0,
      targetSdk: Number(state.android.targetSdk) || 0,
      addPerms: state.perm.add.filter((n) => !BASE_NAMES().includes(n)),
      delPerms: BASE_NAMES().filter((n) => !state.perm.keep.includes(n)),
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

// ---------------------------------------------------------------- 重置
// revealJob 让服务端在本机文件管理器里打开这条记录的产物目录，返回目录路径
export async function revealJob(id) {
  try {
    const d = await api.reveal(id)
    return d?.dir || ''
  } catch (e) {
    MessagePlugin.error('打开文件夹失败：' + e.message)
    return ''
  }
}

// ---------------------------------------------------------------- 启动
export async function boot() {
  await Promise.all([loadEnv(), loadRuntime(), loadKeystores(), loadJobs(), loadAndroidDefaults(), loadDockerHost()])
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
  }
}
