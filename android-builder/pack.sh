#!/usr/bin/env bash
# =====================================================================
# 宿主机一键打包脚本（Git Bash / WSL / macOS / Linux 均可运行）
#
#   源码模式：./pack.sh ../my-uniapp-project -p com.demo.app
#   资源模式：./pack.sh ../dist/build/app   -p com.demo.app
#   环境自检：./pack.sh --doctor
#
# 全部参数见 ./pack.sh --help
# =====================================================================
set -Eeuo pipefail

# Windows Git Bash(MSYS) 默认会把 /workspace/... 这类参数改写成 Windows 路径，
# 会导致 docker -v 挂载串错乱（表现为挂起或直接报错），这里统一禁用。
export MSYS_NO_PATHCONV=1
export MSYS2_ARG_CONV_EXCL='*'

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cd "$HERE"

IMAGE=${IMAGE:-uniapp-android-builder:latest}
GRADLE_VOLUME=${GRADLE_VOLUME:-uniapp_gradle_cache}
PNPM_VOLUME=${PNPM_VOLUME:-uniapp_pnpm_store}
MODE=build

SRC=
OUT=./output
CFG=./config
SDK=
APP_NAME=
VNAME=
VCODE=
PKG=
APPID=
APPKEY=
KS=
SP=
KA=
KP=
USE_CN_MIRROR=${USE_CN_MIRROR:-1}
KEEP_WORK=0
# PACK_COPY=1：不挂 bind mount，改用 docker create + docker cp 传文件。
# 某些 Windows / Docker Desktop 环境首次 bind mount 会卡很久（几十分钟），
# 这个模式可以完全绕开挂载，速度稳定。
PACK_COPY=${PACK_COPY:-0}
ENV_ARGS=()
GRADLE_EXTRA_ARGS=${GRADLE_EXTRA_ARGS:-}

# 按 Docker 实际可用内存自动定 Gradle 堆大小：只给一半左右，
# 否则容器总内存不足时 Gradle daemon 会被直接 OOM kill，
# 报错却是含糊的 "Gradle build daemon disappeared unexpectedly"。
auto_memory_args() {
  local total
  total=$(docker info --format '{{.MemTotal}}' 2>/dev/null | awk '{printf "%d", $1/1024/1024/1024}')
  [ -n "$total" ] && [ "$total" -gt 0 ] || return 0

  local mx
  if   [ "$total" -ge 8 ]; then mx=4096
  elif [ "$total" -ge 6 ]; then mx=3072
  elif [ "$total" -ge 4 ]; then mx=2048
  else                          mx=1024
  fi

  if [ "$total" -lt 4 ]; then
    warn "Docker 可用内存仅 ${total}GB，Android 打包建议 ≥4GB（推荐 8GB）"
    warn "Docker Desktop → Settings → Resources → Memory 调大后再打包会更稳"
  fi
  local workers=4
  [ "$total" -lt 6 ] && workers=2
  GRADLE_EXTRA_ARGS="-Dorg.gradle.jvmargs=-Xmx${mx}m -Dorg.gradle.workers.max=${workers}"
  info "已按 ${total}GB 内存设置 -Xmx${mx}m / ${workers} 并发（可用 GRADLE_EXTRA_ARGS 覆盖）"
}

C_G=$'\033[32m'; C_Y=$'\033[33m'; C_E=$'\033[31m'; C_C=$'\033[36m'; C_R=$'\033[0m'
die() { printf '%s[pack]%s %s\n' "$C_E" "$C_R" "$*" >&2; exit 1; }
log() { printf '%s[pack]%s %s\n' "$C_C" "$C_R" "$*"; }
warn() { printf '%s[pack]%s %s\n' "$C_Y" "$C_R" "$*"; }
info() { printf '%s[pack]%s %s\n' "$C_C" "$C_R" "$*"; }

usage() {
  cat <<'EOF'
用法: pack.sh [选项] <源码目录|本地打包资源目录>

  目录可以是：uni-app 源码根目录 / dist/build/app / HBuilderX 生成的 __UNI__XXX 资源目录

常用选项
  -o, --out DIR          输出 APK 目录（默认 ./output）
  -c, --config DIR       原生配置目录（默认 ./config）
  -s, --sdk DIR          DCloud 离线 SDK 目录（镜像未内置 SDK 时必填）
  -p, --package PKG      applicationId，如 com.demo.app（强烈建议显式指定）
  -n, --name NAME        应用名（默认取 manifest.json 的 name）
  -v, --version VER      versionName（默认取 manifest.json 的 versionName）
      --version-code N   versionCode（默认 1）
      --appid APPID      __UNI__XXXXXXX（默认自动探测）
      --app-key KEY      DCloud 离线打包 appkey（不填真机会报 appkey 错误）

签名
      --keystore FILE    jks/keystore 文件路径
      --store-pass PWD   --key-alias NAME  --key-pass PWD
                         全部缺省时自动生成并保存在 config/keystore/auto.jks

其它
      --cp               用 docker cp 传文件代替 bind mount
                         （Windows 首次挂载卡住时的救命选项，速度稳定）
      --no-cn-mirror     关闭阿里云 Maven 镜像
      --keep             把中间安卓工程留在 ./build-cache 便于排查
      --doctor           打印容器内环境自检
      --shell            直接进入容器 shell（调试）
      --clean            清理容器中间产物
  -h, --help             显示帮助

环境变量：IMAGE / GRADLE_VOLUME / PNPM_VOLUME / USE_CN_MIRROR / GRADLE_EXTRA_ARGS
EOF
  exit 0
}

# ---- 参数解析 -------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    -o|--out)        OUT=$2;  shift 2 ;;
    -c|--config)     CFG=$2;  shift 2 ;;
    -s|--sdk)        SDK=$2;  shift 2 ;;
    -p|--package)    PKG=$2;  shift 2 ;;
    -n|--name)       APP_NAME=$2; shift 2 ;;
    -v|--version)    VNAME=$2;    shift 2 ;;
    --version-code)  VCODE=$2;    shift 2 ;;
    --appid)         APPID=$2;    shift 2 ;;
    --app-key)       APPKEY=$2;   shift 2 ;;
    --keystore)      KS=$2;  shift 2 ;;
    --store-pass)    SP=$2;  shift 2 ;;
    --key-alias)     KA=$2;  shift 2 ;;
    --key-pass)      KP=$2;  shift 2 ;;
    --no-cn-mirror)  USE_CN_MIRROR=0; shift ;;
    --keep)          KEEP_WORK=1;     shift ;;
    --cp)            PACK_COPY=1;     shift ;;
    --doctor)        MODE=doctor;     shift ;;
    --shell)         MODE=shell;      shift ;;
    --clean)         MODE=clean;      shift ;;
    -h|--help)       usage ;;
    --)              shift; SRC=$1; shift; break ;;
    -*)              die "未知参数: $1（试试 --help）" ;;
    *)               SRC=$1; shift ;;
  esac
done

# ---- 路径转换（Windows Git Bash → Docker 可识别路径）---------------
to_docker_path() {
  local p=$1
  if command -v cygpath >/dev/null 2>&1; then
    p=$(cygpath -m "$p" 2>/dev/null || printf '%s' "$p")
  fi
  printf '%s' "$p"
}

require_dir() {
  [ -n "$1" ] || return 0
  mkdir -p "$1" 2>/dev/null || true
}

if [ "$MODE" = build ]; then
  [ -n "$SRC" ] || die "请指定源码/资源目录，或用 --help 查看用法"
  [ -e "$SRC" ] || die "目录/文件不存在: $SRC"
fi

require_dir "$OUT"
require_dir "$CFG"

DOCKER_ARGS=(--rm -e TZ=Asia/Shanghai)

if [ -n "$SRC" ]; then
  DOCKER_ARGS+=(-v "$(to_docker_path "$SRC"):/workspace/src")
fi
DOCKER_ARGS+=(-v "$(to_docker_path "$OUT"):/workspace/output")
DOCKER_ARGS+=(-v "$(to_docker_path "$CFG"):/workspace/config")
if [ -n "$SDK" ]; then
  [ -d "$SDK" ] || die "SDK 目录不存在: $SDK"
  DOCKER_ARGS+=(-v "$(to_docker_path "$SDK"):/workspace/sdk:ro")
fi

if [ "$MODE" = build ] || [ "$MODE" = clean ]; then
  DOCKER_ARGS+=(-v "${GRADLE_VOLUME}:/root/.gradle")
  DOCKER_ARGS+=(-v "${PNPM_VOLUME}:/root/.local/share/pnpm/store")
fi

if [ "$KEEP_WORK" = "1" ]; then
  require_dir ./build-cache
  DOCKER_ARGS+=(-v "$(to_docker_path ./build-cache):/workspace/work")
fi

env_arg() {
  if [ -n "$2" ]; then ENV_ARGS+=(-e "$1=$2"); fi
  return 0
}
env_arg APP_ID            "$APPID"
env_arg APP_KEY           "$APPKEY"
env_arg APP_NAME          "$APP_NAME"
env_arg APP_VERSION_NAME  "$VNAME"
env_arg APP_VERSION_CODE  "$VCODE"
env_arg APP_PACKAGE       "$PKG"
env_arg KEYSTORE_FILE     "$KS"
env_arg STORE_PASSWORD    "$SP"
env_arg KEY_ALIAS         "$KA"
env_arg KEY_PASSWORD      "$KP"
env_arg USE_CN_MIRROR     "$USE_CN_MIRROR"
[ -z "$GRADLE_EXTRA_ARGS" ] && auto_memory_args
env_arg GRADLE_EXTRA_ARGS "$GRADLE_EXTRA_ARGS"
env_arg PACK_IMAGE        "$IMAGE"
DOCKER_ARGS+=("${ENV_ARGS[@]}")

# ---------------------------------------------------------------------
# cp 模式：不挂 bind mount，全部用 docker cp 进出
# ---------------------------------------------------------------------
run_copy_mode() {
  local cname="uniapp-pack-$$"
  docker rm -f "$cname" >/dev/null 2>&1 || true

  local -a C_ARGS=(--name "$cname" -e TZ=Asia/Shanghai)
  C_ARGS+=(-v "${GRADLE_VOLUME}:/root/.gradle")
  C_ARGS+=(-v "${PNPM_VOLUME}:/root/.local/share/pnpm/store")
  C_ARGS+=("${ENV_ARGS[@]}")

  docker create "${C_ARGS[@]}" "$IMAGE" build >/dev/null || die "容器创建失败"

  log "注入源码（docker cp，不走挂载）"
  if [ -f "$SRC" ]; then
    docker cp "$SRC" "$cname":/workspace/src/ >/dev/null
  else
    ( cd "$SRC" && tar cf - . ) | docker cp - "$cname":/workspace/src >/dev/null
  fi
  if [ -n "$(ls -A "$CFG" 2>/dev/null)" ]; then
    ( cd "$CFG" && tar cf - . ) | docker cp - "$cname":/workspace/config >/dev/null
  fi

  log "开始打包"
  docker start -a "$cname"
  local rc=$?

  log "回传产物"
  mkdir -p "$OUT" "$CFG"
  docker cp "$cname":/workspace/output/. "$OUT"/  >/dev/null 2>&1 || true
  docker cp "$cname":/workspace/config/. "$CFG"/  >/dev/null 2>&1 || true

  docker rm -f "$cname" >/dev/null 2>&1 || true
  return $rc
}

if [ "$MODE" = build ] && [ -z "$PKG" ]; then
  warn "未指定 -p/--package，将使用默认包名，上架前请务必显式指定"
fi

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  die "镜像不存在：$IMAGE，请先执行 ./build-image.sh"
fi

log "镜像：$IMAGE   模式：$MODE   传输：$([ "$PACK_COPY" = 1 ] && echo docker-cp || echo bind-mount)"
START=$(date +%s)
if [ "$PACK_COPY" = "1" ] && [ "$MODE" = build ]; then
  run_copy_mode
else
  docker run "${DOCKER_ARGS[@]}" "$IMAGE" "$MODE"
fi
END=$(date +%s)

if [ "$MODE" = build ]; then
  printf '\n%s[pack]%s 总耗时 %ss，产物目录：%s\n' "$C_G" "$C_R" "$((END-START))" "$OUT"
  ls -lh "$OUT"/*.apk 2>/dev/null | tail -5 || true
fi
