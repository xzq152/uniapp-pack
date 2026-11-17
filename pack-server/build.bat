@echo off
REM ===================================================================
REM   一键构建：前端（Vue3 + TDesign）→ 嵌入 Go 二进制（单文件 exe）
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

tasklist 2>nul | findstr /I "uniapp-pack.exe" >nul
if not errorlevel 1 (
  echo [!] uniapp-pack.exe 正在运行，会锁住 exe
  echo     请先在托盘右键选「退出」，然后按任意键继续构建
  pause
)

echo [*] 构建前端...
pushd web
if not exist node_modules ( call npm install --registry=https://registry.npmmirror.com )
call npm run build
if errorlevel 1 ( popd & echo [x] 前端构建失败 & pause & exit /b 1 )
popd

echo [*] 生成 exe 图标与版本资源...
call mkres.bat

echo [*] 编译单文件 exe（服务 + 托盘，无控制台窗口）...
go build -ldflags "-H=windowsgui -s -w" -o uniapp-pack.exe .
if errorlevel 1 ( echo [x] 编译失败 & pause & exit /b 1 )

echo.
echo [ok] 完成：uniapp-pack.exe
echo      双击即用：服务自动启动，托盘常驻，启动时弹窗显示地址
echo.
echo      产物 / 缓存 / 数据 都在 exe 同级：
echo        output\   APK 产物
echo        cache\    可再生缓存（上传包、临时文件）
echo        data\     证书、任务记录
pause