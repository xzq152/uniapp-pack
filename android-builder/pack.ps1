<#
.SYNOPSIS
    uni-app 安卓离线打包 · Windows 纯命令行入口（只依赖 Docker）

.DESCRIPTION
    不装 Go、不装 Node、不装 Git Bash，只要 Docker Desktop 就能把 uni-app
    资源打成已签名的 APK。宿主机上唯一的前置条件就是：

        1) 装好 Docker Desktop 并让它跑起来
        2) 用 build-image.sh / build-image.bat 构建过一次镜像

    和 pack.sh 的关系：
        pack.sh 需要 bash（Windows 上得另外装 Git for Windows）。
        本脚本是它的 Windows 原生等价物，参数名尽量保持一致，
        在 Git Bash / WSL / macOS 上仍然请直接用 pack.sh。

.EXAMPLE
    # 资源模式（HBuilderX 导出的 __UNI__XXXX 目录或 zip）
    .\pack.ps1 D:\pkg\__UNI__6744715 -Package com.demo.app -Name "演示 App" -Version 2.0.8

.EXAMPLE
    # 环境自检
    .\pack.ps1 -Doctor

.EXAMPLE
    # Windows 首次 bind mount 卡住时改用 docker cp
    .\pack.ps1 .\__UNI__6744715.zip -Package com.demo.app -Cp

.EXAMPLE
    # 进容器 shell 排查
    .\pack.ps1 -Shell

.LINK
    https://github.com/xzq152/uniapp-pack
#>

param(
    # 源码目录 / 本地打包资源目录 / zip 文件
    [Parameter(Position = 0)]
    [string]$Src,

    # 输出目录
    [Alias('o')]
    [string]$Out = './output',

    # 原生配置目录
    [Alias('c')]
    [string]$Config = './config',

    # DCloud 离线 SDK 目录（镜像未内置 SDK 时必填）
    [Alias('s')]
    [string]$Sdk,

    # applicationId
    [Alias('p')]
    [string]$Package,

    # 应用名
    [Alias('n')]
    [string]$Name,

    # versionName
    [Alias('v')]
    [string]$Version,

    # versionCode（整数）
    [string]$VersionCode,

    # __UNI__XXXXXXX
    [string]$AppId,

    # DCloud 离线打包 appkey
    [string]$AppKey,

    # jks / keystore 文件
    [string]$Keystore,
    [string]$StorePass,
    [string]$KeyAlias,
    [string]$KeyPass,

    # 用 docker cp 传输，代替 bind mount
    [switch]$Cp,

    # 关闭阿里云 Maven 镜像
    [switch]$NoCnMirror,

    # 保留中间安卓工程到 ./build-cache
    [switch]$Keep,

    [switch]$Doctor,
    [switch]$Shell,
    [switch]$Clean,
    [switch]$Help
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

# ---------------------------------------------------------------------------
# 输出辅助
# ---------------------------------------------------------------------------
function Write-Info  { param([string]$m) Write-Host "[pack] $m" -ForegroundColor Cyan }
function Write-Ok    { param([string]$m) Write-Host "[pack] $m" -ForegroundColor Green }
function Write-Warn  { param([string]$m) Write-Host "[pack] $m" -ForegroundColor Yellow }
function Fail {
    param([string]$m)
    Write-Host "[pack] $m" -ForegroundColor Red
    exit 1
}

function Show-Usage {
    Write-Host @'
用法: .\pack.ps1 [选项] <源码目录|本地打包资源目录|zip>

  目录可以是：uni-app 源码根目录 / dist/build/app / HBuilderX 生成的 __UNI__XXX 资源
  环境只需要 Docker Desktop，不需要 Go / Node / Git Bash。

常用选项
  -Out DIR            输出 APK 目录（默认 ./output）
  -Config DIR         原生配置目录（默认 ./config）
  -Sdk DIR            DCloud 离线 SDK 目录（镜像未内置 SDK 时必填）
  -Package PKG        applicationId，如 com.demo.app（强烈建议显式指定）
  -Name NAME          应用名（默认取 manifest.json 的 name）
  -Version VER        versionName（默认取 manifest.json 的 versionName）
  -VersionCode N      versionCode（默认 1）
  -AppId APPID        __UNI__XXXXXXX（默认自动探测）
  -AppKey KEY         DCloud 离线打包 appkey（不填真机会报 appkey 错误）

签名
  -Keystore FILE      jks/keystore 文件路径
  -StorePass PWD      -KeyAlias NAME      -KeyPass PWD
                      全部缺省时自动生成并保存在 config\keystore\auto.jks

其它
  -Cp                 用 docker cp 传文件代替 bind mount
                      （Windows 首次挂载卡住时的救命选项，速度稳定）
  -NoCnMirror         关闭阿里云 Maven 镜像
  -Keep               把中间安卓工程留在 .\build-cache 便于排查
  -Doctor             打印容器内环境自检
  -Shell              直接进入容器 shell（调试）
  -Clean              清理容器中间产物
  -Help               显示本帮助

环境变量：IMAGE / GRADLE_VOLUME / PNPM_VOLUME / USE_CN_MIRROR / GRADLE_EXTRA_ARGS

范例
  .\pack.ps1 D:\pkg\__UNI__6744715 -Package com.demo.app -Name "演示 App" -Version 2.0.8
  .\pack.ps1 .\__UNI__6744715.zip -Package com.demo.app -Cp
  .\pack.ps1 -Doctor
'@
}

# ---------------------------------------------------------------------------
# 路径处理
#
# Docker 在 Windows 上对 -v 的冒号解析容易和盘符混淆（C:\foo:/bar），
# 统一转成正斜杠形式（C:/foo:/bar）最稳。
# ---------------------------------------------------------------------------
function Resolve-Full {
    param([string]$p)
    if ([System.IO.Path]::IsPathRooted($p)) { $full = $p }
    else { $full = Join-Path (Get-Location).Path $p }
    return [System.IO.Path]::GetFullPath($full)
}

function To-DockerPath {
    param([string]$p)
    return ((Resolve-Full $p) -replace '\\', '/')
}

function Ensure-Dir {
    param([string]$p)
    $full = Resolve-Full $p
    if (-not (Test-Path -LiteralPath $full)) {
        New-Item -ItemType Directory -Path $full -Force | Out-Null
    }
    return $full
}

# ---------------------------------------------------------------------------
# 按 Docker 实际可用内存自动定 Gradle 堆大小
#
# 只给一半左右：Android 打包（尤其 dex / 资源优化）很吃内存，容器总内存不足时
# Gradle daemon 会被直接 OOM kill，而报错却是含糊的
# "Gradle build daemon disappeared unexpectedly"。
# ---------------------------------------------------------------------------
function Get-DockerMemGB {
    try {
        $v = & docker info --format '{{.MemTotal}}' 2>$null
        if ($LASTEXITCODE -ne 0) { return 0 }
        $text = ($v | Select-Object -First 1)
        $bytes = 0.0
        if (-not [double]::TryParse($text, [ref]$bytes)) { return 0 }
        if ($bytes -le 0) { return 0 }
        return [int][math]::Floor($bytes / 1GB)
    } catch {
        return 0
    }
}

function Get-AutoGradleArgs {
    param([int]$TotalGB)
    if     ($TotalGB -ge 8) { $mx = 4096; $workers = 4 }
    elseif ($TotalGB -ge 6) { $mx = 3072; $workers = 4 }
    elseif ($TotalGB -ge 4) { $mx = 2048; $workers = 2 }
    else                    { $mx = 1024; $workers = 2 }

    if ($TotalGB -gt 0 -and $TotalGB -lt 4) {
        Write-Warn "Docker 可用内存仅 ${TotalGB}GB，Android 打包建议 >=4GB（推荐 8GB）"
        Write-Warn "Docker Desktop -> Settings -> Resources -> Memory 调大后再打包会更稳"
    }
    return "-Dorg.gradle.jvmargs=-Xmx${mx}m -Dorg.gradle.workers.max=$workers"
}

# ---------------------------------------------------------------------------
# 参数与模式
# ---------------------------------------------------------------------------
if ($Help) { Show-Usage; exit 0 }

$Mode = 'build'
if ($Doctor) { $Mode = 'doctor' }
if ($Shell)  { $Mode = 'shell' }
if ($Clean)  { $Mode = 'clean' }

$Image        = if ($env:IMAGE)         { $env:IMAGE }         else { 'uniapp-android-builder:latest' }
$GradleVolume = if ($env:GRADLE_VOLUME) { $env:GRADLE_VOLUME } else { 'uniapp_gradle_cache' }
$PnpmVolume   = if ($env:PNPM_VOLUME)   { $env:PNPM_VOLUME }   else { 'uniapp_pnpm_store' }
$ExtraGradle  = if ($env:GRADLE_EXTRA_ARGS) { $env:GRADLE_EXTRA_ARGS } else { '' }

$UseCnMirror = '1'
if ($NoCnMirror) { $UseCnMirror = '0' }
if ($env:USE_CN_MIRROR) { $UseCnMirror = $env:USE_CN_MIRROR }

# ---- 前置检查：docker 在不在 ----------------------------------------------
$dockerCmd = Get-Command docker -ErrorAction SilentlyContinue
if (-not $dockerCmd) {
    Fail "没有找到 docker 命令。请先安装 Docker Desktop 并启动它，然后重开一个终端（让 PATH 生效）。"
}

# ---- build 模式必须有 Src --------------------------------------------------
if ($Mode -eq 'build') {
    if (-not $Src) { Fail "请指定源码/资源目录，或运行 .\pack.ps1 -Help 查看用法" }
    if (-not (Test-Path -LiteralPath (Resolve-Full $Src))) { Fail "目录/文件不存在: $Src" }
}

$OutFull = Ensure-Dir $Out
$CfgFull = Ensure-Dir $Config

# ---------------------------------------------------------------------------
# 组装 docker 参数
# ---------------------------------------------------------------------------
$dockerArgs = New-Object System.Collections.Generic.List[string]
foreach ($a in @('run', '--rm', '-e', 'TZ=Asia/Shanghai')) { $dockerArgs.Add($a) }

if ($Src) {
    $dockerArgs.Add('-v')
    $dockerArgs.Add("$(To-DockerPath $Src):/workspace/src")
}
$dockerArgs.Add('-v'); $dockerArgs.Add("$(To-DockerPath $OutFull):/workspace/output")
$dockerArgs.Add('-v'); $dockerArgs.Add("$(To-DockerPath $CfgFull):/workspace/config")

if ($Sdk) {
    if (-not (Test-Path -LiteralPath (Resolve-Full $Sdk))) { Fail "SDK 目录不存在: $Sdk" }
    $dockerArgs.Add('-v'); $dockerArgs.Add("$(To-DockerPath $Sdk):/workspace/sdk:ro")
}

if ($Mode -eq 'build' -or $Mode -eq 'clean') {
    $dockerArgs.Add('-v'); $dockerArgs.Add("${GradleVolume}:/root/.gradle")
    $dockerArgs.Add('-v'); $dockerArgs.Add("${PnpmVolume}:/root/.local/share/pnpm/store")
}

if ($Keep) {
    $bc = Ensure-Dir './build-cache'
    $dockerArgs.Add('-v'); $dockerArgs.Add("$(To-DockerPath $bc):/workspace/work")
}

# ---- 环境变量 --------------------------------------------------------------
$envPairs = New-Object System.Collections.Generic.List[string]
function Add-Env {
    param([string]$k, [string]$v)
    if ($v -and $v.Trim().Length -gt 0) { $envPairs.Add("$k=$v") }
}
Add-Env 'APP_ID'            $AppId
Add-Env 'APP_KEY'           $AppKey
Add-Env 'APP_NAME'          $Name
Add-Env 'APP_VERSION_NAME'  $Version
Add-Env 'APP_VERSION_CODE'  $VersionCode
Add-Env 'APP_PACKAGE'       $Package
Add-Env 'KEYSTORE_FILE'     $Keystore
Add-Env 'STORE_PASSWORD'    $StorePass
Add-Env 'KEY_ALIAS'         $KeyAlias
Add-Env 'KEY_PASSWORD'      $KeyPass
Add-Env 'USE_CN_MIRROR'     $UseCnMirror

if (-not $ExtraGradle) {
    $memGB = Get-DockerMemGB
    $ExtraGradle = Get-AutoGradleArgs -TotalGB $memGB
    if ($memGB -gt 0) {
        Write-Info "已按 ${memGB}GB 内存设置 Gradle 堆（可用 GRADLE_EXTRA_ARGS 覆盖）"
    }
}
Add-Env 'GRADLE_EXTRA_ARGS' $ExtraGradle
Add-Env 'PACK_IMAGE'        $Image

foreach ($p in $envPairs) {
    $dockerArgs.Add('-e')
    $dockerArgs.Add($p)
}

# ---------------------------------------------------------------------------
# 镜像存在性检查
# ---------------------------------------------------------------------------
& docker image inspect $Image *> $null
if ($LASTEXITCODE -ne 0) {
    Fail "镜像不存在：$Image，请先执行 .\build-image.bat（或 build-image.sh）"
}

if ($Mode -eq 'build' -and -not $Package) {
    Write-Warn "未指定 -Package，将使用默认包名，上架前请务必显式指定"
}

# ---------------------------------------------------------------------------
# cp 模式：不挂 bind mount，全部用 docker cp 进出
# ---------------------------------------------------------------------------
function Invoke-CopyMode {
    $cname = "uniapp-pack-ps-$PID"
    & docker rm -f $cname *> $null

    $cArgs = New-Object System.Collections.Generic.List[string]
    foreach ($a in @('create', '--name', $cname, '-e', 'TZ=Asia/Shanghai')) { $cArgs.Add($a) }
    $cArgs.Add('-v'); $cArgs.Add("${GradleVolume}:/root/.gradle")
    $cArgs.Add('-v'); $cArgs.Add("${PnpmVolume}:/root/.local/share/pnpm/store")
    foreach ($p in $envPairs) { $cArgs.Add('-e'); $cArgs.Add($p) }
    $cArgs.Add($Image)
    $cArgs.Add('build')

    # container id 也会被输出，必须吃掉，否则会混进函数返回值
    & docker $cArgs.ToArray() *> $null
    if ($LASTEXITCODE -ne 0) { Fail "容器创建失败" }

    Write-Info "注入源码（docker cp，不走挂载）"
    $srcFull = Resolve-Full $Src
    if (Test-Path -LiteralPath $srcFull -PathType Container) {
        # 目录：末尾的 /. 表示「把内容拷进去」而不是「把目录本身拷进去」
        & docker cp "$(To-DockerPath $srcFull)/." "${cname}:/workspace/src"
    } else {
        & docker cp "$(To-DockerPath $srcFull)" "${cname}:/workspace/src/"
    }
    if ($LASTEXITCODE -ne 0) {
        & docker rm -f $cname *> $null
        Fail "注入源码失败"
    }

    $cfgEntries = @(Get-ChildItem -LiteralPath $CfgFull -Force -ErrorAction SilentlyContinue)
    if ($cfgEntries.Count -gt 0) {
        & docker cp "$(To-DockerPath $CfgFull)/." "${cname}:/workspace/config"
        if ($LASTEXITCODE -ne 0) {
            & docker rm -f $cname *> $null
            Fail "注入配置失败"
        }
    }

    Write-Info "开始打包"
    # 注意：docker start -a 的输出必须直接落到控制台，不能被函数返回值捕获，
    # 否则日志会污染返回值、把退出码变成一整串字符串。
    & docker start -a $cname | Out-Host
    $rc = $LASTEXITCODE

    Write-Info "回传产物"
    & docker cp "${cname}:/workspace/output/." $OutFull *> $null
    & docker cp "${cname}:/workspace/config/." $CfgFull *> $null

    & docker rm -f $cname *> $null
    # 用 Write-Output -NoEnumerate 保证调用方拿到的是单个整数，而不是数组
    Write-Output -NoEnumerate ([int]$rc)
}

# ---------------------------------------------------------------------------
# 执行
# ---------------------------------------------------------------------------
$transport = if ($Cp) { 'docker-cp' } else { 'bind-mount' }
Write-Info "镜像：$Image   模式：$Mode   传输：$transport"
$startTs = Get-Date

$exitCode = 0
if ($Cp -and $Mode -eq 'build') {
    $exitCode = Invoke-CopyMode
} else {
    $runArgs = $dockerArgs.ToArray()
    if ($Mode -eq 'shell') {
        # shell 需要交互
        $withIt = New-Object System.Collections.Generic.List[string]
        foreach ($a in $runArgs) { $withIt.Add($a) }
        $withIt.Insert(1, '-it')
        $runArgs = $withIt.ToArray()
    }
    & docker $runArgs $Image $Mode
    $exitCode = $LASTEXITCODE
}

$elapsed = [int]((Get-Date) - $startTs).TotalSeconds
if ($Mode -eq 'build') {
    Write-Host ''
    if ($exitCode -eq 0) {
        Write-Ok "总耗时 ${elapsed}s，产物目录：$OutFull"
        Get-ChildItem -LiteralPath $OutFull -Filter '*.apk' -File -ErrorAction SilentlyContinue |
            Select-Object -Last 5 |
            ForEach-Object { Write-Host ("         {0}  {1:N1} MB" -f $_.Name, ($_.Length / 1MB)) }
    } else {
        Write-Warn "打包失败（退出码 $exitCode），产物目录：$OutFull"
    }
}

exit $exitCode
