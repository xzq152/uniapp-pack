<template>
  <t-dialog
    v-model:visible="state.refillOpen"
    header="从历史记录回填"
    width="720px"
    :footer="false"
    :close-on-overlay-click="true"
  >
    <div class="refill">
      <t-input v-model="kw" placeholder="按应用名 / 包名 / 版本筛选" clearable>
        <template #prefix-icon><t-icon name="search" /></template>
      </t-input>

      <div class="refill-list">
        <t-empty v-if="!list.length" description="还没有可回填的历史记录" />

        <div
          v-for="j in list"
          :key="j.id"
          class="refill-item"
          @click="choose(j)"
        >
          <div class="ri-head">
            <span class="ri-name">{{ j.appName || j.package }}</span>
            <t-tag size="small" variant="light">{{ j.versionName }} ({{ j.versionCode }})</t-tag>
            <span style="flex: 1"></span>
            <span class="ri-time">{{ fmtTime(j.createdAt) }}</span>
          </div>

          <div class="ri-grid">
            <span class="k">包名</span><span class="v">{{ j.package || '-' }}</span>
            <span class="k">appid</span><span class="v">{{ j.appId || '-' }}</span>
            <span class="k">appkey</span>
            <span class="v">
              {{ j.appKeySet ? '已设置' : (j.appKey ? '已填写' : '未设置') }}
            </span>
            <span class="k">证书</span>
            <span class="v">{{ j.testSigned ? '自动生成测试证书' : (j.keystore && j.keystore !== 'auto' ? '使用已保存证书' : '自动生成测试证书') }}</span>
          </div>

          <div class="ri-actions">
            <t-button size="small" theme="primary" variant="text" @click.stop="choose(j)">用这套信息</t-button>
          </div>
        </div>
      </div>
    </div>
  </t-dialog>
</template>

<script setup>
import { computed, ref } from 'vue'
import { state, fillFrom } from '../store'
import { fmtTime } from '../utils'

const kw = ref('')

const list = computed(() => {
  const q = kw.value.trim().toLowerCase()
  const all = [...state.jobs].sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt))
  if (!q) return all
  return all.filter((j) =>
    [j.appName, j.package, j.versionName, j.versionCode, j.appId]
      .some((v) => String(v || '').toLowerCase().includes(q)),
  )
})

function choose(j) {
  fillFrom(j)
}
</script>

<style scoped>
.refill-list {
  margin-top: 10px;
  max-height: 56vh;
  overflow-y: auto;
}
.refill-item {
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  padding: 10px 12px;
  margin-bottom: 8px;
  cursor: pointer;
  transition: border-color .15s, background .15s;
}
.refill-item:hover { border-color: #0052d9; background: #f7faff; }
.ri-head { display: flex; align-items: center; gap: 8px; }
.ri-name { font-size: 13.5px; font-weight: 600; }
.ri-time { font-size: 12px; color: #8a94a1; }
.ri-grid {
  display: grid;
  grid-template-columns: 56px 1fr;
  gap: 3px 12px;
  margin-top: 8px;
  font-size: 12.5px;
}
.ri-grid .k { color: #8a94a1; }
.ri-grid .v { color: #1c2126; word-break: break-all; }
.ri-actions { display: flex; justify-content: flex-end; margin-top: 4px; }
</style>
