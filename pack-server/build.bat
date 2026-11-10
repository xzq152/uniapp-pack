@echo off
REM ===================================================================
REM   一键构建：前端（Vue3 + TDesign）→ 嵌入 Go 二进制 → 启动器
REM   注意：本文件编码为 GBK（cmd 按本地码页解析，勿存成 UTF-8）
REM ===================================================================
setlocal
cd /d "%~dp0"

where node >nul 2>nul
if errorlevel 1 (
  if exist "%USERPROFILE%\.workbuddy\binaries\node" (
    for /d %%D in ("%USERPROFILE%\.workbuddy\binaries\node\versions\*") do set "PATH=%%D;%PATH%"
  )
)
where node >nul 2>nul
if errorlevel 1 ( echo [x] 找不到 node，请安装 Node.js 18+ & pause & exit /b 1 )

set "PATH=%PATH%;C:\Program Files\Go\bin"
where go >nul 2>nul
if errorlevel 1 ( echo [x] 找不到 go，请安装 Go 1.22+ & pause & exit /b 1 )

tasklist 2>nul | findstr /I "uniapp-pack-server.exe 启动服务.exe" >nul
if not errorlevel 1 (
  echo [!] 服务或托盘程序正在运行，会锁住 exe
  echo     请先双击 停止服务.exe，然后按任意键继续构建
  pause
)

echo [*] 构建前端...
pushd web
if not exist node_modules ( call npm install --registry=https://registry.npmmirror.com )
call npm run build
if errorlevel 1 ( popd & echo [x] 前端构建失败 & pause & exit /b 1 )
popd

echo [*] 编译服务（前端已嵌入）...
go build -o uniapp-pack-server.exe .
if errorlevel 1 ( echo [x] 编译失败 & pause & exit /b 1 )

echo [*] 编译启动器 / 停止器（无控制台窗口）...
go build -ldflags "-H=windowsgui -s -w" -o 启动服务.exe ./cmd/launcher
if errorlevel 1 ( echo [x] 启动服务.exe 编译失败 & pause & exit /b 1 )
go build -ldflags "-H=windowsgui -s -w" -o 停止服务.exe ./cmd/stopper
if errorlevel 1 ( echo [x] 停止服务.exe 编译失败 & pause & exit /b 1 )

echo.
echo [ok] 完成：
echo      uniapp-pack-server.exe   服务本体
echo      启动服务.exe             双击静默启动（不显示命令行窗口）
echo      停止服务.exe             双击静默停止
pause
