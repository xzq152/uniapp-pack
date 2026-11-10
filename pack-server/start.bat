@echo off
setlocal
cd /d "%~dp0"

REM ===================================================================
REM   uni-app 安卓打包服务 · 局域网启动脚本
REM   双击即可启动，启动后按提示的局域网地址在其他电脑访问
REM ===================================================================

where docker >nul 2>nul
if errorlevel 1 (
  echo [x] 未检测到 docker，请先安装 Docker Desktop 并启动
  pause & exit /b 1
)

docker image inspect uniapp-android-builder:latest >nul 2>nul
if errorlevel 1 (
  echo [!] 打包镜像不存在，请先在上层目录执行 build-image.sh 构建镜像
  echo     cd ..\uniapp-android-builder ^&^& build-image.sh
  pause & exit /b 1
)

if not exist uniapp-pack-server.exe (
  echo [*] 首次启动，正在编译...
  set "PATH=%PATH%;C:\Program Files\Go\bin"
  go build -o uniapp-pack-server.exe .
  if errorlevel 1 ( echo [x] 编译失败 & pause & exit /b 1 )
)

if not exist "%~dp0data" mkdir "%~dp0data"

echo.
echo [*] 正在启动，关闭本窗口即停止服务
echo.
uniapp-pack-server.exe
pause
