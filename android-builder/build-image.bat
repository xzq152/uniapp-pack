@echo off
REM ===================================================================
REM   构建打包镜像 uniapp-android-builder:latest
REM
REM   只依赖 Docker Desktop，不需要 Go / Node / Git Bash。
REM   本文件编码为 GBK + CRLF（cmd 按本地码页解析，勿存成 UTF-8）
REM
REM   用法：
REM     build-image.bat                     默认 tag
REM     build-image.bat -t my-img:1.0       自定义 tag
REM     build-image.bat --no-sdk            不把 sdk\ 下的压缩包烤进镜像
REM ===================================================================
setlocal EnableDelayedExpansion
cd /d "%~dp0"

set "TAG=uniapp-android-builder:latest"
if defined IMAGE set "TAG=%IMAGE%"
set "SKIP_SDK=0"

:parse
if "%~1"=="" goto :done
if /I "%~1"=="-t"          ( set "TAG=%~2" & shift & shift & goto :parse )
if /I "%~1"=="--tag"       ( set "TAG=%~2" & shift & shift & goto :parse )
if /I "%~1"=="--no-sdk"    ( set "SKIP_SDK=1" & shift & goto :parse )
if /I "%~1"=="-h"          goto :help
if /I "%~1"=="--help"      goto :help
echo [build] 未知参数: %~1
echo         试试 build-image.bat --help
exit /b 1
:done

echo [build] 检查 docker...
where docker >nul 2>nul
if errorlevel 1 (
  echo [build] [x] 没有找到 docker 命令
  echo [build]     请先安装 Docker Desktop 并启动它，然后重开一个终端
  pause
  exit /b 1
)

docker info >nul 2>nul
if errorlevel 1 (
  echo [build] [x] Docker 引擎没在运行
  echo [build]     请启动 Docker Desktop，等鲸鱼图标变绿后再试
  pause
  exit /b 1
)

REM ---- 统计 sdk\ 下的压缩包 ----
set "SDK_ITEMS=0"
if exist "sdk\*.zip"    set /a SDK_ITEMS+=1
if exist "sdk\*.tar.gz" set /a SDK_ITEMS+=1
if exist "sdk\*.tgz"    set /a SDK_ITEMS+=1
if exist "sdk\*.tar.xz" set /a SDK_ITEMS+=1

set "CTX=."
if "%SKIP_SDK%"=="1" (
  echo [build] --no-sdk: 镜像不含 DCloud SDK，运行时请用 -Sdk 挂载
  set "CTX=%TEMP%\uniapp-pack-nosdk-%RANDOM%"
  mkdir "!CTX!" 2>nul
  copy /y Dockerfile "!CTX!\" >nul
  xcopy /e /i /q /y docker "!CTX!\docker" >nul
  mkdir "!CTX!\sdk" 2>nul
) else (
  if "%SDK_ITEMS%"=="0" (
    echo [build] 提示: sdk\ 目录为空。
    echo [build]       请先到 https://nativesupport.dcloud.net.cn/AppDocs/download/android
    echo [build]       下载与 HBuilderX 版本匹配的「Android 离线 SDK」，把 zip 放到 sdk\ 下再构建,
    echo [build]       否则镜像仍可构建，但运行时需要通过 pack.ps1 -Sdk ^<SDK目录^> 挂载。
  ) else (
    echo [build] 发现 %SDK_ITEMS% 个 SDK 压缩包，将被烤入镜像
  )
)

echo [build] 开始构建 %TAG%
docker build -t "%TAG%" --provenance=false -f Dockerfile "%CTX%"
if errorlevel 1 (
  echo [build] [x] 构建失败
  if not "%SKIP_SDK%"=="1" goto :cleanup
  rd /s /q "!CTX!" 2>nul
  pause
  exit /b 1
)

if not "%SKIP_SDK%"=="1" goto :ok
:cleanup
rd /s /q "!CTX!" 2>nul

:ok
echo.
echo [build] [ok] 构建完成: %TAG%
echo [build] 镜像大小:
docker images "%TAG%" --format "        {{.Size}}"
echo.
echo 下一步: 用 pack.ps1 打包
echo     .\pack.ps1 D:\path\to\__UNI__XXXX -Package com.demo.app -Name "我的App" -Version 2.0.8
pause
exit /b 0

:help
echo 用法: build-image.bat [-t TAG] [--no-sdk]
echo.
echo   -t, --tag TAG   镜像 tag（默认 uniapp-android-builder:latest）
echo   --no-sdk        不把 sdk\ 下的压缩包烤进镜像
echo   -h, --help      显示本帮助
echo.
echo 环境变量:
echo   IMAGE                   等于 -t 的效果
echo   ANDROID_PLATFORMS       预装的 platform 版本，空格分隔
echo   ANDROID_BUILD_TOOLS     预装的 build-tools 版本，空格分隔
echo   NODE_VERSION / PNPM_VERSION
echo   WITH_JDK11=true         老版本 DCloud SDK（Gradle ^<7.3）需要时开启
echo   APT_MIRROR / NODE_MIRROR / ANDROID_CMDLINE_TOOLS_URL
exit /b 0
