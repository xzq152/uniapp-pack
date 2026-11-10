#!/usr/bin/env bash
# =====================================================================
# 从 DCloud 安卓离线 SDK（zip / 已解压目录）中提取打包工程模板
# 用法: prepare-sdk.sh <sdk.zip|sdk目录> <目标模板目录>
# =====================================================================
set -Eeuo pipefail

SRC=${1:?用法: prepare-sdk.sh <sdk.zip|目录> <目标模板目录>}
DEST=${2:?用法: prepare-sdk.sh <sdk.zip|目录> <目标模板目录>}

[ -e "$SRC" ] || { echo "SDK 路径不存在: $SRC" >&2; exit 2; }

TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

ROOT=$SRC
if [ -f "$SRC" ]; then
  case "$SRC" in
    *.zip)          unzip -q "$SRC" -d "$TMP" ;;
    *.tar.gz|*.tgz) tar -xzf "$SRC" -C "$TMP" ;;
    *.tar.xz)       tar -xJf "$SRC" -C "$TMP" ;;
    *) echo "不支持的压缩格式: $SRC" >&2; exit 2 ;;
  esac
  ROOT=$TMP
fi

PROJ=$(find "$ROOT" -maxdepth 6 -type d -name 'HBuilder-Integrate-AS' 2>/dev/null | head -1) || true
if [ -z "$PROJ" ]; then
  echo "在 $SRC 中找不到 HBuilder-Integrate-AS 目录（确认下载的是 DCloud 安卓离线 SDK）" >&2
  exit 3
fi

mkdir -p "$DEST"
# 只清内容不删目录（DEST 有可能是挂载点）
[ -d "$DEST" ] && find "$DEST" -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null || true
cp -a "$PROJ"/. "$DEST"/

# ---- 补齐 aar 依赖 ---------------------------------------------------
# 官方 SDK 的 aar 在 SDK/libs 下，而工程模板里的模块 libs 有时是空的，
# 这里自动补齐，避免 "Could not find uniapp-v8-release.aar"
LIBSRC=$(find "$ROOT" -maxdepth 7 -type d -path '*/SDK/libs' 2>/dev/null | head -1) || true
[ -n "$LIBSRC" ] || LIBSRC=$(find "$ROOT" -maxdepth 7 -type d -name libs 2>/dev/null | grep -v "$DEST" | head -1) || true

mapfile -t MODLIBS < <(find "$DEST" -maxdepth 3 -type d -name libs 2>/dev/null)
if [ "${#MODLIBS[@]}" -eq 0 ]; then
  APPGRADLE=$(grep -l 'com.android.application' "$DEST"/*/build.gradle 2>/dev/null | head -1) || true
  if [ -n "$APPGRADLE" ]; then
    mkdir -p "$(dirname "$APPGRADLE")/libs"
    MODLIBS=("$(dirname "$APPGRADLE")/libs")
  fi
fi

if [ -n "$LIBSRC" ] && [ "${#MODLIBS[@]}" -gt 0 ]; then
  for d in "${MODLIBS[@]}"; do
    shopt -s nullglob
    have=("$d"/*.aar)
    shopt -u nullglob
    if [ "${#have[@]}" -eq 0 ]; then
      cp -a "$LIBSRC"/*.aar "$d"/ 2>/dev/null || true
      n=$(find "$d" -maxdepth 1 -name '*.aar' | wc -l)
      echo "-> 已从 SDK 复制 ${n} 个 aar 到 ${d#"$DEST"/}/"
    fi
  done
fi

# ---- 记录来源，方便排查 ---------------------------------------------
{
  echo "source: $SRC"
  echo "extracted_at: $(date '+%Y-%m-%d %H:%M:%S')"
} > "$DEST/.sdk-source.txt"

echo "-> 离线 SDK 模板已就绪: $DEST"
