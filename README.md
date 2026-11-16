# uni-app 安卓本地打包服务（离线打包）

局域网内使用的 uni-app Android 打包服务：同事在浏览器里 **上传资源包 → 填包名/证书 → 看进度 → 打开产物文件夹**，
不需要装 HBuilderX、不需要碰命令行。底层复用一份 Docker 打包镜像（DCloud 离线 SDK + Gradle）。

---

## 一分钟了解架构

```
┌──────────────┐   上传 zip/文件夹   ┌────────────────────────┐   docker cp / exec   ┌─────────────────────┐
│ 局域网电脑    │ ─────────────────► │  Go 服务 :8080          │ ──────────────────► │ uniapp-pack-runtime  │
│ 浏览器       │ ◄───────────────── │  Vue3 + TDesign 前端     │ ◄────────────────── │ 容器（离线 SDK+Gradle）│
└──────────────┘  SSE 进度 / 打开目录 └────────────────────────┘   产物 APK            └─────────────────────┘
                                            │
                                            └── 数据落盘：uploads / outputs / keystores / icons / splashes
```

三个组成部分：

| 部分 | 位置 | 说明 |
|---|---|---|
| **Go 服务** | `pack-server/*.go` | 单文件二进制，前端静态资源用 `go:embed` 打进去；无第三方依赖 |
| **前端** | `pack-server/web/` | Vue 3 + TDesign Vue Next + Vite，构建产物 `web/dist` 被嵌入 exe |
| **打包镜像** | `android-builder/` | Dockerfile + entrypoint.sh，内含 DCloud 离线 SDK、Android SDK、Gradle |

---

## 快速开始（换一台机器）

```bash
# 1) 环境：Go 1.22+、Node 18+、Docker Desktop（WSL2）
#    详细要求见 docs/01-环境与依赖.md

# 2) 先构建打包镜像（约 3GB，首次较慢）
cd android-builder && ./build-image.sh

# 3) 再构建服务（前端 + Go + 启动器）
cd ../pack-server && build.bat        # Windows 双击即可

# 4) 运行
双击 启动服务.exe     # 无窗口，右下角托盘常驻；双击托盘图标打开页面
```

打开 <http://127.0.0.1:8080>，点右上「启动环境」后即可上传打包。
打完后点完成区的「打开文件夹」，直接在本机文件管理器定位到 `data/outputs/<jobId>/`。

---

## 远程 Docker（Docker 不在本机）

服务只走 Docker Engine 的 HTTP API，**本机不需要装 docker 命令行**，所以「服务在 A 机、Docker 在 B 机」
完全支持：停止 / 启动 / 重建 都是直接打到 B 机的 API，行为和 Docker 在本机时一致。

**在页面上切换（推荐）**

顶栏状态胶囊里显示的就是当前 Docker 地址（本机显示「本机」，远程显示 `IP:端口`），点它打开切换面板：

- 填地址 → 「测试连接」只试连（显示 Docker 版本与可用内存）→「切换」正式生效
- 写法宽松：`192.168.1.23`（自动补 2375）、`192.168.1.23:2375`、`http://192.168.1.23:2375` 都认
- 留空点「切换」= 回到本机 Docker
- 切换**先试连，连不通就拒绝**，不会把服务打到不可用的地址上
- 有任务在构建（`running`/`queued`）时不允许切换，避免构建中途换机器
- 切换后写入 `data/docker.json`，**重启服务仍然生效**

**用环境变量（启动时固定）**

```bat
set "DOCKER_HOST=tcp://192.168.1.23:2375"
```

> 优先级：`data/docker.json`（页面切换过的）> 环境变量 `DOCKER_HOST` > 本机 socket。

**对方机器要开放 TCP 2375**

- Docker Desktop：Settings → General → 勾选
  「Expose daemon on tcp://localhost:2375 without TLS」
- Linux：`-H tcp://0.0.0.0:2375`（无 TLS 的明文端口）
- 防火墙放行 2375，可用 `curl http://192.168.1.23:2375/version` 自查

> **安全提醒**：2375 无鉴权，等于把 Docker 控制权交出去，务必只在隔离内网使用，不要暴露到公网。

> Windows 本机 Docker 走命名管道（npipe），本项目只实现了 unix socket / TCP，
> 所以 Windows 上「本机」不可用——请填远程地址。

---

## 文档目录

| 文档 | 内容 |
|---|---|
| [docs/01-环境与依赖.md](docs/01-环境与依赖.md) | 需要的软件、版本、安装与自检命令 |
| [docs/02-架构与数据流.md](docs/02-架构与数据流.md) | 组件职责、一次打包的完整链路、数据目录 |
| [docs/03-构建与运行.md](docs/03-构建与运行.md) | 镜像/前端/服务怎么构建，托盘与计划任务怎么配 |
| [docs/04-实现要点.md](docs/04-实现要点.md) | 关键实现：嵌入资源、SSE、渠道注入、启动图 .9.png、清理策略 |
| [docs/05-接口与环境变量.md](docs/05-接口与环境变量.md) | HTTP 接口清单、环境变量表 |
| [docs/06-踩坑清单.md](docs/06-踩坑清单.md) | 已经踩过的坑，改代码前先看这个 |

---

## 目录结构

```
uniapp-pack/
├── README.md                 ← 本文件
├── docs/                     ← 开发文档（环境、架构、构建、要点、接口、踩坑）
├── pack-server/              ← Go 服务 + 前端 + 启动器
│   ├── main.go               入口：配置、路由、静态资源、启动清理
│   ├── api.go                所有 HTTP 接口
│   ├── build.go              打包流程编排（docker cp / exec / 产物收集）
│   ├── store.go              数据层（任务、证书、回收站、清理策略）
│   ├── runtime.go            Docker 环境管理（启动/停止/重建容器）
│   ├── dockerapi.go          Docker Engine HTTP API 客户端（不依赖 docker CLI）
│   ├── dockerhost.go         Docker 地址：规范化、持久化（data/docker.json）、试连
│   ├── openfolder.go         在本机文件管理器打开产物目录
│   ├── auth.go               访问口令（可选）
│   ├── go.mod                module uniapp-pack-server（零第三方依赖）
│   ├── cmd/launcher/         托盘宿主（启动服务 + 托盘图标 + 右键菜单）
│   ├── cmd/stopper/          停止器
│   ├── web/                  Vue3 + TDesign 前端源码
│   ├── build.bat             一键构建（npm build → go build → 启动器）
│   └── start.bat             只跑服务（前台，调试看日志用）
└── android-builder/          ← 打包镜像
    ├── Dockerfile
    ├── build-image.sh        构建 uniapp-android-builder:latest
    ├── docker/
    │   ├── entrypoint.sh     容器内主流程（注入资源/图标/启动图/渠道 → gradle → 收集）
    │   ├── NinePatch.java    普通 png → .9.png 转换工具
    │   ├── init.gradle       国内镜像加速
    │   └── prepare-sdk.sh    DCloud 离线 SDK 预处理
    └── config/               镜像内置的 Gradle/SDK 配置片段
```

> 运行期数据默认在服务目录下的 `data/`（可用 `DATA_DIR` 指到别的盘）。
> 里面含**私钥 `.jks`、明文证书口令、DCloud appkey 和产物 APK**，已在 `.gitignore` 中排除，别提交。
> `data/docker.json` 记录页面上切换过的 Docker 地址（优先于 `DOCKER_HOST`）。
> 本机当前部署在 `D:\uniapp-pack-go`，`E:\uniapp-pack` 是代码归档副本。
