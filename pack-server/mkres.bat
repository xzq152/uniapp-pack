@echo off
REM ===================================================================
REM   生成 Windows 资源文件（rsrc_windows_amd64.syso），把图标嵌进 exe
REM   被 build.bat / release.ps1 调用，一般不用手动跑
REM   注意：本文件编码为 GBK（cmd 按本地码页解析，勿存成 UTF-8）
REM ===================================================================
setlocal
cd /d "%~dp0"

REM 已经存在就直接用（图标改了再删掉它，或加 -force）
if exist rsrc_windows_amd64.syso (
  if /I not "%~1"=="-force" (
    echo [*] rsrc_windows_amd64.syso 已存在，跳过
    exit /b 0
  )
)

if not exist assets\app.ico (
  echo [!] 找不到 assets\app.ico，exe 将没有图标
  exit /b 0
)

set "WINRES=%USERPROFILE%\go\bin\go-winres.exe"
if not exist "%WINRES%" (
  echo [!] 找不到 go-winres，尝试安装...
  set "GOSUMDB=off"
  set "GOPROXY=https://goproxy.cn,direct"
  go install github.com/tc-hib/go-winres@latest
  if errorlevel 1 (
    echo [!] go-winres 安装失败，exe 将没有图标
    echo     可手动执行：go install github.com/tc-hib/go-winres@latest
    exit /b 0
  )
)

echo [*] 生成 exe 图标与版本资源...
"%WINRES%" simply ^
  --arch amd64 ^
  --out rsrc ^
  --manifest cli ^
  --icon assets\app.ico ^
  --file-description "uni-app Android packaging service" ^
  --product-name "uniapp-pack" ^
  --copyright "Copyright (c) uniapp-pack" ^
  --original-filename "uniapp-pack.exe" ^
  --product-version "1.0.0.0" ^
  --file-version "1.0.0.0"

if errorlevel 1 (
  echo [!] 资源生成失败，exe 将没有图标
  exit /b 0
)
echo [ok] rsrc_windows_amd64.syso 已生成
exit /b 0
