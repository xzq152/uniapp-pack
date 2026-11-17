# uni-app 安卓离线打包 · Compose 部署

不装 HBuilderX、不碰 Android Studio，只用 Docker 打 APK。

---

## 快速开始

```bash
# 1. 放资源：HBuilderX「发行 → 原生App-本地打包 → 生成本地打包App资源」
#    把导出的 __UNI__XXXX 整个目录放进 ./src/

# 2. 填参数
cp .env.example .env      # 然后编辑 .env

# 3. 打包
docker compose run --rm build
```

APK 出在 `./output/`。

> 前提：本机已装 Docker（Desktop 或 Linux 版），并且已有打包镜像
> `uniapp-android-builder:latest`。镜像不在的话，先在本项目 `android-builder/`
> 目录执行 `build-image.sh` 构建（约 3GB，首次较慢）。

---

## 目录结构

```
./
├── docker-compose.yml
├── .env                    打包参数（从 .env.example 复制）
├── src/                    __UNI__XXXX 资源目录
│   └── __UNI__6744715/
│       └── www/manifest.json
├── output/                 APK 产物
└── config/                 可选：证书 / 图标 / 启动图
    ├── release.jks
    ├── icon.png
    └── splash.png
```

---

## 三个服务分别是什么

| 服务 | 用途 |
|---|---|
| `build` | 一次性打包任务，跑完就退出 |
| `doctor` | 环境自检：版本、SDK、模板、缓存大小 |
| `runtime` | 常驻空转容器，供 **pack-server 网页服务**通过 `docker exec` 复用 |

只用命令行打包的话，`build` 就够了；`runtime` 是给网页版服务预备的。

---

## 参数说明

| 参数 | 必填 | 说明 |
|---|---|---|
| `APP_ID` | ✓ | uni-app appid，如 `__UNI__6744715` |
| `APP_NAME` | ✓ | 应用名（显示在桌面） |
| `APP_PACKAGE` | ✓ | 包名 `applicationId` |
| `APP_VERSION_NAME` | ✓ | 版本名，如 `2.3.7` |
| `APP_VERSION_CODE` | ✓ | 版本号（整数） |
| `APP_KEY` | | DCloud 离线打包 key |
| `MIN_SDK` | | 覆盖模板默认 21 |
| `TARGET_SDK` | | 覆盖模板默认 33，商店上架通常要求 ≥ 30 |
| `ADD_PERMISSIONS` | | 追加权限，如 `CAMERA,POST_NOTIFICATIONS` |
| `DEL_PERMISSIONS` | | 移除权限，如 `READ_MEDIA_VIDEO` |
| `CHANNELS` | | 渠道包，如 `huawei,xiaomi` |
| `KEYSTORE_FILE` | | 正式证书容器内路径，如 `/workspace/config/release.jks` |
| `SPLASH_BG` | | 启动图底色，`auto` 或 `#RRGGBB` |
| `SPLASH_HEIGHT` | | 启动图高度，宽固定 1080 |
| `USE_CN_MIRROR` | | `1`=国内镜像加速（默认），`0`=直连 |

> `MIN_SDK` / `TARGET_SDK` 写非数字会被静默忽略（回退模板默认值），填错不报错，注意核对日志。

---

## 常用命令

| 命令 | 作用 |
|---|---|
| `docker compose run --rm build` | 打一次包 |
| `docker compose run --rm doctor` | 环境自检（版本、SDK、模板、缓存大小） |
| `docker compose up -d runtime` | 起常驻容器（给 pack-server 用） |
| `docker compose stop runtime` | 停常驻容器，释放内存 |
| `docker compose logs -f runtime` | 看常驻容器日志 |
| `docker compose down` | 删除全部容器（卷保留） |

---

## 配合网页服务（pack-server）使用

服务端不拼 `docker run`，而是调这几条：

| 页面动作 | 实际命令 |
|---|---|
| 启动 | `docker compose -p uniapp-pack up -d --remove-orphans runtime` |
| 停止 | `docker compose -p uniapp-pack stop` |
| 重建 | `docker compose -p uniapp-pack up -d --force-recreate runtime` |
| 构建 | `docker exec -e APP_ID=... uniapp-pack-runtime /opt/pack/entrypoint.sh build` |

服务首次启动会自动生成一份 `docker-compose.yml`，**之后不覆盖**，可以直接改。

---

## 注意事项

**卷名别改**。`uniapp_gradle_cache` / `uniapp_pnpm_store` 里是 Gradle 依赖和 pnpm store，
改名等于清缓存，下次编译从 40 秒退回 1~2 分钟。

**`src/` 结构要对**。必须是 `src/__UNI__XXXX/www/manifest.json`，
不能把 `www` 直接放在 `src` 下。放错了会报「没找到本地打包资源」。

**`build` / `doctor` 是一次性任务**，别用 `docker compose up`（会拉起来立刻退出）。
要么 `run --rm`，要么 `up -d runtime`。

**权限移除是靠 manifest merger 的 `tools:node="remove"`**。模板 Manifest 里一条权限都没有，
权限由 SDK 的 aar 合并进来，所以脚本会自动补 `xmlns:tools` 并写 remove 节点，日志会打印「移除 N 条」。

**`targetSdkVersion` 改了要用 `aapt2` 验证**。日志里会回显产物实际的 `sdkVersion` / `targetSdkVersion`，
以那个为准，别只看配置。

---

## 排错

**`env file .env not found`** —— 这个版本的 yml 不会报，若出现说明用的是旧版配置（带 `env_file` 字段）。

**`volume ... was not created by Docker Compose`** —— 只是警告，卷名显式指定了 `name`，
复用已有卷时必然出现，不影响功能。

**构建报「没找到本地打包资源」** —— `src/` 层级不对，或缺 `www` 目录。

**真机提示 appkey 未配置** —— `.env` 里 `APP_KEY` 为空或写错。

**安装后白屏** —— 多为 `APP_PACKAGE` 与资源包不匹配，或 `www` 里缺 `app-service.js`。

**编译很慢** —— 检查 Gradle 卷是否还在：`docker volume ls | grep uniapp_gradle_cache`。
丢掉缓存后首次编译 1~2 分钟，命中缓存约 40 秒。

---

## 实测基线

当前镜像（2026-11-17 构建）实测：

```
Ubuntu 22.04.5 / JDK 17.0.20.1 / Node 20.19.0 / pnpm 9.15.4
Android platforms 33 34 35 / build-tools 34.0.0 35.0.0
Gradle 8.11.1 / AGP 8.7.3 / DCloud SDK 5.34.82692
模板默认 compileSdk 35 / minSdk 21 / targetSdk 33

一次完整构建：39 秒（有缓存），APK 29M
```
