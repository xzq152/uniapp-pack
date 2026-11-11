# uniapp-android-builder

把 **uni-app 安卓离线打包**整个流程塞进一个 Docker 镜像：**一条命令，产出已签名的 APK**，不依赖 HBuilderX、不依赖云打包排队，可以离线、可复现、能进 CI。

```bash
./pack.sh ../my-uniapp-project -p com.demo.app -n "演示 App"
# → output/演示 App_v2.0.8_20261030-164500.apk
```

## 和参考项目的区别

参考 [u-wlkjyy/uniapp-android-builder-docker](https://github.com/u-wlkjyy/uniapp-android-builder-docker) 已经把编译环境容器化了，但它仍然要求你**先在 HBuilderX 里手动"生成本地打包 App 资源"**，并且要把配置一个一个 `-v` 挂载成 `/workspace/android-project/simpleDemo/...`。本项目在这之上补了几件事：

| | 参考项目 | 本项目 |
|---|---|---|
| 输入 | 必须先在 HBuilderX 生成本地资源 | 支持直接给 **uni-app 源码**，容器内自动 `pnpm/npm install` + `uni build -p app` |
| appid | 手工保证目录名一致 | 自动读 `manifest.json` 的 `id`，同步改写 `assets/apps/<appid>` 与 `dcloud_control.xml` |
| 包名 / 版本号 | 改 `simpledemo-build.gradle` | 命令行 `-p` / `-v` / `--version-code`，Gradle 动态注入 |
| 应用名 | 挂载 `strings.xml` | 直接 sed 改 `app_name`，也可挂载整体 strings.xml |
| 签名 | 用自带的 `test.jks` | 自带 jks 或自动生成持久化证书（否则每次换签名就装不上） |
| 模块名 | 硬编码 `simpleDemo` | 自动解析 `settings.gradle` 的 `include` |
| SDK 组件缺失 | 手工重做镜像 | 按 `build.gradle` 的 compileSdk / buildToolsVersion 自动补安装 |
| 国内网络 | 无处理 | 默认阿里云 Maven 镜像（`--no-cn-mirror` 关闭） |
| 排障 | 看 docker 日志 | `--keep` 保留中间安卓工程，`--doctor` 环境自检，`selftest/` 离线跑通全流程 |

## 前置条件

### 1. 下载 DCloud 安卓离线 SDK（必须，这一步没法自动化）

到 <https://nativesupport.dcloud.net.cn/AppDocs/download/android> 下载**与你 HBuilderX 版本匹配**的 Android 离线 SDK（需要登录）。把 zip 原样放进本目录的 `sdk/` 下：

```
sdk/Android-SDK@4.xx.xxxxx.zip      # 文件名随意，识别到 HBuilder-Integrate-AS 即可
```

> SDK 版本和 HBuilderX 版本差太远会报各种 aar / 资源缺失，版本对齐是最容易踩的坑。

### 2. Docker

已实测环境：Docker Desktop 29.x（Linux container / amd64），构建产物约 **2.3GB**，建议分给 Docker ≥ 8GB 内存、留 ≥ 15GB 磁盘。

**内存是硬门槛**：Android 打包（尤其 dex / 资源优化阶段）很吃内存。低于 4GB 时 Gradle daemon 会被系统 OOM 杀掉，
且报错极其有误导性（`Gradle build daemon disappeared unexpectedly`，看不出是内存问题）。
`pack.sh` 会自动读 `docker info` 的 MemTotal 来定 `-Xmx`（≤一半），但仍建议：

- Docker Desktop → **Settings → Resources → Memory** 调到 **≥ 4GB（推荐 8GB）**
- 换到 `/tmp` 之外的位置没关系，磁盘留 ≥ 15GB 即可

> 如果不想改内存设置，可以加 `--cp` 走 docker cp 传文件（见下），能省掉挂载开销，但编译本身仍需内存。

## 快速开始（按你的环境选一种）

### 0. 只要 Docker：`pack.ps1`（Windows 纯命令行，**无需 Go / Node / Git Bash**）

这是**唯一只依赖 Docker 的入口**。宿主机上不用装 Go、不用装 Node、不用装 Git Bash，
装好 Docker Desktop 就能出包：

```powershell
# 构建镜像（只需一次）
.\build-image.bat

# 打包
.\pack.ps1 D:\pkg\__UNI__6744715 -Package com.demo.app -Name "我的App" -Version 2.0.8
```

参数和 `pack.sh` 基本一一对应（`-Package` / `-Name` / `-Version` / `-Sdk` / `-Cp` …），
运行 `.\pack.ps1 -Help` 看完整列表。需要 PowerShell 5.1+（Windows 10/11 自带）。

> **为什么单独做这个**：`pack.sh` 需要 bash，Windows 上得先装 Git for Windows；
> `pack.bat` 只是转发给 `pack.sh`，同样逃不掉这个依赖。`pack.ps1` 用 PowerShell
> 直接拼 `docker run` 参数，把宿主机依赖压到只剩 Docker。

### A. Windows 双击 / 命令行：`pack.bat`

```bat
pack.bat                                   :: 交互式，把资源目录拖进窗口回车即可
pack.bat D:\pkg\__UNI__6744715 -p com.demo.app -n "我的App" -v 2.0.8
pack.bat --doctor                          :: 环境自检
```
它会自动找 Git Bash 并转发给 `pack.sh`（**需要装 Git for Windows**；
不想装就用上面的 `pack.ps1`）。

### B. Git Bash / macOS / Linux：`pack.sh`

```bash
./pack.sh ../my-uniapp-project -p com.demo.app -n "演示 App"
```

### C. 挂载卡住时：`--cp` / `-Cp`

Windows 上首次 bind mount 有时要等很久（甚至几十分钟），加 `--cp`（PowerShell 用 `-Cp`）
就完全不挂载，改用 `docker create` + `docker cp` 传文件，速度稳定，实测同样能出包：

```bash
./pack.sh --cp ../__UNI__6744715.zip -p com.demo.app -n "演示 App"
```

```powershell
.\pack.ps1 .\__UNI__6744715.zip -Package com.demo.app -Cp
```

---

## 完整流程（第一次使用）

```bash
# 1) 构建镜像（sdk/ 为空也能构建，只是运行时要用 -s 挂载 SDK）
./build-image.sh                  # Windows 纯 Docker 环境用 .\build-image.bat

# 2) 跑一次自检：用假的 gradlew 验证全流程是否贯通（不联网、不真编译）
bash selftest/run.sh
#   看到「完成」+ output/ 下出现 apk 说明链路 OK

# 3) 真正打包
./pack.sh ../my-uniapp-project -p com.demo.app -n "演示 App"
```

`pack.sh` 等价于下面这段 `docker run`（Windows PowerShell / cmd 用户可以直接用）：

```powershell
docker run --rm -e TZ=Asia/Shanghai -e APP_PACKAGE=com.demo.app -e APP_NAME="演示 App" `
  -v ${PWD}\my-uniapp-project:/workspace/src `
  -v ${PWD}\output:/workspace/output `
  -v ${PWD}\config:/workspace/config `
  -v uniapp_gradle_cache:/root/.gradle `
  uniapp-android-builder:latest build
```

## 两种输入模式

`<目录>` 参数给什么都可以，脚本按顺序自动识别：

| 模式 | 触发条件 | 说明 |
|---|---|---|
| **资源模式** | 目录里有 `app-service.js`（含 `www/`、`__UNI__XXX/www`、`unpackage/resources/__UNI__XXX/www`、`dist/build/app` 等形态） | 直接用，跳过前端编译，最快 |
| **源码模式** | 目录里有 `package.json` 但没有 `app-service.js` | 容器内自动装依赖并执行 `build:app`；缺该脚本时回退 `uni build -p app` |

源码模式建议把 pnpm store 挂成一个卷复用（pack.sh 默认已挂 `uniapp_pnpm_store`），冷启动会快很多。

## 目录 & 配置

```
uniapp-android-builder/
├── Dockerfile            # 环境：JDK17 + Android SDK + Node/pnpm + 打包脚本
├── build-image.sh        # 构建镜像（Bash）
├── build-image.bat       # 构建镜像（Windows，只要 Docker）
├── pack.sh               # 一键打包（Bash，日常用这个）
├── pack.ps1              # 一键打包（Windows 纯命令行，只要 Docker）
├── pack.bat              # 交互式入口，转发给 pack.sh（需要 Git Bash）
├── docker/
│   ├── entrypoint.sh     # 容器内主流程
│   ├── prepare-sdk.sh    # 从 DCloud SDK 里抽出工程模板并补齐 aar
│   └── init.gradle       # 阿里云 Maven 镜像（USE_CN_MIRROR=1 时生效）
├── config/               # 挂载进容器的原生配置，详见下
├── sdk/                  # 放 DCloud 离线 SDK 的 zip（构建镜像时烤进去）
├── output/               # APK 与 build-info.json
└── selftest/             # 离线自检：假 gradlew + 假资源
```

### 四个入口怎么选

| 你的环境 | 构建镜像 | 打包 | 宿主机需要 |
|---|---|---|---|
| **只有 Docker**（Windows） | `build-image.bat` | `pack.ps1` | 仅 Docker Desktop |
| Windows + Git Bash | `build-image.bat` | `pack.bat` / `pack.sh` | Docker + Git for Windows |
| macOS / Linux | `build-image.sh` | `pack.sh` | Docker + bash |

`config/` 下**只有存在的文件才会生效**，缺失的项全部走 SDK 模板默认值：

| 路径 | 作用 | 缺失时 |
|---|---|---|
| `config/AndroidManifest.xml` | 整体覆盖清单 | 用 SDK 原生的（推荐不要覆盖） |
| `config/res/**` | 整体覆盖 `app/src/main/res/` | 不覆盖 |
| `config/branding/{icon,splash,push}.png` | 图标 / 启动图 / 推送小图标 | 保留模板自带 |
| `config/strings.xml` | 覆盖 strings | 仅改 `app_name` |
| `config/data/**` | 覆盖 `assets/data/`（dcloud 控制文件） | 自动改 appid/appver |
| `config/keystore/{app.jks, keystore.properties}` | 签名证书 | 自动生成 `auto.jks` |

## 签名

优先级：**命令行 `--keystore` > `config/keystore/app.jks` > 自动生成 `config/keystore/auto.jks`**。

自动生成的证书会连同口令一起写进 `config/keystore/keystore.properties` 并保留在宿主机，
所以同一台机器上反复打包签名是一致的（这点很关键，换签名会导致覆盖安装失败）。
**正式上架请务必换成自己的 jks**，自动证书仅供开发联调。

```bash
./pack.sh ../app -p com.demo.app \
  --keystore ./config/keystore/app.jks \
  --store-pass '******' --key-alias demo --key-pass '******'
```

```powershell
.\pack.ps1 ..\app -Package com.demo.app `
  -Keystore .\config\keystore\app.jks `
  -StorePass '******' -KeyAlias demo -KeyPass '******'
```

## 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `APP_PACKAGE` / `-p` / `-Package` | `com.android.uniapp.pkg` | applicationId，上架前必填 |
| `APP_NAME` / `-n` / `-Name` | manifest 的 `name` | 应用名 |
| `APP_VERSION_NAME` / `-v` / `-Version` | manifest 的 `versionName` | 版本名 |
| `APP_VERSION_CODE` / `--version-code` / `-VersionCode` | `1` | 版本号（整数） |
| `APP_ID` / `--appid` / `-AppId` | manifest 的 `id` | `__UNI__XXXXXXX` |
| `USE_CN_MIRROR` | `1` | 阿里云 Maven 镜像 |
| `GRADLE_EXTRA_ARGS` | 按 Docker 内存自动 | 透传给 gradlew |
| `KEYSTORE_FILE` / `STORE_PASSWORD` / `KEY_ALIAS` / `KEY_PASSWORD` | — | 签名信息 |
| `IMAGE` | `uniapp-android-builder:latest` | 镜像名 |
| `GRADLE_VOLUME` / `PNPM_VOLUME` | `uniapp_gradle_cache` / `uniapp_pnpm_store` | 缓存卷名 |

## 缓存与速度

- **Gradle 依赖**：挂在 `--volume uniapp_gradle_cache:/root/.gradle`，第二次以后基本秒过。
- **pnpm store**：挂在 `uniapp_pnpm_store`，源码模式下免除重复下载。
- **SDK 本体**：建议把 zip 放进 `sdk/` 一起烤进镜像，省掉每次运行时解压。
  想跟换 SDK 版本又不想重做镜像，就 `-s <SDK目录>` 挂载，运行时自动解压。
- 首次打包普遍需要 5～15 分钟（拉 AGP + AndroidX + uni 运行时依赖），之后 1～3 分钟。

## 排障

| 症状 | 处理 |
|---|---|
| 启动即闪退 / 白屏 | 九成是 **SDK 与 HBuilderX 版本不匹配**，或 `AndroidManifest.xml` 被错误覆盖 |
| `找不到 HBuilder-Integrate-AS` | sdk/ 里放的不是完整的安卓离线 SDK |
| `Could not find xxx.aar` | prepare-sdk 没补齐 aar；确认 SDK 包里 `SDK/libs/*.aar` 存在 |
| 构建卡在下载依赖 | 确认 `USE_CN_MIRROR=1`；公司内网需要配代理就加 `-e GRADLE_OPTS=-Dhttps.proxyHost=...` |
| `Gradle build daemon disappeared unexpectedly` | **就是内存不够被 OOM kill**，不是 Gradle 的问题。把 Docker 内存调到 ≥4GB；`pack.sh` 会自动按可用内存设 `-Xmx`，也可手动 `GRADLE_EXTRA_ARGS=-Dorg.gradle.jvmargs=-Xmx1024m` 压低 |
| Gradle OOM / `OutOfMemoryError` | 同上；另可加 `-Dorg.gradle.workers.max=2` 降并发 |
| 安装失败「已存在同名不同签名」 | 证书换了。用固定的 jks，别用不同机器自动生成的 auto.jks |
| 想看中间工程 | `--keep`，产物留在 `build-cache/` |
| Windows 下 `docker run` 卡住不动 / 挂载串错乱 | Git Bash 会把容器路径改写成 Windows 路径。`pack.sh` 已内置 `MSYS_NO_PATHCONV=1`；自己手写命令时请先 `export MSYS_NO_PATHCONV=1` |

## CI 示例（GitHub Actions）

```yaml
- name: 打包 APK
  run: |
    ./pack.sh . -p com.demo.app \
      --version-code ${{ github.run_number }} \
      --keystore ./keystore/app.jks \
      --store-pass ${{ secrets.STORE_PASS }} \
      --key-alias app --key-pass ${{ secrets.KEY_PASS }}
- uses: actions/upload-artifact@v4
  with: { name: apk, path: output/*.apk }
```

## 已知限制 / 可扩展点

- **Apple Silicon**：Mac M 系列上跑 amd64 镜像（`export DOCKER_DEFAULT_PLATFORM=linux/amd64`），首次构建会明显变慢。
- **多渠道**：一次只出一个渠道包。想批量出包可以在 `docker/init.gradle` 里接 walle，或给 `override.gradle` 加 productFlavors 后在 shell 里循环调用 entrypoint。
- 只做 Android；iOS 仍只能在 macOS + Xcode 上打包。
- `--doctor`、`selftest/` 只覆盖到「脚本层面」，真实编译结果仍以 Gradle 输出为准。
