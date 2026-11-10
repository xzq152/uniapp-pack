#!/usr/bin/env bash
# =====================================================================
# 构建 uniapp-android-builder 镜像
#
#   ./build-image.sh                 # 默认 tag: uniapp-android-builder:latest
#   ./build-image.sh -t my-img:1.0   # 自定义 tag
#   ./build-image.sh --no-sdk        # 不把 ./sdk 下的压缩包烤进镜像
#
# 常用构建参数（环境变量同名即可覆盖）：
#   BASE_IMAGE                基础镜像（默认 eclipse-temurin:17-jdk-jammy）
#   ANDROID_PLATFORMS         预装的 platform 版本，空格分隔
#   ANDROID_BUILD_TOOLS       预装的 build-tools 版本，空格分隔
#   NODE_VERSION / PNPM_VERSION
#   WITH_JDK11=true           老版本 DCloud SDK（Gradle<7.3）需要时开启
#   APT_MIRROR / NODE_MIRROR / ANDROID_CMDLINE_TOOLS_URL
# =====================================================================
set -Eeuo pipefail

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cd "$HERE"

TAG=${IMAGE:-uniapp-android-builder:latest}
BASE_IMAGE=${BASE_IMAGE:-eclipse-temurin:17-jdk-jammy}
SKIP_SDK=0

C_G=$'\033[32m'; C_Y=$'\033[33m'; C_E=$'\033[31m'; C_C=$'\033[36m'; C_R=$'\033[0m'
die() { printf '%s[build]%s %s\n' "$C_E" "$C_R" "$*" >&2; exit 1; }
log() { printf '%s[build]%s %s\n' "$C_C" "$C_R" "$*"; }

while [ $# -gt 0 ]; do
  case "$1" in
    -t|--tag)   TAG=$2; shift 2 ;;
    --no-sdk)   SKIP_SDK=1; shift ;;
    -h|--help)  sed -n '2,20p' "$0"; exit 0 ;;
    *)          die "未知参数: $1" ;;
  esac
done

command -v docker >/dev/null 2>&1 || die "没有找到 docker 命令"

# 统计 sdk/ 下的压缩包
SDK_ITEMS=0
shopt -s nullglob
pkgs=(sdk/*.zip sdk/*.tar.gz sdk/*.tgz sdk/*.tar.xz)
SDK_ITEMS=${#pkgs[@]}
shopt -u nullglob

# 只在环境变量显式给出时才覆盖 Dockerfile 里的 ARG 默认值，
# 避免「脚本与 Dockerfile 两处默认值」悄悄不一致（曾经导致 platform 没升级）
BUILD_ARGS=()
opt_arg() {
  # opt_arg <环境变量名> <build-arg名>
  if [ -n "${!1:-}" ]; then BUILD_ARGS+=(--build-arg "$2=${!1}"); fi
}
opt_arg BASE_IMAGE               BASE_IMAGE
opt_arg ANDROID_PLATFORMS        ANDROID_PLATFORMS
opt_arg ANDROID_BUILD_TOOLS      ANDROID_BUILD_TOOLS
opt_arg NODE_VERSION             NODE_VERSION
opt_arg PNPM_VERSION             PNPM_VERSION
opt_arg WITH_JDK11               WITH_JDK11
opt_arg NPM_REGISTRY             NPM_REGISTRY
opt_arg APT_MIRROR               APT_MIRROR
opt_arg NODE_MIRROR              NODE_MIRROR
opt_arg ANDROID_CMDLINE_TOOLS_URL ANDROID_CMDLINE_TOOLS_URL

if [ "$SKIP_SDK" = "1" ]; then
  TMPCTX=$(mktemp -d)
  trap 'rm -rf "$TMPCTX"' EXIT
  cp Dockerfile "$TMPCTX"/
  cp -r docker "$TMPCTX"/
  mkdir -p "$TMPCTX/sdk"
  CTX=$TMPCTX
  printf '%s[build]%s --no-sdk：镜像不含 DCloud SDK，运行时请用 -s 挂载\n' "$C_Y" "$C_R"
else
  CTX=.
  if [ "$SDK_ITEMS" -eq 0 ]; then
    printf '%s[build]%s 提示：sdk/ 目录为空。\n' "$C_Y" "$C_R"
    printf '        请先到 https://nativesupport.dcloud.net.cn/AppDocs/download/android\n'
    printf '        下载与 HBuilderX 版本匹配的「Android 离线 SDK」，把 zip 放到 %s/sdk/ 下再构建，\n' "$HERE"
    printf '        否则镜像仍可构建，但运行时需要通过 ./pack.sh -s <SDK目录> 挂载。\n'
  else
    log "发现 ${SDK_ITEMS} 个 SDK 压缩包，将被烤入镜像"
  fi
fi

log "开始构建 $TAG"
docker build -t "$TAG" --provenance=false "${BUILD_ARGS[@]}" -f Dockerfile "$CTX"
printf '%s[build]%s 构建完成：%s\n' "$C_G" "$C_R" "$TAG"
docker images "$TAG" --format '        镜像大小：{{.Size}}'
