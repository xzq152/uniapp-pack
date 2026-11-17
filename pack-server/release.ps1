# Build the single-file exe for release, and optionally publish to GitHub Releases.
#
# Usage:
#   .\release.ps1                        # build only -> dist\uniapp-pack.exe
#   .\release.ps1 -Tag v1.0.0            # build + commit + tag + push
#   .\release.ps1 -Tag v1.0.0 -Publish   # also upload exe to GitHub Release
#
# Uploading needs a GitHub token in GH_TOKEN or GITHUB_TOKEN.
#
# NOTE: this script is intentionally ASCII-only. Windows PowerShell 5.1 reads
# BOM-less .ps1 files using the system ANSI code page (GBK here), so non-ASCII
# text would corrupt parsing. User-facing Chinese text lives in dist-notes.md.

param(
  [string]$Tag = '',
  [switch]$Publish,
  [string]$Repo = 'xzq152/uniapp-pack'
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

function Step($msg) { Write-Host "`n[*] $msg" -ForegroundColor Cyan }
function Ok($msg) { Write-Host "[ok] $msg" -ForegroundColor Green }
function Fail($msg) {
  Write-Host "[x] $msg" -ForegroundColor Red
  exit 1
}

# ------------------------------------------------------------------ build
Step 'Build frontend'
Push-Location web
if (-not (Test-Path node_modules)) {
  & npm install --registry=https://registry.npmmirror.com
  if ($LASTEXITCODE -ne 0) {
    Pop-Location
    Fail 'npm install failed'
  }
}
& npm run build
$buildCode = $LASTEXITCODE
Pop-Location
if ($buildCode -ne 0) { Fail 'frontend build failed' }

Step 'Compile single-file exe'
$dist = Join-Path $root 'dist'
if (-not (Test-Path $dist)) { New-Item -ItemType Directory $dist | Out-Null }
$exe = Join-Path $dist 'uniapp-pack.exe'

# Generate the Windows resource object (icon + version info) before building.
# Without it the exe shows a blank default icon in Explorer.
$syso = Join-Path $root 'rsrc_windows_amd64.syso'
if (-not (Test-Path $syso)) {
  Write-Host '  generating icon/version resources...'
  & (Join-Path $root 'mkres.bat') | Out-Null
}
if (-not (Test-Path $syso)) {
  Write-Host '  [warn] rsrc not generated, exe will have no icon' -ForegroundColor Yellow
}

& go build -ldflags '-H=windowsgui -s -w' -o $exe .
if ($LASTEXITCODE -ne 0) { Fail 'go build failed' }

$sizeMB = [math]::Round((Get-Item $exe).Length / 1MB, 1)
$sha = (Get-FileHash $exe -Algorithm SHA256).Hash
Ok "artifact: $exe ($sizeMB MB)"
Write-Host "   SHA256: $sha"

if ($Tag -eq '') {
  Write-Host ''
  Write-Host 'No -Tag given, build only. No git operations.' -ForegroundColor Yellow
  Write-Host "artifact: $exe"
  exit 0
}

if (-not ($Tag -match '^v[0-9]+\.[0-9]+\.[0-9]+$')) {
  Fail "Tag must look like v1.0.0, got: $Tag"
}

# -------------------------------------------------------------------- git
Step "Commit and tag $Tag"
$dirty = git status --porcelain
if ($dirty) {
  git add -A
  git commit -m "release: $Tag"
  if ($LASTEXITCODE -ne 0) { Fail 'git commit failed' }
}

$existingTag = git tag -l $Tag
if ($existingTag) {
  Write-Host "tag $Tag already exists, skipping" -ForegroundColor Yellow
} else {
  git tag -a $Tag -m "uniapp-pack $Tag"
  if ($LASTEXITCODE -ne 0) { Fail 'git tag failed' }
}

Step 'Push to GitHub'
git push origin HEAD
if ($LASTEXITCODE -ne 0) { Fail 'git push failed' }
git push origin $Tag
if ($LASTEXITCODE -ne 0) { Fail 'git push tag failed' }
Ok "pushed $Tag"

if (-not $Publish) {
  Write-Host ''
  Write-Host 'No -Publish given, skipping Release upload.' -ForegroundColor Yellow
  exit 0
}

# ---------------------------------------------------------------- release
Step "Publish GitHub Release ($Repo)"

$token = $env:GH_TOKEN
if (-not $token) { $token = $env:GITHUB_TOKEN }
if (-not $token) {
  Write-Host '[x] Missing GitHub token. Set it and retry:' -ForegroundColor Red
  Write-Host "    `$env:GH_TOKEN = 'ghp_xxxx'" -ForegroundColor Yellow
  Write-Host '    token needs repo scope: https://github.com/settings/tokens'
  exit 1
}

$headers = @{
  Authorization          = 'Bearer ' + $token
  Accept                 = 'application/vnd.github+json'
  'X-GitHub-Api-Version' = '2022-11-28'
  'User-Agent'           = 'uniapp-pack-releaser'
}

$notesFile = Join-Path $root 'dist-notes.md'
if (-not (Test-Path $notesFile)) { Fail "missing release notes: $notesFile" }
$notes = (Get-Content $notesFile -Raw -Encoding UTF8) + "`n`nSHA256: " + $sha + "`n"

$rel = $null
try {
  $rel = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/tags/$Tag" -Headers $headers -Method Get -ErrorAction Stop
  Write-Host "Release $Tag exists, replacing asset"
} catch {
  $payload = @{
    tag_name   = $Tag
    name       = 'uniapp-pack ' + $Tag
    body       = $notes
    draft      = $false
    prerelease = $false
  } | ConvertTo-Json -Depth 5

  $rel = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases" -Headers $headers -Method Post -Body $payload -ContentType 'application/json'
  Ok "created Release: $($rel.html_url)"
}

# remove same-name old asset to avoid duplicates
foreach ($asset in $rel.assets) {
  if ($asset.name -eq 'uniapp-pack.exe') {
    Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/assets/$($asset.id)" -Headers $headers -Method Delete | Out-Null
    Write-Host "removed old asset $($asset.name)"
  }
}

Step 'Upload exe'
# upload_url looks like https://.../releases/123/assets{?name,label}
# strip the template tail, then append our own query string
$cut = $rel.upload_url.IndexOf('{')
if ($cut -gt 0) {
  $baseUpload = $rel.upload_url.Substring(0, $cut)
} else {
  $baseUpload = $rel.upload_url
}
$uploadURL = $baseUpload + '?name=uniapp-pack.exe'

$bytes = [System.IO.File]::ReadAllBytes($exe)
$asset = Invoke-RestMethod -Uri $uploadURL -Headers $headers -Method Post -Body $bytes -ContentType 'application/octet-stream'
Ok "uploaded: $($asset.browser_download_url)"

Write-Host ''
Write-Host "release page: $($rel.html_url)" -ForegroundColor Green
