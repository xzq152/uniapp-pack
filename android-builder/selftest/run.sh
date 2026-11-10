#!/usr/bin/env bash
# =====================================================================
# 打包链路自检：用一个假的 gradlew 跑完整流程（不联网、不真编译）
# 用于确认「镜像 + 模板解析 + 资源注入 + 配置注入 + 产物收集」都通了。
# 跑通后再放真实 DCloud SDK，失败点就只会剩下 Gradle 本身。
# =====================================================================
set -Eeuo pipefail

# Windows Git Bash 会把 /workspace/... 这类参数自动转成 Windows 路径，必须关掉
export MSYS_NO_PATHCONV=1
export MSYS2_ARG_CONV_EXCL='*'

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cd "$HERE/.."

to_docker_path() {
  local p=$1
  if command -v cygpath >/dev/null 2>&1; then p=$(cygpath -m "$p"); fi
  printf '%s' "$p"
}

echo "==> 运行自检"
docker run --rm \
  -e TZ=Asia/Shanghai \
  -e APP_PACKAGE=com.selftest.demo \
  -e USE_CN_MIRROR=1 \
  -v "$(to_docker_path "$HERE/mock-sdk"):/workspace/sdk:ro" \
  -v "$(to_docker_path "$HERE/mock-app"):/workspace/src:ro" \
  -v "$(to_docker_path "$HERE/../config"):/workspace/config" \
  -v "$(to_docker_path "$HERE/output"):/workspace/output" \
  "${IMAGE:-uniapp-android-builder:latest}" build

echo
echo "==> 产物："
ls -lh "$HERE/output" 2>/dev/null || echo "(空)"
echo
echo "说明：自检用的是「假 gradlew」直接吐出来的假 APK，不是真 zip，"
echo "      因此出现 apksigner 校验未通过属正常现象；只要能看到 APK 与 build-info.json 即代表链路通。"
