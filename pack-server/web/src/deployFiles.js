// 部署相关文件的原文，打包时用 Vite 的 ?raw 内联进 bundle。
// 这样用户不用去仓库翻文件，页面里就能看、复制、下载。

import composeYml from './deploy/docker-compose.yml?raw'
import envExample from './deploy/.env.example?raw'
import deployReadme from './deploy/README.md?raw'

export const DEPLOY_FILES = [
  {
    key: 'compose',
    name: 'docker-compose.yml',
    desc: '三个服务：build 打包 / doctor 自检 / runtime 常驻',
    content: composeYml,
  },
  {
    key: 'env',
    name: '.env.example',
    desc: '打包参数模板，复制成 .env 再填',
    content: envExample,
  },
  {
    key: 'readme',
    name: 'README.md',
    desc: '完整部署说明：参数表、命令、注意事项、排错',
    content: deployReadme,
  },
]

export const DEPLOY_STEPS = [
  {
    n: 1,
    title: '放资源',
    body: 'HBuilderX「发行 → 原生App-本地打包 → 生成本地打包App资源」，把导出的 __UNI__XXXX 整个目录放进 ./src/',
  },
  {
    n: 2,
    title: '填参数',
    body: '把 .env.example 复制成 .env，填 APP_ID / APP_NAME / APP_PACKAGE / 版本号，至少这几项',
  },
  {
    n: 3,
    title: '开始打包',
    body: '在终端执行 docker compose run --rm build，APK 会出现在 ./output/',
  },
]
