import { createApp } from 'vue'
import TDesign from 'tdesign-vue-next'
import { MotionPlugin } from '@vueuse/motion'
import 'tdesign-vue-next/es/style/index.css'
import './styles.css'
import App from './App.vue'

createApp(App)
  .use(TDesign)
  // v-motion：给步骤切换、卡片、状态变化加过渡，避免生硬跳变
  .use(MotionPlugin)
  .mount('#app')
