<template>
  <div class="login-wrap">
    <t-card class="login-card" :bordered="true">
      <div class="login-brand">
        <span class="logo" style="width:38px;height:38px;border-radius:10px;background:linear-gradient(135deg,#0052d9,#4b8bff);color:#fff;font-size:14px;font-weight:700;display:flex;align-items:center;justify-content:center;">uni</span>
        <h1>安卓打包服务</h1>
        <p>请输入访问口令</p>
      </div>

      <t-form :data="{}" @submit="onSubmit">
        <t-form-item>
          <t-input
            v-model="password"
            type="password"
            size="large"
            placeholder="访问口令"
            autofocus
            @enter="onSubmit"
          />
        </t-form-item>
        <div style="margin-top: 14px">
          <t-button theme="primary" type="submit" block size="large" :loading="loading">进入</t-button>
        </div>
      </t-form>
    </t-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { login } from '../store'

const password = ref('')
const loading = ref(false)

async function onSubmit() {
  if (!password.value) return
  loading.value = true
  try {
    await login(password.value)
  } catch (e) {
    MessagePlugin.error(e.status === 401 ? '口令不正确' : String(e.message))
  } finally {
    loading.value = false
  }
}
</script>
