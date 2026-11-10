# uni-app 安卓离线打包镜像（本地版 HBuilderX 云打包）
# ---------------------------------------------------------------
# 镜像内包含：JDK17 + Android SDK(cmdline-tools/platform/build-tools) + Node/pnpm
#              + Gradle 依赖缓存目录 + 自动化打包入口脚本
# DCloud 离线 SDK 不随镜像分发，构建时把 zip 放进 ./sdk/ 即可被一起烤进镜像；
# 也可以运行期用 -v 挂载（见 README）。
# ---------------------------------------------------------------

ARG BASE_IMAGE=eclipse-temurin:17-jdk-jammy
FROM ${BASE_IMAGE}

LABEL org.opencontainers.image.title="uniapp-android-builder"
LABEL org.opencontainers.image.description="uni-app 安卓离线打包环境，输入源码/本地资源 → 输出已签名 APK"

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# ---- 可调构建参数 -------------------------------------------------
ARG DEBIAN_FRONTEND=noninteractive
ARG APT_MIRROR=
ARG ANDROID_CMDLINE_TOOLS_URL=https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip
# 默认按 DCloud 离线 SDK 5.3x（compileSdk 35 / build-tools 35.0.0）准备，
# 缺的组件 entrypoint 仍会按 build.gradle 自动补装，这里只是省掉每次下载
ARG ANDROID_PLATFORMS="android-33 android-34 android-35"
ARG ANDROID_BUILD_TOOLS="34.0.0 35.0.0"
ARG NODE_VERSION=20.19.0
ARG NODE_MIRROR=https://nodejs.org/dist
ARG NPM_REGISTRY=https://registry.npmmirror.com
ARG PNPM_VERSION=9.15.4
# 老版本 DCloud SDK（Gradle < 7.3）需要 JDK11 时置为 true
ARG WITH_JDK11=false

# ---- 环境变量 -----------------------------------------------------
ENV ANDROID_SDK_ROOT=/opt/android-sdk \
    ANDROID_HOME=/opt/android-sdk \
    GRADLE_USER_HOME=/root/.gradle \
    LANG=C.UTF-8 \
    TZ=Asia/Shanghai \
    DEBIAN_FRONTEND=noninteractive
# 注意：追加而非覆盖基础镜像的 PATH（temurin 把 java 放在 /opt/java/openjdk/bin）
ENV PATH=${ANDROID_SDK_ROOT}/cmdline-tools/latest/bin:${ANDROID_SDK_ROOT}/platform-tools:${PATH}

# ---- 系统依赖 -----------------------------------------------------
RUN if [ -n "${APT_MIRROR}" ]; then \
      sed -i "s|http://archive.ubuntu.com/ubuntu/|${APT_MIRROR}|g; s|http://security.ubuntu.com/ubuntu/|${APT_MIRROR}|g" /etc/apt/sources.list; \
    fi \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
      curl ca-certificates unzip zip xz-utils git file tzdata procps openssl \
 && rm -rf /var/lib/apt/lists/* \
 && ln -snf /usr/share/zoneinfo/${TZ} /etc/localtime && echo ${TZ} > /etc/timezone

# ---- 可选 JDK11（老 SDK 兜底）--------------------------------------
RUN if [ "${WITH_JDK11}" = "true" ]; then \
      apt-get update && apt-get install -y --no-install-recommends openjdk-11-jdk-headless \
      && rm -rf /var/lib/apt/lists/* ; \
    fi

# ---- Node / pnpm ---------------------------------------------------
RUN set -eux; \
    case "$(dpkg --print-architecture)" in \
      amd64) NODE_ARCH=x64 ;; \
      arm64) NODE_ARCH=arm64 ;; \
      *) echo "不支持的架构"; exit 1 ;; \
    esac; \
    curl -fsSL "${NODE_MIRROR}/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.xz" -o /tmp/node.tar.xz; \
    tar -xJf /tmp/node.tar.xz -C /usr/local --strip-components=1 --no-same-owner; \
    rm -f /tmp/node.tar.xz; \
    node -v; npm -v; \
    npm config set registry "${NPM_REGISTRY}"; \
    npm install -g "pnpm@${PNPM_VERSION}"; \
    pnpm -v; \
    npm cache clean --force

# ---- Android SDK ---------------------------------------------------
RUN set -eux; \
    mkdir -p "${ANDROID_SDK_ROOT}/cmdline-tools"; \
    curl -fsSL "${ANDROID_CMDLINE_TOOLS_URL}" -o /tmp/clt.zip; \
    unzip -q /tmp/clt.zip -d /tmp/clt; \
    mv /tmp/clt/cmdline-tools "${ANDROID_SDK_ROOT}/cmdline-tools/latest"; \
    rm -rf /tmp/clt /tmp/clt.zip; \
    ( yes | sdkmanager --sdk_root="${ANDROID_SDK_ROOT}" --licenses >/dev/null 2>&1 || true ); \
    PKGS="platform-tools"; \
    for v in ${ANDROID_PLATFORMS}; do PKGS="${PKGS} platforms;${v}"; done; \
    for v in ${ANDROID_BUILD_TOOLS}; do PKGS="${PKGS} build-tools;${v}"; done; \
    sdkmanager --sdk_root="${ANDROID_SDK_ROOT}" ${PKGS}; \
    rm -rf "${ANDROID_SDK_ROOT}/.temp" "${ANDROID_SDK_ROOT}/.downloadIntermediates"; \
    sdkmanager --sdk_root="${ANDROID_SDK_ROOT}" --list_installed | sed -n '1,40p'

# ---- 打包脚本 ------------------------------------------------------
COPY docker/ /opt/pack/
RUN chmod +x /opt/pack/*.sh \
 && javac -d /opt/pack /opt/pack/NinePatch.java \
 && ln -sf /opt/pack/prepare-sdk.sh /usr/local/bin/prepare-sdk \
 && ln -sf /opt/pack/entrypoint.sh /usr/local/bin/uniapp-build \
 && mkdir -p /opt/dcloud/template /opt/dcloud/sdk \
             /workspace/src /workspace/output /workspace/config /workspace/work

# ---- 可选：把 ./sdk/*.zip 一起烤进镜像 -----------------------------
COPY sdk/ /tmp/sdk/
RUN set -eux; \
    shopt -s nullglob; \
    items=(/tmp/sdk/*.zip /tmp/sdk/*.tar.gz /tmp/sdk/*.tgz /tmp/sdk/*.tar.xz); \
    if [ ${#items[@]} -gt 0 ]; then \
      for it in "${items[@]}"; do \
        if /opt/pack/prepare-sdk.sh "${it}" /opt/dcloud/template; then break; fi; \
        echo "-> 无法从 ${it} 提取模板，尝试下一个"; \
      done; \
      if [ ! -f /opt/dcloud/template/settings.gradle ]; then \
        echo "ERROR: sdk/ 下有压缩包，但没找到 HBuilder-Integrate-AS，请确认下载的是 DCloud 安卓离线 SDK"; exit 1; \
      fi; \
    else \
      echo "-> sdk/ 为空，镜像不含离线 SDK，请在运行时挂载 -v <sdk目录>:/workspace/sdk"; \
    fi; \
    rm -rf /tmp/sdk

WORKDIR /workspace
ENTRYPOINT ["/opt/pack/entrypoint.sh"]
CMD ["build"]
