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
          <t-button size="small" variant="outline" @click="bumpVersion">上次 +1</t-button>
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
        tips="不传：使用 DCloud 官方通用启动界面（原生渲染：圆形应用图标+加载进度+应用名，自动适配所有屏幕，与云打包效果一致）。传了：使用你的整图，自动转 .9.png（内容区锁定不变形），支持直接上传 .9.png。全出血设计在比例不同的屏幕上仍会整体缩放，建议纯色底+居中 logo">
        <div class="inline">
          <input ref="splashInput" type="file" accept=".png" data-upload="splash" hidden @change="onSplash" />
          <t-button size="small" variant="outline" @click="splashInput?.click()">上传启动图</t-button>
          <t-tag v-if="state.splash.name" size="small" variant="light">{{ state.splash.name }}</t-tag>
          <t-button v-if="state.splash.name" size="small" variant="text" @click="clearSplash">清除</t-button>
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

    <div class="submit-bar">
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
      <span class="cell-sub">{{ submitNote }}</span>
    </div>

    <ProgressPanel />
  </t-form>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { DialogPlugin } from 'tdesign-vue-next'
import { state, submit, bumpVersion, suggestVersion, pickIcon, pickSplash } from '../store'
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
const submitting = ref(false)

function onSplash(e) {
  const f = e.target.files?.[0]
  if (f) pickSplash(f)
  e.target.value = ''
}
function clearSplash() {
  pickSplash(null)
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
  !pkgErr.value && !vnameErr.value && !vcodeErr.value && !ksErr.value,
)

const submitNote = computed(() => {
  if (!state.upload.id) return '请先上传资源包'
  if (state.upload.consumed) return '本次的原包已清理，再打请点「再打一个」或重新上传'
  if (!state.form.packageName.trim()) return '包名 applicationId 必填'
  if (pkgErr.value) return '包名格式不对'
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
.span2 { grid-column: 1 / -1; }
.inline { display: flex; gap: 8px; align-items: center; width: 100%; }
.ks-new { margin-top: 10px; width: 100%; }
.ks-grid {
  display: grid; grid-template-columns: 1fr 1fr; gap: 8px;
  margin-top: 8px; max-width: 560px;
}
.submit-bar {
  display: flex; align-items: center; gap: 12px;
  margin-top: 6px; padding-top: 14px; border-top: 1px dashed #e5e7eb;
}
@media (max-width: 760px) {
  .form-grid, .ks-grid { grid-template-columns: 1fr; }
}
</style>
