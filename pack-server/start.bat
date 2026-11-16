@echo off
setlocal
cd /d "%~dp0"

REM ===================================================================
REM   uni-app 安卓打包服务 · 局域网启动脚本
REM
REM   两种部署方式都支持：
REM     · Docker 在本机   → 不需要设置 DOCKER_HOST，自动走 unix socket
REM     · Docker 在别的机器 → 设置下面的 DOCKER_HOST
REM
REM   注意：服务已改为「直连 Docker Engine HTTP API」（见 dockerapi.go），
REM   所以**本机不需要安装 docker 命令行**，只要能访问 Docker 的 API 端口即可。
REM   以前这里会 `where docker` 检查 CLI，跨机部署时会误报「未检测到 docker」。
REM ===================================================================

REM ---- Docker 端点（改成自己的 Docker 机器）----
REM 本机 Docker：留空或注释掉这一行
set "DOCKER_HOST=tcp://192.168.1.23:2375"

REM ---- 数据目录（上传包/证书/产物都在这里，务必放在持久位置）----
REM 不设置的话会默认取 exe 所在目录的 data/，用 go run 时会落到临时目录
set "DATA_DIR=%~dp0data"

if not exist "%DATA_DIR%" mkdir "%DATA_DIR%"

REM ---- 首次启动自动编译 ----
if not exist pack-server.exe (
  echo [*] 首次启动，正在编译...
  set "PATH=%PATH%;C:\Program Files\Go\bin"
  go build -o pack-server.exe .
  if errorlevel 1 ( echo [x] 编译失败 & pause & exit /b 1 )
)

echo.
echo [*] DOCKER_HOST = %DOCKER_HOST%
echo [*] DATA_DIR    = %DATA_DIR%
echo [*] 正在启动，关闭本窗口即停止服务
echo.

pack-server.exe
pause
