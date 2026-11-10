#!/usr/bin/env bash
# =====================================================================
# uni-app 安卓离线打包 · 容器入口
#
# 用法:  entrypoint.sh [build|doctor|shell|clean]
#  build  : 默认。由环境变量驱动（见 README「环境变量」）
#  doctor : 打印容器内环境自检信息
#  shell  : 进入容器 shell（调试用）
#  clean  : 清理中间产物目录
# =====================================================================
set -Eeuo pipefail

# ------------------------------------------------------------------
# 静默死亡防御
# 本脚本大量依赖 grep/find 管道，而它们「没匹配到」时返回 1，
# 叠加 set -e + pipefail 会让脚本无声退出（极难排查）。
# 因此：① 可能被 $(...) 捕获的管道一律加 || true；② 用 ERR trap 兜底报出出错行号。
# ------------------------------------------------------------------
trap 'ec=$?; printf "\n%s✗ 意外中断：第 %s 行，退出码 %s%s\n" "$C_E" "$LINENO" "$ec" "$C_R" >&2' ERR

# ---------- 终端着色 ----------
if [ "${PACK_NO_COLOR:-0}" = "0" ]; then
  C_R=$'\033[0m'; C_C=$'\033[36m'; C_G=$'\033[32m'; C_Y=$'\033[33m'; C_E=$'\033[31m'; C_D=$'\033[2m'
else
  C_R=; C_C=; C_G=; C_Y=; C_E=; C_D=
fi
_ts()   { date '+%H:%M:%S'; }
log()   { printf '%s[%s]%s %s\n' "$C_C" "$(_ts)" "$C_R" "$*"; }
step()  { printf '\n%s━━ %s%s\n' "$C_C" "$*" "$C_R"; }
ok()    { printf '   %s✓%s %s\n' "$C_G" "$C_R" "$*"; }
info()  { printf '   %s%s%s\n' "$C_D" "$*" "$C_R"; }
warn()  { printf '   %s!%s %s\n' "$C_Y" "$C_R" "$*"; }
die()   { printf '\n%s✗ %s%s\n' "$C_E" "$*" "$C_R" >&2; exit 1; }

# ---------- 路径 / 环境变量 ----------
SRC_DIR=${SRC_DIR:-/workspace/src}
OUT_DIR=${OUT_DIR:-/workspace/output}
CFG_DIR=${CFG_DIR:-/workspace/config}
SDK_DIR=${SDK_DIR:-/workspace/sdk}
# 注意：目录名不能以 '.' 开头——SDK 的 settings.gradle 通常没有 rootProject.name，
# Gradle 会退化成「用目录名当工程名」，点号开头会直接报错。
WORK_DIR=${WORK_DIR:-/workspace/work}
TPL_BAKED=${TPL_BAKED:-/opt/dcloud/template}
ANDROID_SDK_ROOT=${ANDROID_SDK_ROOT:-/opt/android-sdk}
USE_CN_MIRROR=${USE_CN_MIRROR:-1}
GRADLE_EXTRA_ARGS=${GRADLE_EXTRA_ARGS:-}

APP_ID=${APP_ID:-}
APP_NAME=${APP_NAME:-}
APP_VERSION_NAME=${APP_VERSION_NAME:-}
APP_VERSION_CODE=${APP_VERSION_CODE:-}
APP_PACKAGE=${APP_PACKAGE:-}
# DCloud 离线打包 appkey（dev.dcloud.net.cn → 应用 → 各平台信息 → Android 离线打包 key）
# 不填就会在真机上提示「appkey未配置或配置错误」
APP_KEY=${APP_KEY:-}
# 启动图底色：auto = 取图标四角颜色，也可写 #RRGGBB
SPLASH_BG=${SPLASH_BG:-auto}
# 启动图高度（宽度固定 1080）：默认 2400 = 20:9，贴近现在主流手机
SPLASH_HEIGHT=${SPLASH_HEIGHT:-2400}
KEYSTORE_FILE=${KEYSTORE_FILE:-}
STORE_PASSWORD=${STORE_PASSWORD:-}
KEY_ALIAS=${KEY_ALIAS:-}
KEY_PASSWORD=${KEY_PASSWORD:-}

START_TS=$(date +%s)
elapsed() { local e=$(( $(date +%s) - START_TS )); printf '%dm%02ds' $((e/60)) $((e%60)); }

# =====================================================================
doctor() {
  step "环境自检 (doctor)"
  echo "   image     : ${PACK_IMAGE:-uniapp-android-builder:latest}"
  echo "   java      : $(java -version 2>&1 | head -1)"
  echo "   JAVA_HOME : ${JAVA_HOME:-<未设置>}"
  echo "   node      : $(node -v 2>/dev/null || echo '-')  npm: $(npm -v 2>/dev/null || echo '-')  pnpm: $(pnpm -v 2>/dev/null || echo '-')"
  echo "   sdk root  : $ANDROID_SDK_ROOT"
  echo "   platforms : $(ls -1 "$ANDROID_SDK_ROOT/platforms" 2>/dev/null | tr '\n' ' ')"
  echo "   buildtool : $(ls -1 "$ANDROID_SDK_ROOT/build-tools" 2>/dev/null | tr '\n' ' ')"
  echo "   gradle    : $(gradle --version 2>/dev/null | grep -m1 Gradle || echo '(仅使用 wrapper)')"
  echo "   内置模板  : $([ -f "$TPL_BAKED/settings.gradle" ] && echo "已内置" || echo "未内置（需挂载 /workspace/sdk）")"
  echo "   挂载的SDK : $(ls -A "$SDK_DIR" 2>/dev/null | tr '\n' ' ' )"
  echo "   源码目录  : $(ls -A "$SRC_DIR" 2>/dev/null | head -8 | tr '\n' ' ')"
  echo "   配置目录  : $(ls -A "$CFG_DIR" 2>/dev/null | tr '\n' ' ')"
  echo "   输出目录  : $OUT_DIR"
  echo "   gradle缓存: $(du -sh "${GRADLE_USER_HOME:-/root/.gradle}" 2>/dev/null | cut -f1)"
}

# =====================================================================
resolve_template() {
  step "准备离线 SDK 模板"
  if [ -f "${TPL_BAKED}/settings.gradle" ]; then
    TPL_DIR=$TPL_BAKED
    ok "使用镜像内置模板 ${TPL_DIR}  ($(cat "${TPL_BAKED}/.sdk-source.txt" 2>/dev/null | head -1 || echo '来源未知'))"
    return
  fi
  [ -d "$SDK_DIR" ] || die "镜像未内置离线 SDK，且未挂载 SDK 目录到 $SDK_DIR"
  local first
  first=$(find "$SDK_DIR" -maxdepth 1 -mindepth 1 \
            \( -name '*.zip' -o -name '*.tar.gz' -o -name '*.tgz' -o -name '*.tar.xz' \
               -o -name 'HBuilder-Integrate-AS' -o -name 'Android-SDK*' \) 2>/dev/null | sort | head -1) || true
  [ -n "$first" ] || die "$SDK_DIR 下没有找到 zip / Android-SDK* / HBuilder-Integrate-AS"
  TPL_DIR=/opt/dcloud/runtime-template
  log "从 $(basename "$first") 提取模板…"
  /opt/pack/prepare-sdk.sh "$first" "$TPL_DIR" || die "SDK 解析失败"
  [ -f "${TPL_DIR}/settings.gradle" ] || die "SDK 解析后缺少 settings.gradle"
  ok "模板就绪：${TPL_DIR}"
}

# =====================================================================
detect_module() {
  local root=$1 m=""
  if [ -f "$root/settings.gradle" ]; then
    m=$(grep -oE "include[[:space:]]*\(?[[:space:]]*['\"]:[A-Za-z0-9_.-]+" "$root/settings.gradle" 2>/dev/null \
        | head -1 | sed -E "s/.*['\"]://") || true
  fi
  if [ -z "$m" ]; then
    m=$(grep -l 'com.android.application' "$root"/*/build.gradle 2>/dev/null | head -1 \
        | sed -E 's#/build\.gradle$##; s#.*/##') || true
  fi
  printf '%s' "${m:-app}"
}

find_www() {
  local s=$1 c
  for c in \
      "$s/www" \
      "$s" \
      "$s"/unpackage/resources/__UNI__*/www \
      "$s"/__UNI__*/www \
      "$s"/*/www \
      "$s"/dist/build/app \
      "$s"/dist/resources/__UNI__*/www ; do
    [ -f "$c/app-service.js" ] && { printf '%s' "$c"; return 0; }
  done
  return 1
}

json_get() {
  # json_get <manifest.json>  → 依次输出 id / name / versionName / versionCode（4 行）
  node -e '
    const fs=require("fs");
    let m={};
    try { m=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); } catch(e) {}
    const g=o=>String(o===undefined||o===null?"":o);
    console.log([g(m.id),g(m.name),g(m.versionName),g(m.versionCode)].join("\n"));
  ' "$1" 2>/dev/null || printf '\n\n\n\n'
}

read_prop() {
  [ -f "$1" ] || return 0
  sed -n -E "s/^$2[[:space:]]*=[[:space:]]*(.*)$/\1/p" "$1" | head -1
}

sanitize() { printf '%s' "$1" | tr ' /\\:*?"<>|\t' '__________'; }

# 输入可能是压缩包（HBuilderX「生成本地打包App资源」或直接传过来的 zip），
# 支持两种形态：① SRC_DIR 本身就是 zip  ② SRC_DIR 目录里只有这一个 zip
maybe_unpack() {
  local s=$1 out=/tmp/uniapp-src zips
  local -a found=()
  # 注意：本函数会被 $(...) 捕获，返回值必须是「干净的一行路径」，
  # 所以 unpack_to 的日志一律重定向到 stderr，否则路径会被日志污染
  if [ -f "$s" ]; then
    unpack_to "$s" "$out" >&2 || return 1
    printf '%s' "$out"; return
  fi
  if [ -d "$s" ]; then
    while IFS= read -r f; do found+=("$f"); done < <(find "$s" -maxdepth 1 -type f \( -name '*.zip' -o -name '*.tar.gz' \) 2>/dev/null)
    if [ "${#found[@]}" -eq 1 ] && ! find_www "$s" >/dev/null 2>&1; then
      unpack_to "${found[0]}" "$out" >&2 || return 1
      printf '%s' "$out"; return
    fi
  fi
  printf '%s' "$s"
}

unpack_to() {
  local src=$1 out=$2
  rm -rf "$out"; mkdir -p "$out"
  case "$src" in
    *.zip)          unzip -q "$src" -d "$out" ;;
    *.tar.gz|*.tgz) tar -xzf "$src" -C "$out" ;;
    *.tar.xz)       tar -xJf "$src" -C "$out" ;;
    *)              return 1 ;;
  esac
  ok "已解压 $(basename "$src")"
}

# 清空目录内容但不删目录本身（目录可能是挂载点，rm -rf 会报 Device or resource busy）
clear_dir() {
  local d=$1
  [ -d "$d" ] || return 0
  find "$d" -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null || true
}

# =====================================================================
build_from_source() {
  step "编译 uni-app 前端资源"
  cd "$SRC_DIR"
  local pm=npm
  [ -f pnpm-lock.yaml ] && pm=pnpm
  if [ -f yarn.lock ] && [ ! -f pnpm-lock.yaml ]; then pm=yarn; fi
  log "包管理器：$pm"
  if [ ! -d node_modules ]; then
    log "安装依赖…（首次较慢，建议用 pack.sh 挂载 pnpm store 复用）"
    $pm install || die "依赖安装失败"
  else
    ok "复用已有 node_modules"
  fi
  local script
  script=$(node -e '
    const s=(require("./package.json").scripts)||{};
    for (const k of ["build:app","build:app-plus","build:app-android"]) { if (s[k]) { console.log(k); break; } }
  ' 2>/dev/null || true)
  if [ -n "$script" ]; then
    log "执行：$pm run $script"
    $pm run "$script" || die "前端构建失败"
  elif [ -f node_modules/@dcloudio/vite-plugin-uni/bin/uni.js ]; then
    log "回退：uni build -p app"
    node node_modules/@dcloudio/vite-plugin-uni/bin/uni.js build -p app || die "前端构建失败"
  else
    die "package.json 里没有 build:app 脚本：请添加 \"build:app\": \"uni build -p app\"，或改用「本地打包资源」模式把 dist/build/app 挂进来"
  fi
}

# =====================================================================
ensure_sdk_pkgs() {
  step "校验 Android SDK 组件"
  local files=("$APPDIR/build.gradle")
  [ -f "$WORK_DIR/build.gradle" ] && files+=("$WORK_DIR/build.gradle")
  local plats bt need=0 p
  # 单引号、双引号两种写法都要认（DCloud 模板用的是单引号）；尾部 || true 防止没命中就静默退出
  plats=$(grep -hoE "(compileSdk|compileSdkVersion|targetSdkVersion)[[:space:]]*[= ]?[[:space:]]*(android-)?[0-9]+" \
          "${files[@]}" 2>/dev/null | grep -oE "[0-9]+$" | sort -u) || true
  bt=$(grep -hoE "buildToolsVersion[[:space:]]*[= ]?[[:space:]]*['\"][0-9.]+['\"]" "${files[@]}" 2>/dev/null \
       | grep -oE "[0-9]+\.[0-9]+\.[0-9]+" | sort -u | head -1) || true
  for p in $plats; do
    if [ ! -d "$ANDROID_SDK_ROOT/platforms/android-$p" ]; then
      warn "缺少 platform android-$p，正在安装…"
      sdkmanager --sdk_root="$ANDROID_SDK_ROOT" "platforms;android-$p" >/dev/null
      need=1
    fi
  done
  if [ -n "$bt" ] && [ ! -d "$ANDROID_SDK_ROOT/build-tools/$bt" ]; then
    warn "缺少 build-tools $bt，正在安装…"
    sdkmanager --sdk_root="$ANDROID_SDK_ROOT" "build-tools;$bt" >/dev/null
    need=1
  fi
  printf 'sdk.dir=%s\n' "$ANDROID_SDK_ROOT" > "$WORK_DIR/local.properties"
  if [ "$need" = "1" ]; then ok "已补齐缺失组件"; else ok "组件齐全"; fi
}

# =====================================================================
gen_override() {
  cat > "$WORK_DIR/override.gradle" <<'GROOVY'
// >>> uniapp-android-builder 自动注入，勿手工修改 <<<
def _cfg = android.signingConfigs.findByName("release")
if (_cfg == null) { _cfg = android.signingConfigs.create("release") }
_cfg.storeFile     = new File("__KEYSTORE__")
_cfg.storePassword = "__STOREPASS__"
_cfg.keyAlias      = "__KEYALIAS__"
_cfg.keyPassword   = "__KEYPASS__"
_cfg.v1SigningEnabled = true
_cfg.v2SigningEnabled = true

android.defaultConfig.applicationId = "__PACKAGE__"
android.defaultConfig.versionCode   = __VCODE__
android.defaultConfig.versionName   = "__VNAME__"

android.buildTypes.each { bt ->
    if (bt.name == "release") { bt.signingConfig = _cfg }
}
GROOVY

  local esc_for_sed
  esc_for_sed() { printf '%s' "$1" | sed -e 's/[&\\]/\\&/g'; }

  local KS=$(esc_for_sed "$KEYSTORE_FILE")
  local SP=$(esc_for_sed "$STORE_PASSWORD")
  local KA=$(esc_for_sed "$KEY_ALIAS")
  local KP=$(esc_for_sed "$KEY_PASSWORD")
  local PK=$(esc_for_sed "$APP_PACKAGE")
  local VN=$(esc_for_sed "$APP_VERSION_NAME")

  sed -i \
    -e "s#__KEYSTORE__#${KS}#" \
    -e "s#__STOREPASS__#${SP}#" \
    -e "s#__KEYALIAS__#${KA}#" \
    -e "s#__KEYPASS__#${KP}#" \
    -e "s#__PACKAGE__#${PK}#" \
    -e "s#__VCODE__#${APP_VERSION_CODE}#" \
    -e "s#__VNAME__#${VN}#" \
    "$WORK_DIR/override.gradle"

  grep -q "override.gradle" "$APPDIR/build.gradle" 2>/dev/null || \
    printf '\n// >>> uniapp-android-builder >>>\napply from: "%s"\n' "$WORK_DIR/override.gradle" >> "$APPDIR/build.gradle"
}

# =====================================================================
apply_config() {
  step "应用自定义配置"
  local RES="$APPDIR/src/main/res"
  local DATA="$APPDIR/src/main/assets/data"

  if [ -f "$CFG_DIR/AndroidManifest.xml" ]; then
    cp -f "$CFG_DIR/AndroidManifest.xml" "$APPDIR/src/main/AndroidManifest.xml"; ok "AndroidManifest.xml"
  fi
  if [ -d "$CFG_DIR/data" ]; then
    mkdir -p "$DATA"; cp -a "$CFG_DIR"/data/. "$DATA"/; ok "assets/data"
  fi
  if [ -d "$CFG_DIR/res" ]; then
    cp -a "$CFG_DIR"/res/. "$RES"/; ok "res 覆盖目录"
  fi
  local f
  for f in icon splash push; do
    if [ -f "$CFG_DIR/branding/$f.png" ]; then
      mkdir -p "$RES/drawable"; cp -f "$CFG_DIR/branding/$f.png" "$RES/drawable/$f.png"; ok "branding/$f.png"
    fi
  done

  # 应用名
  if [ -f "$CFG_DIR/strings.xml" ]; then
    cp -f "$CFG_DIR/strings.xml" "$RES/values/strings.xml"
  fi
  local STR="$RES/values/strings.xml"
  if [ -f "$STR" ]; then
    local safe_name
    safe_name=$(printf '%s' "$APP_NAME" | sed -e 's/[&\\]/\\&/g')
    if grep -q 'name="app_name"' "$STR"; then
      sed -i -E "s#(<string name=\"app_name\">)[^<]*(</string>)#\1${safe_name}\2#" "$STR"
    else
      sed -i -E "s#(</resources>)#    <string name=\"app_name\">${safe_name}</string>\n\1#" "$STR"
    fi
    ok "应用名称 → $APP_NAME"
  else
    warn "未找到 res/values/strings.xml，跳过应用名设置"
  fi

  # dcloud_control.xml：appid 必须与 assets/apps/<appid> 目录名一致
  local DCTL="$DATA/dcloud_control.xml"
  if [ -f "$DCTL" ]; then
    sed -i -E "s#appid=\"[^\"]*\"#appid=\"${APP_ID}\"#; s#appver=\"[^\"]*\"#appver=\"${APP_VERSION_NAME}\"#" "$DCTL"
    ok "dcloud_control.xml → appid=$APP_ID appver=$APP_VERSION_NAME"
  else
    warn "未找到 assets/data/dcloud_control.xml"
  fi
}

# =====================================================================
# 注入 dcloud_appkey（离线打包必须，否则真机报「appkey未配置或配置错误」）
# =====================================================================
inject_appkey() {
  step "注入离线打包 appkey"
  local M="$APPDIR/src/main/AndroidManifest.xml"
  [ -f "$M" ] || { warn "未找到 AndroidManifest.xml，跳过"; return 0; }

  if [ -z "$APP_KEY" ]; then
    warn "未提供 appkey —— 真机会提示「appkey未配置或配置错误」"
    warn "去 dev.dcloud.net.cn → 应用 → 各平台信息 → Android 离线打包 key 复制过来"
    return 0
  fi

  if grep -q 'dcloud_appkey' "$M"; then
    APP_KEY="$APP_KEY" perl -0777 -i -pe \
      's#<meta-data\s+android:name="dcloud_appkey"\s+android:value="[^"]*"\s*/>#qq{<meta-data android:name="dcloud_appkey" android:value="$ENV{APP_KEY}" />}#se' \
      "$M"
  else
    APP_KEY="$APP_KEY" perl -0777 -i -pe \
      's#</application>#qq{    <meta-data android:name="dcloud_appkey" android:value="$ENV{APP_KEY}" />\n    </application>}#e' \
      "$M"
  fi

  if grep -qF "android:value=\"$APP_KEY\"" "$M"; then
    ok "dcloud_appkey 已写入"
  else
    warn "appkey 写入失败，请检查 AndroidManifest.xml"
  fi
}

# =====================================================================
# 应用图标：优先资源包自带的 app_icons（HBuilderX 打出的最大尺寸），
# 其次页面上传的 icon.png，都没有就沿用 SDK 模板图标
# =====================================================================
detect_icon_from_www() {
  local dir="$WWW/static/app_icons" best=""
  if [ -d "$dir" ]; then
    # 文件名形如 1024x1024.png：按第一段数字取最大（直接对路径排序会因为前缀非数字而失效）
    local f b n
    best=$(
      for f in "$dir"/*.png; do
        [ -e "$f" ] || continue
        b=$(basename "$f")
        n=${b%%x*}
        n=${n//[!0-9]/}
        printf '%06d\t%s\n' "${n:-0}" "$f"
      done | sort -n | tail -1 | cut -f2-
    )
  fi
  if [ -z "$best" ] && [ -f "$WWW/static/logo.png" ]; then
    best="$WWW/static/logo.png"
  fi
  printf '%s' "$best"
}

inject_icon() {
  step "应用图标"
  local RES="$APPDIR/src/main/res"
  local DST="$RES/drawable/icon.png"
  local src=""

  if [ -f "$CFG_DIR/icon.png" ]; then
    src="$CFG_DIR/icon.png"; ok "图标：页面上传"
  elif [ -f "$CFG_DIR/branding/icon.png" ]; then
    src="$CFG_DIR/branding/icon.png"; ok "图标：config/branding/icon.png"
  else
    src=$(detect_icon_from_www)
    if [ -n "$src" ] && [ -f "$src" ]; then
      ok "图标：资源包 $(basename "$src")"
    else
      src=""
      warn "资源包内未找到 app_icons/*.png，沿用 SDK 默认图标"
    fi
  fi

  if [ -n "$src" ]; then
    mkdir -p "$RES/drawable"; cp -f "$src" "$DST"
  fi
}

# 启动图优先级：页面上传 > config/branding > 用图标合成。
# 统一转成 .9.png（运行时检测 nine-patch chunk 后用 NinePatchDrawable 绘制，
# 只有背景拉伸、内容区锁定，任意屏幕不变形）。
inject_splash() {
  # $1 = APPDIR（do_build 里的局部变量，必须显式传入）
  local APPDIR="$1"
  step "启动图"
  local RES="$APPDIR/src/main/res"
  local NODPI="$RES/drawable-nodpi"
  mkdir -p "$NODPI"

  # 官方云打包的 APK 里没有任何启动图资源——不提供 splash 位图时，
  # runtime 会用原生方式渲染「通用启动界面」（圆形应用图标 + 加载进度 + 应用名称，
  # 自动适配所有屏幕，进度圈是动画）。因此：
  #   1) 未上传自定义启动图 → 删掉模板的 splash.png，交给 runtime 原生渲染（官方行为）
  #   2) 上传了自定义启动图 → 普通 png 自动转 .9.png（背景拉伸、内容区锁定）；
  #      直接上传 .9.png 则原样使用
  local custom=""
  if [ -f "$CFG_DIR/splash.9.png" ]; then
    custom="$CFG_DIR/splash.9.png"; is9=1
  elif [ -f "$CFG_DIR/splash.png" ]; then
    custom="$CFG_DIR/splash.png"; is9=0
  elif [ -f "$CFG_DIR/branding/splash.png" ]; then
    custom="$CFG_DIR/branding/splash.png"; is9=0
  fi

  if [ -z "$custom" ]; then
    rm -f "$RES/drawable/splash.png" "$NODPI/splash.9.png"
    ok "未提供自定义启动图 → 使用官方通用启动界面（原生渲染，自动适配屏幕）"
    return 0
  fi

  rm -f "$RES/drawable/splash.png"
  if [ "$is9" = "1" ]; then
    cp -f "$custom" "$NODPI/splash.9.png"
    ok "自定义启动图：使用 .9.png"
  else
    if java -Djava.awt.headless=true -cp /opt/pack NinePatch "$custom" "$NODPI/splash.9.png" >/dev/null 2>&1; then
      ok "自定义启动图：已转为 .9.png（背景拉伸、内容区锁定）"
    else
      cp -f "$custom" "$NODPI/splash.png"
      warn ".9 转换失败，退回普通位图（可能拉伸）"
    fi
  fi
}

# =====================================================================
prepare_keystore() {
  step "签名配置"
  mkdir -p "$CFG_DIR/keystore"
  local PROPS="$CFG_DIR/keystore/keystore.properties"
  [ -n "$STORE_PASSWORD" ] || STORE_PASSWORD=$(read_prop "$PROPS" storePassword)
  [ -n "$KEY_ALIAS" ]      || KEY_ALIAS=$(read_prop "$PROPS" keyAlias)
  [ -n "$KEY_PASSWORD" ]   || KEY_PASSWORD=$(read_prop "$PROPS" keyPassword)

  if [ -n "$KEYSTORE_FILE" ]; then
    [ -f "$KEYSTORE_FILE" ] || die "指定的签名文件不存在: $KEYSTORE_FILE"
  elif [ -f "$CFG_DIR/keystore/app.jks" ]; then
    KEYSTORE_FILE="$CFG_DIR/keystore/app.jks"
  elif [ -f "$CFG_DIR/keystore/app.keystore" ]; then
    KEYSTORE_FILE="$CFG_DIR/keystore/app.keystore"
  else
    KEYSTORE_FILE="$CFG_DIR/keystore/auto.jks"
    if [ ! -f "$KEYSTORE_FILE" ]; then
      warn "未提供签名文件，自动生成自用证书（请勿用于正式上架）"
      keytool -genkeypair -alias "${KEY_ALIAS:-key0}" \
        -dname "CN=UniApp Local Builder, OU=Dev, O=Local, L=Local, ST=Local, C=CN" \
        -keyalg RSA -keysize 2048 -validity 10950 \
        -keystore "$KEYSTORE_FILE" -storetype PKCS12 \
        -storepass "${STORE_PASSWORD:-android}" -keypass "${KEY_PASSWORD:-android}" >/dev/null
      KEY_STORE_AUTO=1
      {
        echo "# 自动生成，请妥善备份；替换前请勿删除，否则无法覆盖安装"
        echo "storeFile=$(basename "$KEYSTORE_FILE")"
        echo "storePassword=${STORE_PASSWORD:-android}"
        echo "keyAlias=${KEY_ALIAS:-key0}"
        echo "keyPassword=${KEY_PASSWORD:-android}"
      } > "$PROPS"
    fi
  fi

  STORE_PASSWORD=${STORE_PASSWORD:-android}
  KEY_ALIAS=${KEY_ALIAS:-key0}
  KEY_PASSWORD=${KEY_PASSWORD:-android}

  ok "keystore : $KEYSTORE_FILE"
  if [ "${KEY_STORE_AUTO:-0}" = "1" ]; then
    warn "使用的是自动生成的测试证书，正式发版请换成自己的 jks"
  fi
}

# =====================================================================
# 写入 DCLOUD_STREAMAPP_CHANNEL（渠道包标记）
# 格式：包名|appid|0|渠道名   —— 与 DCloud 云打包产出的结构一致
inject_channel() {
  # $1 = APPDIR  $2 = 渠道名（空 = 不打渠道标记）
  local APPDIR="$1" CH="$2"
  local M="$APPDIR/src/main/AndroidManifest.xml"
  [ -f "$M" ] || { warn "找不到 AndroidManifest.xml，跳过渠道注入"; return 0; }

  # 先清掉已有标记，避免多次构建叠加
  perl -0777 -i -pe 's{\s*<meta-data[^>]*android:name="DCLOUD_STREAMAPP_CHANNEL"[^>]*/>}{}g' "$M"
  if [ -z "$CH" ]; then
    return 0
  fi
  local VAL="$APP_PACKAGE|$APP_ID|0|$CH"
  perl -0777 -i -pe "s{</application>}{    <meta-data android:name=\"DCLOUD_STREAMAPP_CHANNEL\" android:value=\"$VAL\"/>\n    </application>}" "$M"
  if grep -q "DCLOUD_STREAMAPP_CHANNEL" "$M"; then
    ok "渠道标记：$CH（$VAL）"
  else
    warn "渠道标记写入失败（$CH）"
  fi
}

# =====================================================================
# 收集单个渠道的 APK（每个渠道执行一次），汇总信息写进 .apks / .apkjson
collect_output() {
  local CH="${1:-}"
  if [ -n "$CH" ]; then step "收集产物（渠道 $CH）"; else step "收集产物"; fi
  mkdir -p "$OUT_DIR"
  local OUT_APK_DIR="$APPDIR/build/outputs/apk/release"
  local apk
  apk=$(find "$OUT_APK_DIR" -maxdepth 2 -type f -name '*.apk' 2>/dev/null | head -1) || true
  [ -n "$apk" ] || apk=$(find "$APPDIR/build/outputs" -type f -name '*.apk' 2>/dev/null | head -1) || true
  [ -n "$apk" ] || die "编译结束但没有找到 APK，检查上面的 Gradle 输出"

  local safe suffix FINAL
  safe=$(sanitize "$APP_NAME")
  suffix=""
  [ -n "$CH" ] && suffix="_$CH"
  FINAL="$OUT_DIR/${safe}_v${APP_VERSION_NAME}${suffix}_${BUILD_STAMP}.apk"
  cp -f "$apk" "$FINAL"
  ok "APK: $(ls -lh "$FINAL" | awk '{print $5}')  $(basename "$FINAL")"

  # 校验摘要（每个包都校一次）
  local BT=""
  local candidate
  for candidate in $(ls -1d "$ANDROID_SDK_ROOT"/build-tools/* 2>/dev/null | sort -Vr); do BT=$candidate; done

  local MIN_SDK="" TARGET_SDK="" SIGN_SHA="" SIGN_DN="" VERIFIED="false"
  if [ -n "$BT" ]; then
    info "$("$BT/aapt2" dump badging "$FINAL" 2>/dev/null \
           | grep -E "^(package|application-label|launchable-activity|sdkVersion|targetSdkVersion)" | tr '\n' ';')"
    if "$BT/apksigner" verify "$FINAL" >/dev/null 2>&1; then
      ok "签名校验通过"
      VERIFIED="true"
    else
      warn "apksigner 校验未通过（APK 可能未签名或被改动）"
    fi
    MIN_SDK=$("$BT/aapt2" dump badging "$FINAL" 2>/dev/null | sed -n "s/.*sdkVersion:'\([0-9]*\)'.*/\1/p" | head -1) || true
    TARGET_SDK=$("$BT/aapt2" dump badging "$FINAL" 2>/dev/null | sed -n "s/.*targetSdkVersion:'\([0-9]*\)'.*/\1/p" | head -1) || true
    local CERTS
    CERTS=$("$BT/apksigner" verify --print-certs "$FINAL" 2>/dev/null) || true
    SIGN_SHA=$(printf '%s\n' "$CERTS" | sed -n 's/^Signer #1 certificate SHA-256 digest: //p' | head -1) || true
    SIGN_DN=$(printf '%s\n' "$CERTS" | sed -n 's/^Signer #1 certificate DN: //p' | head -1) || true
  fi
  SIGN_DN=${SIGN_DN//\"/\'}

  local TEST_SIGN="false"
  [ "${KEY_STORE_AUTO:-0}" = "1" ] && TEST_SIGN="true"
  case "$(basename "${KEYSTORE_FILE:-}")" in auto.jks) TEST_SIGN="true" ;; esac

  local SIZE SHA
  SIZE=$(stat -c%s "$FINAL")
  SHA=$(sha256sum "$FINAL" | awk '{print $1}')

  printf '%s\t%s\t%s\t%s\n' "${CH:-}" "$(basename "$FINAL")" "$SIZE" "$SHA" >> "$OUT_DIR/.apks"
  printf '{"channel":"%s","file":"%s","size":%s,"sha256":"%s"}\n' \
    "${CH:-}" "$(basename "$FINAL")" "$SIZE" "$SHA" >> "$OUT_DIR/.apkjson"

  # 全局保留最后一次的签名/体积信息（同一证书、同一版本，各渠道一致）
  LAST_MIN_SDK="${MIN_SDK:-0}"
  LAST_TARGET_SDK="${TARGET_SDK:-0}"
  LAST_SIGN_SHA="$SIGN_SHA"
  LAST_SIGN_DN="$SIGN_DN"
  LAST_VERIFIED="$VERIFIED"
  LAST_TEST_SIGN="$TEST_SIGN"
}

write_build_info() {
  local FIRST_FILE FIRST_SIZE FIRST_SHA APKS_JSON
  FIRST_FILE=$(awk -F'\t' 'NR==1{print $2}' "$OUT_DIR/.apks")
  FIRST_SIZE=$(awk -F'\t' 'NR==1{print $3}' "$OUT_DIR/.apks")
  FIRST_SHA=$(awk -F'\t' 'NR==1{print $4}' "$OUT_DIR/.apks")
  APKS_JSON=$(paste -sd, "$OUT_DIR/.apkjson")

  {
    printf '{\n'
    printf '  "appId": "%s",\n' "$APP_ID"
    printf '  "appName": "%s",\n' "$(printf '%s' "$APP_NAME" | sed 's/"/\\"/g')"
    printf '  "package": "%s",\n' "$APP_PACKAGE"
    printf '  "versionName": "%s",\n' "$APP_VERSION_NAME"
    printf '  "versionCode": %s,\n' "$APP_VERSION_CODE"
    printf '  "apk": "%s",\n' "$FIRST_FILE"
    printf '  "size": %s,\n' "$FIRST_SIZE"
    printf '  "sha256": "%s",\n' "$FIRST_SHA"
    printf '  "apks": [%s],\n' "$APKS_JSON"
    printf '  "minSdk": %s,\n' "${LAST_MIN_SDK:-0}"
    printf '  "targetSdk": %s,\n' "${LAST_TARGET_SDK:-0}"
    printf '  "signatureSha256": "%s",\n' "$LAST_SIGN_SHA"
    printf '  "signerDN": "%s",\n' "$LAST_SIGN_DN"
    printf '  "signatureVerified": %s,\n' "$LAST_VERIFIED"
    printf '  "testSigned": %s,\n' "$LAST_TEST_SIGN"
    printf '  "appKeySet": %s,\n' "$([ -n "$APP_KEY" ] && echo true || echo false)"
    printf '  "builtAt": "%s",\n' "$(date '+%Y-%m-%d %H:%M:%S')"
    printf '  "elapsed": "%s"\n' "$(elapsed)"
    printf '}\n'
  } > "$OUT_DIR/build-info.json"
  ls -1 "$OUT_DIR" > "$OUT_DIR/.manifest" 2>/dev/null || true
  ok "build-info.json 已写入"
}

# =====================================================================
do_build() {
  local SRC_SRC
  SRC_SRC=$(maybe_unpack "$SRC_DIR")
  if [ "$SRC_SRC" != "$SRC_DIR" ]; then SRC_DIR=$SRC_SRC; fi

  [ -d "$SRC_DIR" ] || die "源码目录不存在: $SRC_DIR"
  [ -n "$(ls -A "$SRC_DIR" 2>/dev/null)" ] || die "源码目录为空: $SRC_DIR"
  mkdir -p "$OUT_DIR" "$CFG_DIR"

  resolve_template

  step "定位 uni-app 资源"
  local WWW=""
  if WWW=$(find_www "$SRC_DIR"); then
    ok "发现本地打包资源：$WWW"
  else
    warn "未找到已编译资源，进入源码编译模式"
    build_from_source
    WWW=$(find_www "$SRC_DIR") || die "前端编译后仍找不到 app-service.js，请确认 uni build -p app 的输出路径"
    ok "编译产物：$WWW"
  fi

  step "读取应用信息"
  local MF="$WWW/manifest.json"
  local id name vn vc
  {
    read -r id; read -r name; read -r vn; read -r vc
  } < <(json_get "$MF")
  [ -n "$id" ] || id=$(basename "$(dirname "$WWW")" 2>/dev/null | grep -E '^__UNI__' || true)
  APP_ID=${APP_ID:-${id:-__UNI__BUILDER}}
  APP_NAME=${APP_NAME:-${name:-UniApp}}
  APP_VERSION_NAME=${APP_VERSION_NAME:-${vn:-1.0.0}}
  APP_VERSION_CODE=${APP_VERSION_CODE:-${vc:-1}}
  case $APP_VERSION_CODE in ''|*[!0-9]*) APP_VERSION_CODE=1 ;; esac
  APP_PACKAGE=${APP_PACKAGE:-com.android.uniapp.pkg}
  ok "appid=$APP_ID  name=$APP_NAME  v${APP_VERSION_NAME}(${APP_VERSION_CODE})  package=$APP_PACKAGE"

  step "组装 Android 工程"
  clear_dir "$WORK_DIR"; mkdir -p "$WORK_DIR"
  cp -a "$TPL_DIR"/. "$WORK_DIR"/
  local MODULE APPDIR
  MODULE=$(detect_module "$WORK_DIR")
  APPDIR="$WORK_DIR/$MODULE"
  ok "模块 :$MODULE"
  [ -d "$APPDIR" ] || die "工程模块目录不存在: $APPDIR（settings.gradle 里 include 的是 :$MODULE）"

  # 兜底：SDK 的 settings.gradle 一般只有 include，没有 rootProject.name，
  # 此时 Gradle 会拿目录名当工程名（点号开头会直接失败）。这里显式补一个。
  if [ -f "$WORK_DIR/settings.gradle" ] \
     && ! grep -qE 'rootProject\.name' "$WORK_DIR/settings.gradle"; then
    printf "\nrootProject.name = 'uniapp-pack'\n" >> "$WORK_DIR/settings.gradle"
  fi

  [ -f "$WORK_DIR/gradlew" ] || die "模板缺少 gradlew"
  chmod +x "$WORK_DIR/gradlew"
  grep -q $'\r' "$WORK_DIR/gradlew" && sed -i 's/\r$//' "$WORK_DIR/gradlew" || true

  # 注入打包资源（先清空 apps，避免模板自带的旧资源被一起打进去）
  local APPS="$APPDIR/src/main/assets/apps"
  clear_dir "$APPS"
  mkdir -p "$APPS/$APP_ID"
  cp -a "$WWW" "$APPS/$APP_ID/www"
  ok "已注入资源 → apps/$APP_ID/www"

  apply_config
  inject_appkey
  inject_icon
  inject_splash "$APPDIR"
  prepare_keystore
  ensure_sdk_pkgs
  gen_override

  step "Gradle 编译"
  export ANDROID_SDK_ROOT ANDROID_HOME
  local -a GARGS=("--no-daemon" "--console=plain" "--stacktrace")
  if [ "$USE_CN_MIRROR" = "1" ]; then GARGS+=("-I" "/opt/pack/init.gradle"); fi
  if [ -n "$GRADLE_EXTRA_ARGS" ]; then
    local -a extra=()
    read -r -a extra <<< "$GRADLE_EXTRA_ARGS"
    GARGS+=("${extra[@]}")
  fi
  # 渠道：CHANNELS="huawei,honor,xiaomi"，为空则打通用包（不带渠道标记）
  BUILD_STAMP=$(date '+%Y%m%d-%H%M%S')
  rm -f "$OUT_DIR/.apks" "$OUT_DIR/.apkjson"

  local -a CH_LIST=()
  if [ -n "${CHANNELS:-}" ]; then
    local _c
    for _c in $(printf '%s' "$CHANNELS" | tr ',' ' '); do
      _c=$(printf '%s' "$_c" | tr -d ' \t')
      [ -n "$_c" ] && CH_LIST+=("$_c")
    done
  fi
  [ ${#CH_LIST[@]} -eq 0 ] && CH_LIST=("")

  local _idx=0
  for CH in "${CH_LIST[@]}"; do
    _idx=$((_idx + 1))
    if [ -n "$CH" ]; then
      step "打包渠道 $CH（$_idx/${#CH_LIST[@]}）"
    fi
    inject_channel "$APPDIR" "$CH"
    log "./gradlew :$MODULE:assembleRelease"
    ( cd "$WORK_DIR" && ./gradlew ":$MODULE:assembleRelease" "${GARGS[@]}" ) \
      || die "Gradle 编译失败（渠道 ${CH:-通用}）"
    collect_output "$CH"
  done

  write_build_info
  rm -f "$OUT_DIR/.apks" "$OUT_DIR/.apkjson"

  step "完成"
  ok "耗时 $(elapsed)"
  info "产物目录：$OUT_DIR"
}

# =====================================================================
case "${1:-build}" in
  doctor) doctor ;;
  shell)  exec bash ;;
  clean)  clear_dir "$WORK_DIR"; ok "已清理中间产物" ;;
  build)  do_build ;;
  *)      die "未知指令: $1（支持 build/doctor/shell/clean）" ;;
esac
