<template>
  <LoginView v-if="state.needLogin" />

  <div v-else class="app-shell">
    <header class="topbar">
      <div class="brand">
        <span class="logo"><t-icon name="logo-android" size="20px" /></span>
        <h1>uniapp 安卓本地打包</h1>
      </div>

      <div class="topbar-right">
        <t-button v-if="state.env.auth" size="small" variant="text" @click="onLogout">退出</t-button>
      </div>
    </header>

    <main class="content">
      <div v-if="!state.booted" class="detecting">
        <t-loading size="small" />
        <span>加载中…</span>
      </div>

      <template v-else>
        <!-- 第一步永远是「绑定镜像环境」，未就绪时下面的工作流不出现 -->
        <EnvironmentStep />

        <transition name="stage-fade" mode="out-in">
          <!-- 环境就绪：才展开上传与打包表单 -->
          <t-card v-if="workflowReady" key="workflow" :bordered="true" class="main-card">
            <template #header>
              <div class="card-head">
                <span class="card-title">新建打包</span>
                <span class="card-desc">{{ state.upload.id ? '确认信息后开始打包' : '先上传资源包' }}</span>
              </div>
            </template>

            <UploadPanel />

            <!-- 用单个元素包一层：<transition> 对多根节点只能做入场，
                 包成 div 后进出都能正确过渡。config-wrap 没有自己的样式，
                 只是过渡的挂载点。 -->
            <transition name="expand">
              <div v-if="state.upload.id" class="config-wrap">
                <div class="divider"></div>
                <ConfigPanel />
              </div>
            </transition>
          </t-card>

          <!-- 未就绪：一句说明，避免空白 -->
          <div v-else key="locked" class="locked">
            <t-icon name="lock-on" size="15px" />
            <span>绑定并启动打包镜像环境后，这里会出现上传与打包表单</span>
          </div>
        </transition>

        <!-- 自己部署：折叠起来，不抢主流程的位置，需要时才展开 -->
        <div class="deploy-card">
          <button class="deploy-toggle" @click="deployOpen = !deployOpen">
            <t-icon :name="deployOpen ? 'chevron-up' : 'chevron-down'" size="16px" />
            <span class="deploy-toggle-title">自己部署一套打包环境</span>
            <span class="deploy-toggle-sub">docker-compose.yml / .env.example / 部署说明，可直接下载</span>
          </button>
          <transition name="expand">
            <div v-if="deployOpen" class="deploy-body">
              <DeployGuide />
            </div>
          </transition>
        </div>
      </template>
    </main>

    <LogDialog />
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { state, workflowReady, boot, logout } from './store'
import LoginView from './components/LoginView.vue'
import EnvironmentStep from './components/EnvironmentStep.vue'
import UploadPanel from './components/UploadPanel.vue'
import ConfigPanel from './components/ConfigPanel.vue'
import LogDialog from './components/LogDialog.vue'
import DeployGuide from './components/DeployGuide.vue'

// 自己部署面板默认收起，避免和「新建打包」抢注意力
const deployOpen = ref(false)

async function onLogout() {
  await logout()
}

onMounted(boot)
</script>

<style scoped>
.locked {
  display: flex; align-items: center; justify-content: center; gap: 8px;
  padding: 26px 16px;
  border: 1px dashed var(--line);
  border-radius: 8px;
  background: #fafbfc;
  color: var(--text-3);
  font-size: 12.5px;
}

/* 主舞台切换：环境就绪前后 */
.stage-fade-enter-active { transition: opacity .32s ease, transform .32s cubic-bezier(.22,.61,.36,1); }
.stage-fade-leave-active { transition: opacity .18s ease, transform .18s ease; }
.stage-fade-enter-from { opacity: 0; transform: translateY(10px); }
.stage-fade-leave-to { opacity: 0; transform: translateY(-6px); }

/* 上传完成后表单展开 */
.expand-enter-active { transition: opacity .3s ease .05s, transform .3s cubic-bezier(.22,.61,.36,1) .05s; }
.expand-leave-active { transition: opacity .16s ease, transform .16s ease; }
.expand-enter-from { opacity: 0; transform: translateY(-8px); }
.expand-leave-to { opacity: 0; transform: translateY(-4px); }

/* 自己部署：可折叠卡片 */
.deploy-card {
  margin-top: 16px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: #fff;
  overflow: hidden;
}
.deploy-toggle {
  width: 100%;
  display: flex; align-items: center; gap: 8px;
  padding: 13px 16px;
  border: none; background: transparent;
  cursor: pointer;
  font-family: inherit;
  text-align: left;
  transition: background .15s;
}
.deploy-toggle:hover { background: #fafbfc; }
.deploy-toggle-title { font-size: 13px; font-weight: 600; color: var(--text-1); }
.deploy-toggle-sub { font-size: 11.5px; color: var(--text-3); }
.deploy-body { padding: 4px 16px 18px; border-top: 1px solid #f0f1f3; padding-top: 16px; }

@media (max-width: 760px) {
  .deploy-toggle-sub { display: none; }
}
</style>
