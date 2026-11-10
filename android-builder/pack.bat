@echo off
setlocal EnableDelayedExpansion
REM ===================================================================
REM   uni-app 安卓打包 · Windows 便捷入口
REM
REM   用法（双击=交互式，或命令行直接带参数）：
REM     pack.bat                     交互式：把资源目录拖进窗口即可
REM     pack.bat D:\xxx\__UNI__123 -p com.demo.app -n "我的App" -v 2.0.8
REM     pack.bat --doctor
REM
REM   本文件只是转发给 pack.sh，会自动寻找 Git Bash / WSL 的 bash.exe
REM ===================================================================

cd /d "%~dp0"

REM ---- 找 bash ----
set "BASH="
for %%P in (
  "C:\Program Files\Git\bin\bash.exe"
  "C:\Program Files (x86)\Git\bin\bash.exe"
  "%LOCALAPPDATA%\Programs\Git\bin\bash.exe"
  "C:\Program Files\Git\usr\bin\bash.exe"
) do (
  if exist %%~P if not defined BASH set "BASH=%%~P"
)
if not defined BASH (
  where bash.exe >nul 2>nul && set "BASH=bash.exe"
)
if not defined BASH (
  echo [pack] 找不到 bash.exe，请先安装 Git for Windows
  echo         https://git-scm.com/download/win
  pause
  exit /b 1
)

if "%~1"=="" goto :interactive

echo [pack] 调用打包脚本...
"%BASH%" -lc "cd '%~dp0' && ./pack.sh %*"
if errorlevel 1 ( echo [pack] 打包失败 & pause & exit /b 1 )
echo [pack] 完成，产物在 output\
pause
exit /b 0

:interactive
set /p SRCDIR=把 uni-app 源码目录或 __UNI__XXX 资源目录拖进来后回车: 
set /p PKG=包名 applicationId ^(如 com.demo.app，回车跳过^): 
set /p VNAME=版本名 versionName ^(如 2.0.8，回车跳过^): 

set "ARGS=%SRCDIR%"
if defined PKG   set "ARGS=%ARGS% -p %PKG%"
if defined VNAME set "ARGS=%ARGS% -v %VNAME%"

echo [pack] 开始打包: %ARGS%
"%BASH%" -lc "cd '%~dp0' && ./pack.sh %ARGS%"
if errorlevel 1 ( echo [pack] 打包失败 & pause & exit /b 1 )
echo [pack] 完成，产物在 output\
pause
exit /b 0
