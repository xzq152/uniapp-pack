# uni-app 安卓打包服务（Go + Vue3 / 局域网）

一个单文件的内网打包服务：同事在浏览器里**上传资源包 → 填包名和证书 → 看进度条 → 下载 APK**，
不用装 Docker、不用碰命令行。底层复用 `../uniapp-android-builder` 的打包镜像。

- 后端：Go 标准库（`net/http` + `embed`），零第三方依赖
- 前端：**Vue 3 + [TDesign Vue Next](https://tdesign.tencent.com/vue-next/overview)**，Vite 构建后整包嵌入二进制

```
┌─────────────┐  上传 zip   ┌──────────────────┐  docker  ┌────────────────┐
│ 局域网任意电脑 │ ─────────► │ 本服务 (Go :8080) │ ───────► │ 打包镜像出 APK  │
│   浏览器      │ ◄───────── │  SSE 进度 + 下载  │          │                │
└─────────────┘   Range 下载 └──────────────────┘          └────────────────┘
```

## 启动

**双击 `启动服务.exe`** —— 不显示命令行窗口，服务在后台跑，**右下角托盘出现图标**：

- 启动时弹一个气泡提示（带局域网地址），几秒自动消失
- **双击托盘图标** → 打开打包页面
- **右键托盘图标** → 菜单：
  - 顶部灰字状态：`服务运行中/未运行 · 打包环境运行中/已停止`
  - 打开打包页面
  - 重新启动服务
  - **停止打包环境（仅容器）** —— 只停 Docker 容器释放内存，服务继续跑
  - **停止服务并停止镜像** —— 先停容器，再结束服务和托盘
  - 仅停止服务（保留镜像）—— 只结束服务和托盘
- 鼠标悬停能看到当前状态和地址

**重复启动会被拦掉**：托盘宿主有单实例互斥（`Local\uniapp-pack-tray`），
再双击只会提示「已经在运行」；如果 8080 被**别的程序**占用（探活 `/api/env` 不通），
会明确报「端口被其他程序占用」，而不是傻等。
带 `/open` 参数启动（桌面快捷方式用的就是这个）时，已经在运行就不弹框，直接开页面。
- 托盘图标是 Android 机器人头（Material Design Icons `android`，Apache-2.0，填充 `#3DDC84`）；
  源图 `cmd/launcher/tray.svg`，`tray.ico` 含 16/24/32/48/64 五种尺寸，编译时嵌入，
  运行时写临时文件用 `LoadImage` 加载（直接塞整个 .ico 给 `CreateIconFromResourceEx` 会失败并退回系统默认图标）
- 托盘图标是 Android 机器人头（Material Design Icons `android`，Apache-2.0，填充 `#3DDC84`）；
  源图 `cmd/launcher/tray.svg`，`tray.ico` 含 16/24/32/48/64 五种尺寸，编译时嵌入，
  运行时写临时文件用 `LoadImage` 加载（直接塞整个 .ico 给 `CreateIconFromResourceEx` 会失败并退回系统默认图标）

停止：双击 `停止服务.exe`（无窗口，会同时移除托盘图标并弹提示）。
加参数 `/image` 会连打包容器一起停：`停止服务.exe /image`。

**桌面快捷方式** `uni-app 安卓打包.lnk`：目标 `启动服务.exe`，参数 `/open`，
双击 = 启动服务（已在运行则跳过）+ 打开 http://127.0.0.1:8080 。

命令行方式（调试时看日志更方便）：

```bash
./uniapp-pack-server.exe          # 前台运行，会打印局域网地址
```

**改了前端代码**后要重新构建（前端是编译进 exe 的）：

```bash
双击 build.bat            # 前端 npm run build → go build → 启动器
# 或手动：
cd web && npm run build && cd .. && go build -o uniapp-pack-server.exe .
```

> 构建前先 `停止服务.exe`，否则 exe 被占用会报 Access is denied。

前端热更新调试（后端照常跑在 8080）：

```bash
cd web && npm run dev     # http://localhost:5173，/api 与 /d 已代理到 8080
```

其他电脑用提示框里的 `http://192.168.x.x:8080` 打开。

> 其他电脑打不开 → 检查 Windows 防火墙是否放行 8080 端口。

## 远程 Docker（Docker 不在本机）

服务只用 Docker Engine 的 HTTP API，**本机不需要装 docker 命令行**，所以「服务在 A 机、Docker 在 B 机」
完全支持：停止 / 启动 / 重建 都是直接打到 B 机的 API，功能与 Docker 在本机时一致。

**在页面上切换（推荐）**

顶栏状态胶囊里的地址就是当前 Docker 位置，点它打开「Docker 地址」弹窗：

- 当前地址上方有「本机 / 远程」标签
- 输入框填远程地址 → 点「测试连接」只试连（会显示 Docker 版本和可用内存）→ 点「切换」正式生效
- 地址写法随意：`192.168.1.23`（自动补 2375）、`192.168.1.23:2375`、`http://192.168.1.23:2375` 都认
- **留空点「切换」= 回到本机 Docker**
- 切换会先试连，**连不通就拒绝**，不会把服务打到不可用的地址上
- 有任务正在构建（`running`/`queued`）时不允许切换，避免构建中途换机器
- 切换后自动记住，写在 `data/docker.json`，**重启服务仍然生效**

**用环境变量（启动时就固定）**

```bat
set "DOCKER_HOST=tcp://192.168.1.23:2375"
```

> 优先级：`data/docker.json`（页面切换过的） > 环境变量 `DOCKER_HOST` > 本机 socket。

**对方机器要开放 TCP 2375**

- Docker Desktop：Settings → General → 勾选
  「Expose daemon on tcp://localhost:2375 without TLS」
- Linux：`-H tcp://0.0.0.0:2375`（注意这是**无 TLS 的明文端口**，只适合可信内网）
- 防火墙放行 2375；连通性可用 `curl http://192.168.1.23:2375/version` 快速自查

> 安全提醒：2375 无鉴权，等于把 Docker 控制权交出去。**务必只在隔离内网使用**，
> 不要暴露到公网或办公网大网段。

## 界面流程

页面只有两块：**新建打包** + **打包记录**。

0. **环境开关** —— 顶栏标题右边的状态胶囊：`● 打包环境运行中 / ● 打包环境已停止`。
   默认停止（容器不跑就不占内存），点胶囊里的「启动」约 2 秒；停止状态下不显示打包区。
   首次打开会先显示「检测 Docker 服务…」，检测完才判定是运行中还是未连接。
   **胶囊里还会显示当前 Docker 地址**（本机显示「本机」，远程显示 `IP:端口`），
   点它就能切换地址——见下面「远程 Docker」。**停止 / 重建 等操作始终作用在当前地址上**。
1. **上传资源包** —— 没上传时是一块拖拽区，支持两种方式：
   **选择 zip 文件**（HBuilderX 生成的 __UNI__XXXX.zip）或
   **选择文件夹**（HBuilderX 导出的本地打包资源目录，浏览器按相对路径整目录上传，
   需 Chrome / Edge 等内核；文件夹会在服务端原样还原，打包时自动压成 zip 送进容器）。
   上传成功后收成一行文件条（名称 / 大小 / 文件数 / 是否带图标 / 重新选择 / 回填信息）。
2. **填写信息** —— 上传后才展开表单：应用名、包名、版本名（旁边「上次 +1」）、版本号、appid、
   **渠道包（可选，多选/可输入）**、离线打包 appkey、图标（可选）、启动图（可选）、签名证书。
   包名和版本号实时校验。
3. **开始打包** —— 按钮旁边是「打开日志」。进度条**只在提交后真正有进度时**才出现在按钮下方，
   同时显示阶段文字；完成后原地显示产物名、体积和**产物所在的本机目录**，
   右侧「打开文件夹」直接在本机文件管理器里定位到 `data/outputs/<jobId>/`（本地构建，不用再下载一遍）。
   日志在弹窗里实时滚动，带「复制 / 下载 / 自动滚动」，不占页面空间。
4. **打包记录** —— 状态用**整行底色**表示（绿=完成 / 红=失败 / 蓝=构建中 / 橙=排队），
   支持搜索、状态筛选、每页 5 条分页、展开看详情。
   需要复用以前的填写内容时，用上传区右侧的「回填信息」按钮，从弹窗里挑一条。

## 效率细节

| 场景 | 做法 |
|---|---|
| 重复打同一应用的包 | 上传 zip 后点「回填信息」→ 弹窗里挑一条历史，自动填好包名/appkey/证书/版本 |
| 版本号懒得算 | 版本名旁边「上次 +1」，按同包名最近一条自动 `2.0.8 → 2.0.9`、`versionCode + 1` |
| 上传时资源包没写版本 | 自动按同包名历史记录推算填入 |
| 填错包名 | 实时校验 `com.company.app` 格式、`versionCode` 必须整数，红字提示并且禁用提交按钮 |
| 构建失败看不清原因 | 「打开日志」弹窗里实时看，可复制/下载；失败记录上直接「重试」（同参数重跑，不用重新上传） |
| 手滑删了记录 | 删除是移入**回收站**，7 天内可恢复或彻底删除（`data/trash/`） |
| 记录越来越多 | 只保留最近 20 条（`KEEP_JOBS` 可调），更早的自动移入回收站，不占列表 |
| 记录太多 | 搜索框（应用/包名/版本/文件名）+ 状态筛选 + 每页 15 条「显示更多」 |
| 一眼分清记录状态 | 不占一列标签：**整行底色**表示状态（绿=完成 / 红=失败 / 蓝=构建中 / 橙=排队），行首还有 3px 色条 |
| 打完包取产物 | 服务在本机跑，完成后点「打开文件夹」直接到 `data/outputs/<jobId>/`，渠道包和 build-info.json 都在里面 |
| 下载前想确认打的什么包 | 完成区显示体积和产物目录；记录右侧显示 `minSdk / targetSdk / 体积`；用测试证书签名会打橙色「测试签名」标记 |

## appkey 必须填，否则真机起不来

离线打包的 APK 在启动时校验 `AndroidManifest.xml` 里的 `dcloud_appkey` 是否与 appid 匹配，
不匹配就是那句 **「appkey未配置或配置错误」**。SDK 模板里这个值是 DCloud 写的一句中文提示语，
所以不填一定报错。

获取方式：<https://dev.dcloud.net.cn> → 选择应用 → **各平台信息 → Android → 离线打包 key**。
填一次会按 appid 记在 `data/appkeys.json`，下次同一个 appid 自动带出。

## 渠道包

HBuilderX 云打包的「渠道包」在这边同样支持：**一个版本，按渠道出多个 APK**。

在表单「渠道包」里选（或直接输入）渠道，例如 `huawei / xiaomi / yingyongbao`：

- 不选 = 只打 **1 个通用包**（不带任何渠道标记），和以前一样
- 选了 N 个 = 打 **N 个 APK**，文件名带渠道段：
  `容通助手_v2.4.1_huawei_20261104-120128.apk`、`..._xiaomi_....apk`
- 每个包都会写入 Manifest 的渠道标记（与 DCloud 云打包同构）：

```xml
<meta-data android:name="DCLOUD_STREAMAPP_CHANNEL"
           android:value="包名|appid|0|渠道名"/>
```

实现方式：只组装一次工程，然后**按渠道逐个跑 `assembleRelease`**（改 Manifest → 打包 → 收集），
Gradle 增量让第 2 个渠道起每个约 30~40 秒（双渠道整包约 90 秒）。

多渠道构建的产物同样落在同一个 `data/outputs/<jobId>/` 目录里，点「打开文件夹」就能
一次看到所有渠道的 APK 和 `build-info.json`，不用逐条下载：

- 完成区会提示「N 个渠道包」，体积是该记录第一个包的大小
- 页面上的 `GET /api/jobs/{id}/zip`（全部打成一个 zip）和 `GET /d/{jobId}/{文件名}`（单包，支持 Range）
  仍然保留，需要把包发给别的机器时可以直接用这两个地址
- 展开记录详情还能看到这次选了哪些渠道
渠道名只保留 `字母数字_-`，自动去重，最多 12 个。

> 改完打包脚本（`docker/entrypoint.sh`）必须重建镜像，**并且重启一次打包环境容器**——
> `docker exec` 用的是容器里的旧脚本，不重启会跑老逻辑（这个坑踩过）。

## 图标与启动图

**应用图标**优先级（从高到低）：

1. 页面上传的图标（`data/icons/`）
2. `config/branding/icon.png`（命令行用法）
3. **资源包内 `www/static/app_icons/` 最大那张**（HBuilderX 打资源包时自带 1024×1024 等全套）
4. SDK 模板默认图标

**启动图**：与官方云打包一致（见
[启动界面文档](https://uniapp.dcloud.net.cn/tutorial/app-splashscreen.html)）：

1. **不传启动图** → **不注入任何图片**，交给 DCloud runtime 原生渲染「通用启动界面」：
   圆形应用图标 + 加载动画 + 应用名，自动适配所有屏幕比例，和官方云打包出来的效果完全一样。
   （对比过官方云打包的 APK：里面根本没有 splash 资源，所以这条路才是正解）
2. **上传启动图** → 使用你的整图，自动转 `.9.png`（背景拉伸、内容区锁定），
   直接上传 `.9.png` 则原样使用。

原理：DCloud 运行时绘制启动图前会检查 `Bitmap.getNinePatchChunk()`，
是 nine-patch 就用 `NinePatchDrawable` 绘制，黑线标记的区域才拉伸 ——
所以「纯色底 + 居中 logo」的设计在任意分辨率下都不会变形。
产物里对应 `res/drawable-nodpi/splash.9.png`（模板自带的 16:9 默认图会被删除）。

> 全出血（内容铺满整图）的设计没有可锁定的纯色区，比例差异大时仍会整体缩放，
> 建议自定义启动图用「纯色底 + 居中 logo」。

想用设计软件做 .9 图：Android Studio 右键 png → Create 9-Patch file，或用
[在线工具](http://inloop.github.io/shadow4android/) 做好后直接上传。

## 速度相关设计

| 环节 | 做法 |
|---|---|
| 上传 | 裸流 `PUT`，不做 multipart / base64，服务端 1MB 缓冲直接落盘；前端 XHR 显示实时 MB/s |
| 传输到容器 | `docker cp` 直传，**不走 bind mount**（Windows 首次挂载可能卡几十分钟） |
| 取产物 | 默认「打开文件夹」直接定位本机目录，零拷贝；需要跨机时再用 Range 下载 |
| 下载 | 手写 Range 支持（206 + `Content-Range`），1MB 拷贝缓冲；`Accept-Ranges` 开启所以能断点续传 |
| 长连接 | SSE 每 15s 心跳；服务关闭了读写超时，大包上传/下载不会被掐断 |

实测（环回地址）：52MB APK 整包下载 1.9s ≈ 30MB/s；局域网千兆环境下瓶颈在网卡而非本服务。

## 配置（环境变量）

| 变量 | 默认 | 说明 |
|---|---|---|
| `PORT` | `8080` | 监听端口 |
| `DATA_DIR` | `./data` | 上传、产物、证书存放目录 |
| `IMAGE` | `uniapp-android-builder:latest` | 打包镜像 |
| `GRADLE_VOLUME` | `uniapp_gradle_cache` | Gradle 缓存卷（**保留能大幅提速**） |
| `PNPM_VOLUME` | `uniapp_pnpm_store` | pnpm 缓存卷 |
| `GRADLE_EXTRA_ARGS` | 自动 | 缺省时按 `docker info` 内存自动给 `-Xmx` |
| `USE_CN_MIRROR` | `1` | 阿里云 Maven 镜像 |
| `ACCESS_PASSWORD` | 空 | **访问口令**。设置了就必须登录才能用；不设置则局域网内免登录 |
| `SPLASH_HEIGHT` | `2400` | 启动图高度（宽固定 1080），按真机比例调整避免拉伸变形 |
| `KEEP_APK` | `5` | **只保留最近几次的 APK**，更早的自动删除（记录和 build-info 保留，详情仍可查） |
| `KEEP_JOBS` | `20` | **历史记录只保留最近多少条**。页面已不展示记录列表，这个值现在只用于控制 `jobs.json` 体积、以及按 appid 带出上次的包名/appkey；超出的移入回收站，7 天内可恢复 |

## 分离部署（Go 服务与 Docker 不在同一台机器）

调试后端代码时，可以让 **Go 服务跑在自己电脑，Docker 只装在打包机上**。
服务端通过 `DOCKER_HOST` 直连远程 daemon，代码不用改：所有文件传输走 `docker cp`（API 流），
只用到 Gradle / pnpm 两个命名卷，没有绑定宿主目录，所以跨机是安全的。

### 打包机（装 Docker + 镜像的那台）

1. Docker Desktop → Settings → General → 勾选
   **"Expose daemon on tcp://localhost:2375 without TLS"**
   （等价于在 `%APPDATA%\Docker\settings-store.json` 里加 `"ExposeDockerAPIOnTCP2375": true`，改完要重启 Docker Desktop）

2. 这个开关只绑 `127.0.0.1`，需要转发到局域网：

   ```
   netsh interface portproxy add v4tov4 listenaddress=0.0.0.0 listenport=2375 connectaddress=127.0.0.1 connectport=2375
   ```

3. 防火墙放行 2375（**只在可信网段开放**）：

   ```powershell
   New-NetFirewallRule -DisplayName "Docker API 2375" -Direction Inbound -Protocol TCP -LocalPort 2375 -Action Allow -Profile Private,Domain
   ```

4. 镜像在本机构建（`build-image.sh`）。**不要在开发机上跑 `docker build`**——
   构建上下文（含 294MB SDK）会整包传到远端，既慢又占带宽。

### 开发机（跑 Go 服务）

1. **不需要 docker CLI**——服务只用 Docker Engine 的 HTTP API，能访问 `2375` 端口即可
2. 设环境变量后启动服务（也可以在页面上直接切换，见上文「远程 Docker」）：

   ```bat
   set DOCKER_HOST=tcp://192.168.1.23:2375
   uniapp-pack-server.exe
   ```

   启动横幅会打印 `Docker : tcp://192.168.1.23:2375（远程）`，
   页面顶栏胶囊里显示的就是当前地址，点它可切换。

3. 前端热更新：`cd web && npm run dev` → http://localhost:5173
   （vite 已配 `/api`、`/d` 代理到 `127.0.0.1:8080`）

### 注意事项

- **2375 端口没有认证**，等于把 Docker 控制权（约等于 root）开放给能访问该端口的人。
  务必只在可信内网启用，并用防火墙限定来源 IP；不要暴露到公网。
- 数据（上传包、APK、证书、jobs.json）落在**开发机**的 `DATA_DIR`，
  打包机只负责编译，产物通过 `docker cp` 拉回来。
- `docker info` 读到的内存是**打包机**的，Gradle 堆参数会按它自动调，符合预期。
- 切地址前请确认没有任务在构建；服务端也会拦（返回 409），避免构建中途换了机器。

## 安卓原生配置（SDK 版本 / 权限）

上传资源包后，页面上可以单独调整原生配置，不用改 SDK 模板文件：

| 配置 | 默认 | 实现方式 |
|---|---|---|
| `minSdkVersion` | 21 | `override.gradle` 追加 `android.defaultConfig.minSdkVersion` |
| `targetSdkVersion` | 33 | 同上。部分应用商店要求 ≥ 34，可按需调高（调高后要回归测试通知/存储/定位） |
| Android 权限 | SDK aar 自带的 7 条 | 增量写 `<uses-permission>`；移除用 `tools:node="remove"` |

**权限基线不在模板文件里。** 模板 `simpleDemo/src/main/AndroidManifest.xml` 一条
`uses-permission` 都没有，实际 APK 的权限来自 DCloud SDK 的 aar，靠 manifest merger 合并。
所以「移除」必须写 `tools:node="remove"` 交给 merger，直接删模板行是删不掉的。

实测基线（`aapt2 dump permissions` 一个真实产物）：

```
INTERNET / ACCESS_NETWORK_STATE / WRITE_EXTERNAL_STORAGE / READ_EXTERNAL_STORAGE
READ_MEDIA_IMAGES / READ_MEDIA_VIDEO / READ_MEDIA_VISUAL_USER_SELECTED
```

渠道角标类第三方权限（`com.huawei...CHANGE_BADGE`、`com.vivo...BADGE_ICON`、
`com.asus.msa...`、`freemme.permission.msa`）不参与增删。

### 配置自动回填

上传资源包时会读里面的 `manifest.json`，但 **HBuilderX「生成本地打包App资源」导出的
manifest.json 是被裁剪过的**，不含 `app-plus.distribute.android`：

| 上传什么 | 能回填什么 |
|---|---|
| 只要资源包 | **appid / 应用名 / versionName / versionCode** |
| 资源包 + 项目根 `manifest.json` | 追加上 **包名 / minSdk / targetSdk / 权限清单** |

所以页面上单独留了「上传项目 manifest.json」入口——那份是 HBuilderX 项目根目录的完整配置
（带 `app-plus.distribute`），传一次就能把权限和 SDK 版本一起带出来。

两个实现细节：

- **HBuilderX 的 manifest.json 是 JSONC**：允许 `//`、`/* */` 注释和尾随逗号，
  标准 `JSON.parse` 会直接报「不是合法的 JSON」。页面里先按标准 JSON 试，失败自动降级
  去注释 + 去尾随逗号再解析（字符串里的 `https://`、`/* */` 不会被误伤）。
- **包名通常不在本地 manifest 里**（HBuilderX 的包名存在云端配置）。所以 manifest 里没有
  `packagename` 时，会按 appid 从历史构建记录里带出上次用过的包名。

读不到 distribute 时，会按资源包 `manifest.json` 里声明的模块（`Camera` / `Geolocation` /
`Push`…）推测建议权限填进「可额外添加」，页面会提示「请核对后打包」。

回填权限时**只加不减**：manifest 里的 `permissions` 是「云打包要追加的」，不是 APK 权限的
完整清单。离线打包的基线权限来自 SDK aar，本来就存在，如果按清单去删（比如没写
`READ_MEDIA_IMAGES` 就删掉），Android 13+ 保存图片会直接失败。要减请在弹窗里手动取消勾选。

## 目录结构

```
web/                     前端工程（Vue3 + TDesign）
  src/App.vue              骨架：顶栏 / 环境状态 / 主卡片
  src/store.js             全局状态与所有 API 动作（含 SSE、JSONC 解析）
  src/api.js               接口封装
  src/components/          UploadPanel / ConfigPanel / ProgressPanel / LogDialog / LoginView / DockerHostDialog
  dist/                    npm run build 产物（被 go:embed 打进 exe）
android.go               安卓原生配置：基线权限表、可选权限目录、模块→权限建议
dockerapi.go             Docker Engine HTTP API 客户端（不依赖 docker CLI）
dockerhost.go            Docker 地址：规范化、持久化（data/docker.json）、试连
openfolder.go            在本机文件管理器打开产物目录
manifest_test.go         manifest.json 解析与权限白名单的回归测试（go test）
_legacy/removed-2026-11-16/  已下线的 RecordsPanel（打包记录）与 RefillDialog（回填）留档
_legacy/                 旧版无框架前端，仅留档
data/
  uploads/<uploadId>/   上传的原始包
  outputs/<jobId>/      APK + build-info.json
  keystores/            证书文件 + index.json
  icons/                页面上传的图标
  config/               容器内 /workspace/config 的持久化镜像（含自动生成的 auto.jks）
  trash/<jobId>/        回收站（保留 7 天）
  appkeys.json          按 appid 记住的离线打包 appkey
  jobs.json             任务记录（重启后历史仍在，产物目录都还在）
```

## 上传包的生命周期（省磁盘）

| 时机 | 处理 |
|---|---|
| 上传 | 原始 zip 存 `data/uploads/<uploadId>/`，不解压，只读 zip 里的 `manifest.json` 填表 |
| 打包 | `docker cp` 整个 zip 进容器，容器内解压到 `/tmp`，只把解压后的 `www` 打进 APK（zip 本身不进包） |
| **构建成功** | **立刻删掉原包**，释放 20～30MB |
| 构建失败 | 保留原包，方便「一键重试」；重试成功后再删 |
| 删除记录 | 一并删掉原包 |
| **成功构建超过 5 次** | 更早的 **APK 文件**自动删除（只留最近 5 个），记录保留但按钮变成「产物已清理」 |
| 服务启动 | 清理没有任何任务引用的孤儿包（比如上传了但没打包就关页面） |

因此：**已经成功的记录不能重试**（原包没了，重试按钮也不会显示）；失败记录的「重试」可以直接用。

## 注意

- **单任务串行**：Docker 打包很吃资源，同时只跑一个，其余排队。
- **重新构建镜像后要点一次「重启」**：重启是「删掉旧容器再按新镜像建」，这样才会用上新镜像；
  只 `docker restart` 用的还是旧镜像层。
- 证书口令明文存在 `data/keystores/index.json`、appkey 明文存在 `data/appkeys.json`——
  内网工具的设计取舍，**别暴露到公网**。
- 服务重启会把「运行中」的任务标记为中断；已完成的 APK 不受影响，目录照样能打开。
- 「打开文件夹」调的是**服务所在那台机器**的文件管理器：服务跑在本机时最顺手；
  如果服务部署在别的机器上，请到那台机器上看，或者用 `/d/{jobId}/{文件名}` 下载。
- 只做 Android；iOS 仍需要 macOS + Xcode。
