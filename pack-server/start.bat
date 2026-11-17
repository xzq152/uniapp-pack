@echo off
REM ===================================================================
REM   uni-app 安卓打包服务 · 前台启动脚本（调试看日志用）
REM
REM   发布版是单文件 exe：双击 uniapp-pack.exe 即可，
REM   服务会自动启动并常驻托盘，本脚本只是方便在命令行里看输出。
REM
REM   注意：本文件编码为 GBK（cmd 按本地码页解析，勿存成 UTF-8）
REM ===================================================================
setlocal
cd /d "%~dp0"

REM ---- Docker 端点（改成自己的 Docker 机器）----
REM 本机 Docker：留空或注释掉这一行
set "DOCKER_HOST=tcp://192.168.1.23:2375"

REM ---- 端口 / 目录（不设就用默认）----
REM set "PORT=8080"
REM set "OUTPUT_DIR=%~dp0output"
REM set "CACHE_DIR=%~dp0cache"
REM set "DATA_DIR=%~dp0data"

if not exist uniapp-pack.exe (
  echo [*] 首次启动，正在编译...
  set "PATH=%PATH%;C:\Program Files\Go\bin"
  go build -o uniapp-pack.exe .
  if errorlevel 1 ( echo [x] 编译失败 & pause & exit /b 1 )
)

echo.
echo [*] DOCKER_HOST = %DOCKER_HOST%
echo [*] 正在启动，关闭本窗口即停止服务
echo.

uniapp-pack.exe
pause