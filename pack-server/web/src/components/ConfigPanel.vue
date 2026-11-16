<template>
  <t-form :data="state.form" label-align="top" :colon="false">
    <div class="form-grid">
      <t-form-item label="应用名称">
        <t-input v-model="state.form.appName" placeholder="容通助手" clearable />
      </t-form-item>

      <t-form-item label="包名 applicationId" required :status="pkgErr ? 'error' : 'default'" :tips="pkgErr">
        <t-input v-model="state.form.packageName" placeholder="com.company.app" clearable @blur="onPkgBlur" />
      </t-form-item>

      <t-form-item label="版本名 versionName" :status="vnameErr ? 'error' : 'default'" :tips="vnameErr">
        <div class="inline">
          <t-input v-model="state.form.versionName" placeholder="2.0.8" />
          <t-button size="small" variant="outline" @click="bumpVersion">版本 +1</t-button>
        </div>
      </t-form-item>

      <t-form-item label="版本号 versionCode" :status="vcodeErr ? 'error' : 'default'" :tips="vcodeErr">
        <t-input v-model="state.form.versionCode" placeholder="208" />
      </t-form-item>

      <t-form-item label="appid">
        <t-input v-model="state.form.appId" placeholder="__UNI__XXXXXXX" />
      </t-form-item>

      <t-form-item
        class="span2"
        label="渠道包"
        tips="不选＝只打一个通用包；选了会按渠道各打一个 APK（文件名带渠道名），并把渠道写进 AndroidManifest 的 DCLOUD_STREAMAPP_CHANNEL。可直接输入自定义渠道"
      >
        <t-select
          v-model="state.form.channels"
          :options="channelOptions"
          multiple
          filterable
          creatable
          clearable
          placeholder="选择或输入渠道，如 huawei / xiaomi / yingyongbao"
        />
      </t-form-item>

      <t-form-item
        class="span2"
        label="SDK 版本"
        :status="sdkErr ? 'error' : 'default'"
        :tips="sdkTip"
      >
        <div class="inline">
          <div class="sdk-field">
            <span class="field-tag">minSdk</span>
            <t-input v-model="state.android.minSdk" placeholder="21" style="width: 92px" />
          </div>
          <div class="sdk-field">
            <span class="field-tag">targetSdk</span>
            <t-input v-model="state.android.targetSdk" placeholder="33" style="width: 92px" />
          </div>
          <t-button size="small" variant="text" @click="resetSdk">恢复默认</t-button>
        </div>
      </t-form-item>

      <t-form-item
        class="span2"
        label="安卓权限"
        tips="不勾选＝从 AndroidManifest 里移除，勾选额外项＝追加声明。移除关键权限（网络、存储）可能导致应用无法使用"
      >
        <div class="inline">
          <t-button class="perm-trigger" size="small" variant="outline" @click="permOpen = true">
            管理权限（保留 {{ state.perm.keep.length }}/{{ state.android.baseline.length }} · 额外 {{ state.perm.add.length }}）
          </t-button>
          <input ref="mfInput" type="file" accept=".json" data-upload="manifest" hidden @change="onManifest" />
          <t-button size="small" variant="outline" @click="mfInput?.click()">上传项目 manifest.json</t-button>
          <t-tag v-if="state.manifestName" size="small" variant="light">{{ state.manifestName }}</t-tag>
        </div>
        <div v-if="state.permSuggested" class="hint-line">
          资源包里的 manifest.json 已被 HBuilderX 裁剪，已按声明的模块推测勾选建议权限，请核对后打包
        </div>
      </t-form-item>

      <t-form-item
        class="span2"
        label="离线打包 appkey"
        required
        tips="dev.dcloud.net.cn → 应用 → 各平台信息 → Android → 离线打包 key。不填真机会提示「appkey未配置或配置错误」"
      >
        <t-input v-model="state.form.appKey" placeholder="粘贴 DCloud 离线打包 key" />
      </t-form-item>

      <t-form-item label="应用图标"
        tips="默认取资源包内 static/app_icons 最大的一张，可在这里覆盖（建议 1024×1024）">
        <div class="inline">
          <input ref="iconInput" type="file" accept=".png" data-upload="icon" hidden @change="onIcon" />
          <t-button size="small" variant="outline" @click="iconInput?.click()">上传图标</t-button>
          <t-tag v-if="state.icon.name" size="small" variant="light">{{ state.icon.name }}</t-tag>
          <t-button v-if="state.icon.name" size="small" variant="text" @click="clearIcon">清除</t-button>
        </div>
      </t-form-item>

      <t-form-item label="启动图"
        tips="默认用官方通用界面；自定义上传整图，自动转 .9.png 适配不同屏幕">
        <div class="stack">
          <t-radio-group v-model="splashMode" @change="onSplashMode">
            <t-radio value="default">默认</t-radio>
            <t-radio value="custom">自定义</t-radio>
          </t-radio-group>
          <div v-if="splashMode === 'custom'" class="inline">
            <input ref="splashInput" type="file" accept=".png" data-upload="splash" hidden @change="onSplash" />
            <t-button size="small" variant="outline" @click="splashInput?.click()">上传启动图</t-button>
            <t-tag v-if="state.splash.name" size="small" variant="light">{{ state.splash.name }}</t-tag>
            <t-button v-if="state.splash.name" size="small" variant="text" @click="clearSplash">清除</t-button>
          </div>
        </div>
      </t-form-item>

      <t-form-item class="span2" label="签名证书"
        :status="ksErr ? 'error' : 'default'" :tips="ksErr">
        <t-radio-group v-model="state.form.keystore">
          <t-radio value="auto">自动生成测试证书</t-radio>
          <t-radio value="saved">使用已有证书</t-radio>
          <t-radio value="new">上传新证书</t-radio>
        </t-radio-group>

        <div v-if="state.form.keystore === 'saved'" style="margin-top: 10px; width: 100%">
          <t-select
            v-model="state.form.keystoreId"
            :options="ksOptions"
            placeholder="选择证书"
            style="max-width: 420px"
          />
        </div>

        <div v-else-if="state.form.keystore === 'new'" class="ks-new">
          <input ref="ksInput" type="file" accept=".jks,.keystore" hidden @change="onKs" />
          <div class="inline">
            <t-button size="small" variant="outline" @click="ksInput?.click()">选择 .jks 文件</t-button>
            <t-tag v-if="ksFile" size="small" variant="light">{{ ksFile.name }}</t-tag>
          </div>
          <div class="ks-grid">
            <t-input v-model="newKs.name" placeholder="证书名称（如 公司正式证书）" />
            <t-input v-model="newKs.alias" placeholder="别名 keyAlias" />
            <t-input v-model="newKs.storePass" type="password" placeholder="库密码 storePassword" />
            <t-input v-model="newKs.keyPass" type="password" placeholder="密钥密码（留空同库密码）" />
          </div>
          <div class="cell-sub">上传后会自动保存，下次可直接在下拉里选</div>
        </div>
      </t-form-item>
    </div>

    <!-- 进度放在固定按钮条上方，否则会被遮住 -->
    <ProgressPanel />

    <div class="submit-bar">
      <div class="submit-inner">
        <t-button
          theme="primary"
          size="large"
          :loading="submitting || building"
          :disabled="!canSubmit || submitting || building"
          @click="onSubmit"
        >
          开始打包
        </t-button>
        <t-button
          v-if="state.job.id"
          size="large"
          variant="outline"
          @click="state.logOpen = true"
        >
          打开日志
        </t-button>
        <span class="cell-sub submit-note">{{ submitNote }}</span>
      </div>
    </div>
    <!-- 给固定条留出高度，避免遮住卡片内容 -->
    <div class="submit-spacer"></div>

    <t-dialog
      v-model:visible="permOpen"
      header="安卓权限"
      width="760px"
      :footer="false"
      placement="center"
      class="perm-dialog"
    >
      <div class="perm-toolbar">
        <t-input v-model="permQuery" placeholder="搜索权限名或中文" clearable style="max-width: 280px" />
        <div class="perm-actions">
          <t-button size="small" variant="text" @click="onResetPerms">恢复默认</t-button>
          <t-button size="small" theme="primary" @click="permOpen = false">完成</t-button>
        </div>
      </div>

      <div class="perm-body">
        <div class="perm-section">
          <div class="perm-title">
            APK 现有 {{ state.android.baseline.length }} 项
            <span class="perm-sub">取消勾选 = 打包时移除</span>
          </div>
          <div class="perm-list">
            <div
              v-for="p in filteredBaseline"
              :key="p.name"
              class="perm-item"
              :class="{ off: !isKept(p.name), danger: isCritical(p.name) && !isKept(p.name) }"
              @click="onToggleKeep(p.name)"
            >
              <span class="perm-box" :class="{ on: isKept(p.name) }"></span>
              <span class="perm-name">{{ p.name }}</span>
              <span class="perm-label">{{ p.label }}</span>
            </div>
          </div>
        </div>

        <div class="perm-section catalog">
          <div class="perm-title">
            可额外添加
            <span class="perm-sub">勾选 = 追加声明</span>
          </div>
          <div class="perm-list">
            <div
              v-for="p in filteredCatalog"
              :key="p.name"
              class="perm-item"
              :class="{ on: isAdded(p.name) }"
              @click="onToggleAdd(p.name)"
            >
              <span class="perm-box" :class="{ on: isAdded(p.name) }"></span>
              <span class="perm-name">{{ p.name }}</span>
              <span class="perm-label">{{ p.label }}</span>
              <span v-if="p.note" class="perm-note">{{ p.note }}</span>
            </div>
          </div>
        </div>

        <div v-if="!filteredBaseline.length && !filteredCatalog.length" class="perm-empty">
          没有匹配的权限
        </div>
      </div>
    </t-dialog>
  </t-form>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { DialogPlugin } from 'tdesign-vue-next'
import {
  state, submit, bumpVersion, suggestVersion, pickIcon, pickSplash,
  isKept, toggleKeep, isAdded, toggleAdd, resetPerms, uploadManifestJson, CRITICAL_PERMS,
} from '../store'
import ProgressPanel from './ProgressPanel.vue'

// 常用渠道（可搜索、可直接输入新的）
const channelOptions = [
  { label: '华为 huawei', value: 'huawei' },
  { label: '荣耀 honor', value: 'honor' },
  { label: '小米 xiaomi', value: 'xiaomi' },
  { label: 'OPPO oppo', value: 'oppo' },
  { label: 'vivo vivo', value: 'vivo' },
  { label: '应用宝 yingyongbao', value: 'yingyongbao' },
  { label: '魅族 meizu', value: 'meizu' },
  { label: '三星 samsung', value: 'samsung' },
]
import { PKG_RE, VNAME_RE } from '../utils'

const iconInput = ref(null)
const ksInput = ref(null)
const ksFile = ref(null)
const splashInput = ref(null)
const mfInput = ref(null)
const submitting = ref(false)

// ---- 安卓配置 ----
const permOpen = ref(false)
const permQuery = ref('')

// 取消这些权限基本等于让应用废掉，列表里标红提醒
function isCritical(name) {
  return CRITICAL_PERMS.includes(name)
}

function filterPerms(list) {
  const q = permQuery.value.trim().toLowerCase()
  if (!q) return list
  return list.filter((p) => p.name.toLowerCase().includes(q) || (p.label || '').includes(q))
}

const filteredBaseline = computed(() => filterPerms(state.android.baseline))
const filteredCatalog = computed(() => filterPerms(state.android.catalog))

function onToggleKeep(name) {
  toggleKeep(name, !isKept(name))
}
function onToggleAdd(name) {
  toggleAdd(name, !isAdded(name))
}
function onResetPerms() {
  resetPerms()
}
function onManifest(e) {
  const f = e.target.files?.[0]
  if (f) uploadManifestJson(f)
  e.target.value = ''
}
function resetSdk() {
  state.android.minSdk = state.android.defMinSdk
  state.android.targetSdk = state.android.defTargetSdk
}

const sdkErr = computed(() => {
  const a = Number(state.android.minSdk)
  const b = Number(state.android.targetSdk)
  if (!state.android.minSdk || !state.android.targetSdk) return '两项都要填'
  if (!Number.isInteger(a) || !Number.isInteger(b)) return '必须是整数'
  if (a < 16) return 'minSdk 不能低于 16'
  if (b < a) return 'targetSdk 不能小于 minSdk'
  if (b > state.android.compileSdk) return `targetSdk 不能超过编译版本 ${state.android.compileSdk}`
  return ''
})
const sdkTip = computed(
  () => sdkErr.value
    || `默认沿用 DCloud SDK 模板（minSdk 21 / targetSdk 33）。部分应用商店要求 targetSdk ≥ 34，可按需调高；调高后请回归测试通知、存储、定位等权限`,
)

function onSplash(e) {
  const f = e.target.files?.[0]
  if (f) pickSplash(f)
  e.target.value = ''
}

// 启动图：默认（官方通用界面） / 自定义（上传整图）
const splashMode = ref('default')

function onSplashMode(v) {
  splashMode.value = v
  if (v === 'default') pickSplash(null)
}

// 表单重置或外部清空文件时，单选框回到「默认」
watch(
  () => state.splash.file,
  (f) => {
    if (!f) splashMode.value = 'default'
    else splashMode.value = 'custom'
  },
  { immediate: true },
)

function clearSplash() {
  pickSplash(null)
  splashMode.value = 'default'
}
// 构建期间按钮保持 loading，避免重复提交
const building = computed(() => !!state.job.id && ['queued', 'running'].includes(state.job.status))

const newKs = reactive({ name: '', alias: '', storePass: '', keyPass: '' })

const pkgErr = computed(() => {
  const v = state.form.packageName.trim()
  if (!v) return ''
  return PKG_RE.test(v) ? '' : '格式应类似 com.company.app（至少两段，字母开头）'
})

const vnameErr = computed(() => {
  const v = state.form.versionName.trim()
  if (!v) return ''
  return VNAME_RE.test(v) ? '' : '只能是数字和点，如 2.0.8'
})

const vcodeErr = computed(() => {
  const v = state.form.versionCode.trim()
  if (!v) return ''
  return /^\d+$/.test(v) ? '' : '必须是整数'
})

const ksErr = computed(() => {
  if (state.form.keystore === 'saved' && !state.form.keystoreId) return '请选择一个证书'
  if (state.form.keystore === 'new' && !ksFile.value) return '请选择 .jks 文件'
  if (state.form.keystore === 'new' && (!newKs.alias || !newKs.storePass)) return '证别名和库密码必填'
  return ''
})

const ksOptions = computed(() =>
  state.keystores.map((k) => ({ label: `${k.name} · ${k.alias}`, value: k.id })),
)

const canSubmit = computed(() =>
  !!state.upload.id &&
  !state.upload.consumed &&
  !!state.form.packageName.trim() &&
  !pkgErr.value && !vnameErr.value && !vcodeErr.value && !ksErr.value && !sdkErr.value,
)

const submitNote = computed(() => {
  if (!state.upload.id) return '请先上传资源包'
  if (state.upload.consumed) return '本次的原包已清理，要再打一次请重新选择文件夹'
  if (!state.form.packageName.trim()) return '包名 applicationId 必填'
  if (pkgErr.value) return '包名格式不对'
  if (sdkErr.value) return sdkErr.value
  if (!state.form.appKey.trim()) return '建议填写 appkey，否则真机会提示 appkey 错误'
  return ''
})

function onPkgBlur() {
  suggestVersion()
}

function onIcon(e) {
  const f = e.target.files?.[0]
  if (f) pickIcon(f)
  e.target.value = ''
}

function clearIcon() {
  pickIcon(null)
}

function onKs(e) {
  const f = e.target.files?.[0]
  if (f) ksFile.value = f
  e.target.value = ''
}

async function onSubmit() {
  const doSubmit = async () => {
    if (submitting.value) return
    state.form.newKeystore = {
      file: ksFile.value,
      name: newKs.name || ksFile.value?.name,
      alias: newKs.alias,
      storePass: newKs.storePass,
      keyPass: newKs.keyPass,
    }
    submitting.value = true
    try {
      await submit()
    } finally {
      submitting.value = false
    }
  }
  if (!state.form.appKey.trim()) {
    const dlg = DialogPlugin.confirm({
      header: '还没有填写 appkey',
      body: '打出的包在真机上会提示「appkey未配置或配置错误」，确定继续？',
      confirmBtn: '继续打包',
      cancelBtn: '返回填写',
      onConfirm: () => { dlg.destroy(); doSubmit() },
      onCancel: () => dlg.destroy(),
    })
    return
  }
  doSubmit()
}
</script>

<style scoped>
.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 4px 18px;
}
.form-grid :deep(.t-form__item) { margin-bottom: 14px; }
/* TDesign 的 controls-content 默认 nowrap，多行内容（如权限提示条）会被挤到同一行 */
.form-grid :deep(.t-form__controls-content) { flex-wrap: wrap; }
.span2 { grid-column: 1 / -1; }
.inline { display: flex; gap: 8px; align-items: center; width: 100%; }
.stack { display: flex; flex-direction: column; gap: 8px; width: 100%; align-items: flex-start; }
.ks-new { margin-top: 10px; width: 100%; }
.ks-grid {
  display: grid; grid-template-columns: 1fr 1fr; gap: 8px;
  margin-top: 8px; max-width: 560px;
}
/* 底部固定操作条：卡片高度会超出视口，sticky 在末尾无法吸附，只能 fixed */
.submit-bar {
  position: fixed;
  left: 0; right: 0; bottom: 0;
  z-index: 100;
  background: #fff;
  border-top: 1px solid #e5e7eb;
  box-shadow: 0 -6px 18px rgba(15, 23, 42, 0.08);
}
.submit-inner {
  max-width: 1120px;
  margin: 0 auto;
  /* 48 = 页面内容区 padding(24) + 卡片 padding(24)，让按钮和表单输入框左对齐 */
  padding: 12px 48px;
  display: flex;
  align-items: center;
  gap: 12px;
}
.submit-note {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.submit-spacer { height: 56px; }

/* ---- SDK 版本 / 权限 ---- */
.sdk-field { display: flex; align-items: center; gap: 6px; }
.field-tag { font-size: 12px; color: #6b7280; }
.hint-line {
  flex-basis: 100%; width: 100%;
  margin-top: 8px; font-size: 12px; color: #b45309; line-height: 1.6;
  background: #fffbeb; border-left: 2px solid #f59e0b;
  padding: 5px 9px; border-radius: 3px;
}

.perm-toolbar {
  display: flex; align-items: center; justify-content: space-between;
  gap: 12px; margin-bottom: 10px;
}
.perm-actions { display: flex; gap: 8px; }
.perm-body { max-height: 52vh; overflow-y: auto; padding-right: 6px; }
.perm-section + .perm-section { margin-top: 18px; }
.perm-title { font-size: 13px; font-weight: 600; color: #111827; margin-bottom: 6px; }
.perm-sub { font-weight: 400; font-size: 12px; color: #9ca3af; margin-left: 8px; }
.perm-list { display: grid; grid-template-columns: 1fr 1fr; gap: 1px 12px; }
/* 「可额外添加」带说明文字，改成单列，避免长权限名被挤成竖排 */
.perm-section.catalog .perm-list { grid-template-columns: 1fr; }
.perm-item {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  padding: 5px 8px; border-radius: 4px; cursor: pointer;
  font-size: 12px; color: #374151; line-height: 1.4;
}
.perm-item:hover { background: #f3f4f6; }
.perm-item.off .perm-name,
.perm-item.off .perm-label { color: #9ca3af; text-decoration: line-through; }
.perm-item.danger { background: #fef2f2; }
.perm-item.danger .perm-label { color: #b91c1c; }
.perm-box {
  width: 14px; height: 14px; flex: none;
  border: 1.5px solid #d1d5db; border-radius: 3px;
  display: inline-flex; align-items: center; justify-content: center;
  background: #fff;
}
.perm-box.on { background: #0052d9; border-color: #0052d9; }
.perm-box.on::after {
  content: ''; width: 3px; height: 7px; margin-top: -2px;
  border: solid #fff; border-width: 0 2px 2px 0;
  transform: rotate(45deg);
}
.perm-name {
  font-family: ui-monospace, Consolas, monospace; font-size: 11px; color: #4b5563;
  flex: none; white-space: nowrap;
}
.perm-label { color: #6b7280; white-space: nowrap; }
.perm-note { font-size: 11px; color: #b45309; margin-left: 2px; }
.perm-note::before { content: '·'; margin-right: 5px; color: #d1d5db; }
.perm-empty { padding: 28px; text-align: center; color: #9ca3af; font-size: 13px; }

@media (max-width: 760px) {
  .form-grid, .ks-grid { grid-template-columns: 1fr; }
  .perm-list { grid-template-columns: 1fr; }
  .submit-inner { padding: 10px 36px; gap: 8px; }
  .submit-inner :deep(.t-button) { padding-left: 16px; padding-right: 16px; }
  .submit-spacer { height: 52px; }
}
</style>
